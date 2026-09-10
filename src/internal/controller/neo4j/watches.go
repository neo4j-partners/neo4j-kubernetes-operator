package neo4j

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// mapSecretToNeo4j enqueues CRs that mount this Secret. cert-manager TLS Secrets
// are owned by the Certificate, not the Neo4j CR, so Owns(Secret) never sees them
// (ADR-009). The mountable label keeps ServiceAccount tokens out of the queue.
func (r *Neo4jReconciler) mapSecretToNeo4j(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	if secret.Labels == nil || secret.Labels[rendersecrets.MountableLabel] != rendersecrets.MountableLabelValue {
		return nil
	}
	var list neo4jv1beta1.Neo4jList
	if err := r.List(ctx, &list, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		n := &list.Items[i]
		if !crMountsSecret(n, secret.Name) {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: n.Name, Namespace: n.Namespace},
		})
	}
	return reqs
}

// mapPVCToNeo4j enqueues the CR whose storage this claim backs. Neither storage mode gives the
// claim an ownerRef to the CR — a Dynamic claim is created by the StatefulSet controller, which
// sets none, and an Existing.claimName claim predates the CR entirely — so Owns(PVC) never fires
// and this mapper is the only path (ADR-009).
//
// The two modes need two paths: a Dynamic claim carries operator provenance labels, a BYO claim
// carries nothing the operator put there and can only be recognised by name.
func (r *Neo4jReconciler) mapPVCToNeo4j(ctx context.Context, obj client.Object) []reconcile.Request {
	if _, ok := obj.(*corev1.PersistentVolumeClaim); !ok {
		return nil
	}
	provenance := obj.GetLabels()
	if name := provenance[render.LabelInstance]; name != "" &&
		provenance[render.LabelComponent] == render.ComponentStorage &&
		render.HasOperandLabels(obj, name) {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{Name: name, Namespace: obj.GetNamespace()},
		}}
	}
	// Unlabelled: possibly a claim some CR binds by name. Costs one cached List, and only on the
	// claims the label path already rejected.
	var list neo4jv1beta1.Neo4jList
	if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		n := &list.Items[i]
		if !crUsesClaim(n, obj.GetName()) {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: n.Name, Namespace: n.Namespace},
		})
	}
	return reqs
}

// crUsesClaim reports whether the CR binds this claim by name. ProtectedClaimNames is exactly the
// Existing.claimName set, which is the same set as "claims the operator never renders".
func crUsesClaim(neo4j *neo4jv1beta1.Neo4j, name string) bool {
	_, ok := renderstorage.ProtectedClaimNames(neo4j)[name]
	return ok
}

// pvcStatusChanged narrows the claim watch to the transitions StorageReady reads: the bind, and
// capacity catching up with the request once an expansion lands. A bind writes several annotations
// on the same claim, and each would otherwise re-run a pipeline that opens Bolt sessions in Cluster
// mode (ADR-003). Create and Delete stay enabled, since a claim a scale-out creates is born at the
// StatefulSet template's older size and has to be grown on sight.
func pvcStatusChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			before, okBefore := e.ObjectOld.(*corev1.PersistentVolumeClaim)
			after, okAfter := e.ObjectNew.(*corev1.PersistentVolumeClaim)
			if !okBefore || !okAfter {
				return true
			}
			if before.Status.Phase != after.Status.Phase {
				return true
			}
			sizeBefore := before.Status.Capacity[corev1.ResourceStorage]
			sizeAfter := after.Status.Capacity[corev1.ResourceStorage]
			if sizeBefore.Cmp(sizeAfter) != 0 {
				return true
			}
			// FileSystemResizePending and friends: the claim reports expansion progress here.
			return !equality.Semantic.DeepEqual(before.Status.Conditions, after.Status.Conditions)
		},
	}
}

func crMountsSecret(neo4j *neo4jv1beta1.Neo4j, name string) bool {
	if rendertrust.ReferencesSecret(neo4j, name) {
		return true
	}
	for _, n := range rendersecrets.ReferencedMountSecrets(neo4j) {
		if n == name {
			return true
		}
	}
	return false
}
