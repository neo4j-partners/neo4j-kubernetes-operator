package persistence

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
)

// WipeOnUninstall deletes operator-managed Dynamic PVCs when volumeClaimRetention.whenDeleted=Delete.
// Existing.claimName PVCs are never deleted. Returns pending=true while STS/PVCs still exist.
func WipeOnUninstall(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j) (pending bool, err error) {
	if !renderstorage.DeleteDataOnUninstall(neo4j) {
		return false, nil
	}

	sel := labels.SelectorFromSet(map[string]string{
		render.LabelInstance: neo4j.Name,
	})

	var stsList appsv1.StatefulSetList
	if err := c.List(ctx, &stsList, client.InNamespace(neo4j.Namespace), client.MatchingLabelsSelector{Selector: sel}); err != nil {
		return false, fmt.Errorf("list statefulsets for wipe: %w", err)
	}
	for i := range stsList.Items {
		sts := &stsList.Items[i]
		if err := c.Delete(ctx, sts); err != nil && !apierrors.IsNotFound(err) {
			return true, fmt.Errorf("delete statefulset %s: %w", sts.Name, err)
		}
		pending = true
	}
	if pending {
		return true, nil
	}

	protected := renderstorage.ProtectedClaimNames(neo4j)
	pvcSel := labels.SelectorFromSet(map[string]string{
		render.LabelInstance:  neo4j.Name,
		render.LabelComponent: "storage",
	})
	var pvcList corev1.PersistentVolumeClaimList
	if err := c.List(ctx, &pvcList, client.InNamespace(neo4j.Namespace), client.MatchingLabelsSelector{Selector: pvcSel}); err != nil {
		return false, fmt.Errorf("list pvcs for wipe: %w", err)
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if _, skip := protected[pvc.Name]; skip {
			continue
		}
		if err := c.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return true, fmt.Errorf("delete pvc %s: %w", pvc.Name, err)
		}
		pending = true
	}
	return pending, nil
}
