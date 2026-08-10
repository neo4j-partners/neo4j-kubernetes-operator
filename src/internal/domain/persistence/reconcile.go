package persistence

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// Reconciler validates storage spec and reports the PVC/volume plan.
// Dynamic PVCs are created by the StatefulSet controller from volumeClaimTemplates;
// Existing modes bind claimName / raw volumes (no operator-owned PVC create).
type Reconciler struct {
	Client client.Client
}

func New(c client.Client) *Reconciler { return &Reconciler{Client: c} }

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	if err := renderstorage.Validate(neo4j); err != nil {
		log.Error(err, "storage validation failed")
		return shared.Failed(err)
	}

	// ADD-06: pin whenDeleted at first reconcile so a late Delete cannot arm wipe-on-uninstall.
	if renderstorage.PinWhenDeleted(neo4j) {
		if r.Client != nil {
			if err := r.Client.Status().Update(ctx, neo4j); err != nil {
				return shared.Failed(fmt.Errorf("pin volumeClaimRetentionWhenDeleted: %w", err))
			}
		}
		log.Info("pinned volumeClaimRetentionWhenDeleted",
			"whenDeleted", string(*neo4j.Status.VolumeClaimRetentionWhenDeleted))
	} else if neo4j.Status.VolumeClaimRetentionWhenDeleted != nil &&
		renderstorage.SpecWhenDeleted(neo4j) == neo4jv1beta1.VolumeClaimRetentionDelete &&
		*neo4j.Status.VolumeClaimRetentionWhenDeleted != neo4jv1beta1.VolumeClaimRetentionDelete {
		log.Info("ignoring late whenDeleted=Delete; uninstall wipe remains pinned",
			"pinned", string(*neo4j.Status.VolumeClaimRetentionWhenDeleted))
	}

	for _, pool := range render.ActivePools(neo4j) {
		ctxRender := render.ContextForPool(neo4j, pool)
		logDataPlan(log, ctxRender)
		logAuxPlans(log, ctxRender)
		if err := r.observeDataPVC(ctx, ctxRender); err != nil {
			log.Error(err, "storage observe failed", "pool", string(pool))
			return shared.Failed(err)
		}
	}
	return shared.Done()
}

func logDataPlan(log logr.Logger, ctxRender render.Context) {
	if ctxRender.Neo4j.Spec.Storage == nil || ctxRender.Neo4j.Spec.Storage.Volumes == nil {
		log.Info("storage plan", "pool", string(ctxRender.Pool), "volume", "data", "note", "no storage.spec — using defaults at render time")
		return
	}
	data := ctxRender.Neo4j.Spec.Storage.Volumes.Data
	keys := []any{
		"pool", string(ctxRender.Pool),
		"volume", "data",
		"mode", string(data.Mode),
		"sts", ctxRender.STSName(),
	}
	switch data.Mode {
	case neo4jv1beta1.VolumeModeDynamic:
		size, sc := "", ""
		if data.Dynamic != nil {
			size = data.Dynamic.Size
			sc = data.Dynamic.StorageClassName
		}
		pvc, _ := renderstorage.DataPVCLookup(ctxRender)
		keys = append(keys,
			"size", size,
			"storageClass", sc,
			"pvc", pvc,
			"provisioning", "StatefulSet volumeClaimTemplate (controller creates PVC)",
		)
	case neo4jv1beta1.VolumeModeExisting:
		if data.Existing != nil && data.Existing.ClaimName != "" {
			keys = append(keys, "pvc", data.Existing.ClaimName, "binding", "existing claimName mount")
		} else if data.Existing != nil && data.Existing.Volume != nil {
			keys = append(keys, "binding", "existing raw VolumeSource (no PVC)")
		} else if data.Existing != nil && data.Existing.VolumeClaimTemplate != nil {
			pvc, _ := renderstorage.DataPVCLookup(ctxRender)
			keys = append(keys, "pvc", pvc, "binding", "existing volumeClaimTemplate")
		} else {
			keys = append(keys, "binding", "existing (unspecified)")
		}
	default:
		keys = append(keys, "note", fmt.Sprintf("mode %q", data.Mode))
	}
	log.Info("storage plan", keys...)
}

func logAuxPlans(log logr.Logger, ctxRender render.Context) {
	if ctxRender.Neo4j.Spec.Storage == nil || ctxRender.Neo4j.Spec.Storage.Volumes == nil {
		return
	}
	vols := ctxRender.Neo4j.Spec.Storage.Volumes
	for _, item := range []struct {
		name string
		aux  *neo4jv1beta1.AuxiliaryVolumeSpec
	}{
		{"backups", vols.Backups},
		{"logs", vols.Logs},
		{"metrics", vols.Metrics},
		{"import", vols.Import},
		{"licenses", vols.Licenses},
		{"plugins", vols.Plugins},
	} {
		if item.aux == nil {
			continue
		}
		mode := item.aux.Mode
		if mode == "" {
			mode = neo4jv1beta1.VolumeModeShare
		}
		log.V(1).Info("storage aux plan",
			"pool", string(ctxRender.Pool),
			"volume", item.name,
			"mode", string(mode),
		)
	}
}

func (r *Reconciler) observeDataPVC(ctx context.Context, ctxRender render.Context) error {
	log := ctrllog.FromContext(ctx)
	pvcName, ok := renderstorage.DataPVCLookup(ctxRender)
	if !ok {
		log.Info("storage observe",
			"pool", string(ctxRender.Pool),
			"volume", "data",
			"pvc", "(none)",
			"phase", "n/a",
			"note", "no PVC to track for this data mode",
		)
		return nil
	}
	if r.Client == nil {
		return nil
	}
	var pvc corev1.PersistentVolumeClaim
	err := r.Client.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: ctxRender.Namespace()}, &pvc)
	if apierrors.IsNotFound(err) {
		log.Info("storage observe",
			"pool", string(ctxRender.Pool),
			"volume", "data",
			"pvc", pvcName,
			"phase", "Missing",
			"note", "PVC not created yet (Dynamic: wait for STS; Existing: create/bind the claim)",
		)
		return nil
	}
	if err != nil {
		return err
	}
	capacity := ""
	if q, has := pvc.Status.Capacity[corev1.ResourceStorage]; has {
		capacity = q.String()
	}
	sc := ""
	if pvc.Spec.StorageClassName != nil {
		sc = *pvc.Spec.StorageClassName
	}
	log.Info("storage observe",
		"pool", string(ctxRender.Pool),
		"volume", "data",
		"pvc", pvcName,
		"phase", string(pvc.Status.Phase),
		"capacity", capacity,
		"storageClass", sc,
		"volumeName", pvc.Spec.VolumeName,
	)
	return nil
}
