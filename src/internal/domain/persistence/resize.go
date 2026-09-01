package persistence

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// expandVolumes grows every operator-owned claim whose spec size moved up.
//
// A StatefulSet's volumeClaimTemplates are immutable once it exists, so a size change cannot be
// routed through the workload: the operator patches the claims themselves and leaves the template
// at the size the StatefulSet was created with. The consequence is that a claim a later scale-out
// creates is born at the old size — which is why this runs on every pass over every ordinal rather
// than only over what just changed. Shrink never reaches here: CEL refuses it at admission, and
// growClaim refuses it again in case an older CRD is installed (BDR-005).
func (r *Reconciler) expandVolumes(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) {
	if r.Client == nil {
		return
	}
	log := ctrllog.FromContext(ctx)
	protected := renderstorage.ProtectedClaimNames(neo4j)
	var grown []string

	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		stsName := ctxRender.STSName()

		// Bounded by the live StatefulSet: claims past its replica count are retained leftovers of a
		// scale-in, and growing a volume nothing mounts would bill the user for nothing.
		var sts appsv1.StatefulSet
		key := types.NamespacedName{Name: stsName, Namespace: ctxRender.Namespace()}
		if err := r.Client.Get(ctx, key, &sts); err != nil {
			continue // not created yet — the template carries the right size at create
		}
		replicas := int32(1)
		if sts.Spec.Replicas != nil {
			replicas = *sts.Spec.Replicas
		}

		for _, vct := range renderstorage.VolumeClaimTemplates(ctxRender) {
			want, ok := vct.Spec.Resources.Requests[corev1.ResourceStorage]
			if !ok {
				continue
			}
			for ordinal := int32(0); ordinal < replicas; ordinal++ {
				name := renderstorage.ClaimName(vct.Name, stsName, ordinal)
				if _, skip := protected[name]; skip {
					continue
				}
				changed, err := r.growClaim(ctx, neo4j, ctxRender.Namespace(), name, want)
				if err != nil {
					// A refusal is not a pipeline failure: Neo4j keeps serving at the old size and
					// every other step must go on converging. The Event carries the API server's own
					// words — usually a StorageClass with allowVolumeExpansion false — while the
					// writer reports the claim as behind for as long as it stays behind.
					r.advisories.Emitf(r.Recorder, neo4j, corev1.EventTypeWarning, oracle.ReasonStorageResizeFailed,
						"cannot grow claim %s to %s: %v", name, want.String(), err)
					log.Error(err, "volume expansion refused", "pvc", name, "requested", want.String())
					continue
				}
				if changed {
					grown = append(grown, name)
				}
			}
		}
	}

	if len(grown) > 0 {
		sort.Strings(grown)
		log.Info("volume expansion requested",
			"claims", strings.Join(grown, ","),
			"note", "capacity follows once the CSI driver resizes the volume",
		)
	}
}

// growClaim raises one claim's storage request, reporting whether it changed anything. It never
// lowers a request: Kubernetes rejects that outright, and a claim already larger than the spec is
// somebody's deliberate manual expansion, not drift to correct.
func (r *Reconciler) growClaim(ctx context.Context, neo4j *neo4jv1beta1.Neo4j, namespace, name string,
	want resource.Quantity) (bool, error) {
	var pvc corev1.PersistentVolumeClaim
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil // the StatefulSet controller has not created it yet
		}
		return false, err
	}
	if !render.HasOperandLabels(&pvc, neo4j.Name) {
		return false, nil // not ours to touch (ADD-04)
	}
	have := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if want.Cmp(have) <= 0 {
		return false, nil
	}
	base := pvc.DeepCopy()
	if pvc.Spec.Resources.Requests == nil {
		pvc.Spec.Resources.Requests = corev1.ResourceList{}
	}
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = want
	if err := r.Client.Patch(ctx, &pvc, client.MergeFrom(base)); err != nil {
		return false, err
	}
	return true, nil
}

// claimsBehindCapacity names every operator-owned claim whose capacity has not reached its own
// request, which is what an expansion still in flight looks like from outside.
func (r *Reconciler) claimsBehindCapacity(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) []string {
	if r.Client == nil {
		return nil
	}
	var list corev1.PersistentVolumeClaimList
	if err := r.Client.List(ctx, &list, client.InNamespace(neo4j.Namespace),
		client.MatchingLabelsSelector{Selector: render.StoragePVCSelector(neo4j.Name)}); err != nil {
		return nil
	}
	var behind []string
	for i := range list.Items {
		pvc := &list.Items[i]
		requested := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		actual := pvc.Status.Capacity[corev1.ResourceStorage]
		if actual.Cmp(requested) < 0 {
			behind = append(behind, pvc.Name)
		}
	}
	sort.Strings(behind)
	return behind
}

// reportResizeCompleted emits one Event on the pass that sees the last claim catch up. The edge is
// read from the condition the previous pass published, the only durable memory the operator keeps
// of "a grow was in flight": emitting on level instead would fire every pass and spend the object's
// Event budget (internal/events).
func (r *Reconciler) reportResizeCompleted(neo4j *neo4jv1beta1.Neo4j, wasResizing bool, behind []string) {
	if !wasResizing || len(behind) > 0 || r.Recorder == nil {
		return
	}
	r.Recorder.Event(neo4j, corev1.EventTypeNormal, oracle.ReasonStorageResizeCompleted.String(),
		fmt.Sprintf("every volume reached the size the spec asks for (%s)", desiredSizeSummary(neo4j)))
}

func desiredSizeSummary(neo4j *neo4jv1beta1.Neo4j) string {
	if neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil {
		return "unspecified"
	}
	data := neo4j.Spec.Storage.Volumes.Data
	if data.Mode == neo4jv1beta1.VolumeModeDynamic && data.Dynamic != nil && data.Dynamic.Size != "" {
		return "data=" + data.Dynamic.Size
	}
	return "unspecified"
}
