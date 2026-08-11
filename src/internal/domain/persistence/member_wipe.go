package persistence

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// WipeStaleMemberPVCs deletes Dynamic PVCs for pool ordinals >= keep.
// Needed after scale-in when whenScaled=Delete: Dropped server IDs cannot be
// re-enabled, so stores must not remount on the next scale-out.
// No-op when volumeClaimRetention.whenScaled is Retain (default) — NEO-007.
func WipeStaleMemberPVCs(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, keep int32) error {
	if !hasDynamicData(neo4j) || !renderstorage.DeleteDataOnScale(neo4j) {
		return nil
	}
	stsName := render.ContextForPool(neo4j, pool).STSName()
	protected := renderstorage.ProtectedClaimNames(neo4j)

	sel := render.StoragePVCSelector(neo4j.Name)
	var pvcList corev1.PersistentVolumeClaimList
	if err := c.List(ctx, &pvcList, client.InNamespace(neo4j.Namespace), client.MatchingLabelsSelector{Selector: sel}); err != nil {
		return fmt.Errorf("list pvcs: %w", err)
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if _, skip := protected[pvc.Name]; skip {
			continue
		}
		ord, ok := ordinalForSTS(pvc.Name, stsName)
		if !ok || ord < keep {
			continue
		}
		if err := c.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pvc %s: %w", pvc.Name, err)
		}
	}
	return nil
}

// RecycleMemberStore deletes the pod and its Dynamic PVCs so Neo4j starts a new server UUID.
// Called only when formation sees a Dropped/Deallocated identity remount (ENABLE reject /
// terminal SHOW SERVERS) — a heal, not scale-in wipe. Allowed under whenScaled:Retain
// (NEO-007): Retain means "don't wipe on scale-in"; a Dropped store cannot rejoin, so
// recycling that ordinal is recovery, not a false promise of reversible scale.
func RecycleMemberStore(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, ordinal int32, podName string) error {
	var pod corev1.Pod
	key := types.NamespacedName{Name: podName, Namespace: neo4j.Namespace}
	if err := c.Get(ctx, key, &pod); err == nil {
		if !render.HasOperandLabels(&pod, neo4j.Name) {
			return fmt.Errorf("refuse recycle of pod %s: missing operator provenance labels (ADD-04)", podName)
		}
		if err := c.Delete(ctx, &pod); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pod %s: %w", podName, err)
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	if !hasDynamicData(neo4j) {
		return nil
	}
	stsName := render.ContextForPool(neo4j, pool).STSName()
	protected := renderstorage.ProtectedClaimNames(neo4j)
	sel := render.StoragePVCSelector(neo4j.Name)
	var pvcList corev1.PersistentVolumeClaimList
	if err := c.List(ctx, &pvcList, client.InNamespace(neo4j.Namespace), client.MatchingLabelsSelector{Selector: sel}); err != nil {
		return fmt.Errorf("list pvcs: %w", err)
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if _, skip := protected[pvc.Name]; skip {
			continue
		}
		ord, ok := ordinalForSTS(pvc.Name, stsName)
		if !ok || ord != ordinal {
			continue
		}
		if err := c.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pvc %s: %w", pvc.Name, err)
		}
	}
	return nil
}

func hasDynamicData(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Storage != nil &&
		neo4j.Spec.Storage.Volumes != nil &&
		neo4j.Spec.Storage.Volumes.Data.Mode == neo4jv1beta1.VolumeModeDynamic
}

// ordinalForSTS parses "{volume}-{stsName}-{ordinal}" (stsName may contain hyphens).
func ordinalForSTS(pvcName, stsName string) (int32, bool) {
	marker := "-" + stsName + "-"
	i := strings.LastIndex(pvcName, marker)
	if i < 0 {
		return 0, false
	}
	suf := pvcName[i+len(marker):]
	n, err := strconv.ParseInt(suf, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}
