package shared

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// clusterNoSecondaries is a Cluster CR with 3 primaries and no secondaries, so analytics and read
// are "removed" pools.
func clusterNoSecondaries() *neo4jv1.Neo4j {
	return &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode:      neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{Members: 3},
			},
		},
	}
}

func poolService(name, pool string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: "default",
		Labels: map[string]string{
			render.LabelInstance:  "prod",
			render.LabelManagedBy: render.ManagedByValue,
			render.LabelPool:      pool,
		},
	}}
}

func pruneScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Once a removed pool's StatefulSet is gone, its labelled Services are deleted; the primary pool's
// Services and any Service lacking the pool label survive.
func TestPruneRemovedPoolObjectsDeletesOrphans(t *testing.T) {
	s := pruneScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		clusterNoSecondaries(),
		poolService("prod-analytics", "analytics"),
		poolService("prod-analytics-0", "analytics"),
		poolService("prod-primary", "primary"),
	).Build()

	if err := PruneRemovedPoolObjects(t.Context(), c, clusterNoSecondaries(), &corev1.ServiceList{}); err != nil {
		t.Fatal(err)
	}

	get := func(name string) error {
		return c.Get(t.Context(), types.NamespacedName{Name: name, Namespace: "default"}, &corev1.Service{})
	}
	if err := get("prod-analytics"); !apierrors.IsNotFound(err) {
		t.Fatalf("analytics headless Service should be pruned, get err = %v", err)
	}
	if err := get("prod-analytics-0"); !apierrors.IsNotFound(err) {
		t.Fatalf("analytics member Service should be pruned, get err = %v", err)
	}
	if err := get("prod-primary"); err != nil {
		t.Fatalf("primary Service must survive a secondary-pool prune: %v", err)
	}
}

// While the removed pool still has a StatefulSet, the prune waits — it must not race formation's
// drain/DROP by pulling the pool's discovery Services out from under a leaving member.
func TestPruneRemovedPoolObjectsWaitsForStatefulSetDeletion(t *testing.T) {
	s := pruneScheme(t)
	replicas := int32(1)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		clusterNoSecondaries(),
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "prod-analytics", Namespace: "default"},
			Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
		},
		poolService("prod-analytics", "analytics"),
	).Build()

	if err := PruneRemovedPoolObjects(t.Context(), c, clusterNoSecondaries(), &corev1.ServiceList{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), types.NamespacedName{Name: "prod-analytics", Namespace: "default"}, &corev1.Service{}); err != nil {
		t.Fatalf("Service must not be pruned while the pool StatefulSet still exists: %v", err)
	}
}
