package persistence

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// WipeStaleMemberPVCs deletes Dynamic PVCs for pool ordinals >= keep.
// Needed after scale-in: Dropped server IDs cannot be re-enabled, so retained
// stores must not remount on the next scale-out (Neo4j: "once dropped, cannot rejoin").
func WipeStaleMemberPVCs(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, keep int32) error {
	if !hasDynamicData(neo4j) {
		return nil
	}
	stsName := render.ContextForPool(neo4j, pool).STSName()
	protected := renderstorage.ProtectedClaimNames(neo4j)

	sel := labels.SelectorFromSet(map[string]string{
		render.LabelInstance:  neo4j.Name,
		render.LabelComponent: "storage",
	})
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
func RecycleMemberStore(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j, pool render.PoolID, ordinal int32, podName string) error {
	var pod corev1.Pod
	key := types.NamespacedName{Name: podName, Namespace: neo4j.Namespace}
	if err := c.Get(ctx, key, &pod); err == nil {
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
	sel := labels.SelectorFromSet(map[string]string{
		render.LabelInstance:  neo4j.Name,
		render.LabelComponent: "storage",
	})
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
