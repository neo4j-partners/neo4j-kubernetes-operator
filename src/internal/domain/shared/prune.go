package shared

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// PruneRemovedPoolObjects deletes operator-owned objects that belong to a secondary pool the spec
// no longer declares, once formation has torn the pool's StatefulSet down. Each reconciler calls it
// with the list type for the kind it owns (connectivity with a ServiceList, serverconfig with a
// ConfigMapList).
//
// Gated two ways on purpose:
//   - Only pools in render.RemovedPools — a key left topology.secondaries.
//   - Only once the pool's StatefulSet is gone, so the prune never races the drain/DROP. The
//     per-member internals Services feed Neo4j's K8S discovery; pulling them while a member is
//     still leaving could perturb an in-progress DEALLOCATE.
//
// List + per-object Delete, deliberately not client.DeleteAllOf: DeleteAllOf needs the
// `deletecollection` verb, which the operator's Role does not grant (it has get/list/watch/create/
// update/patch/delete). The {instance, managed-by, pool} selector means a call can only ever match
// this CR's own objects, for that one pool, of that one kind — the StatefulSet, the PVCs, and every
// other pool are out of reach, and the managed-by label enforces provenance (ADD-04). Driven by
// persistent state, it re-runs harmlessly every reconcile until the objects are gone, then no-ops.
//
// ponytail: steady-state cost for a cluster that never declares secondaries is one StatefulSet Get
// plus one no-op List per removed pool per reconcile. Cheap, not zero.
func PruneRemovedPoolObjects(ctx context.Context, c client.Client, neo4j *neo4jv1.Neo4j, list client.ObjectList) error {
	for _, pool := range render.RemovedPools(neo4j) {
		ctxPool := render.ContextForPool(neo4j, pool)
		var sts appsv1.StatefulSet
		stsKey := client.ObjectKey{Namespace: ctxPool.Namespace(), Name: ctxPool.STSName()}
		switch err := c.Get(ctx, stsKey, &sts); {
		case err == nil:
			continue // teardown still in flight — do not race the drain
		case !apierrors.IsNotFound(err):
			return err
		}
		sel := client.MatchingLabels{
			render.LabelInstance:  neo4j.Name,
			render.LabelManagedBy: render.ManagedByValue,
			render.LabelPool:      string(pool),
		}
		if err := c.List(ctx, list, client.InNamespace(neo4j.Namespace), sel); err != nil {
			return err
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return err
		}
		for _, item := range items {
			obj, ok := item.(client.Object)
			if !ok {
				continue
			}
			if err := c.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}
