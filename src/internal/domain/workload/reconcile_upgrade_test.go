package workload

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendercfg "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
)

// clusterWithReadPool is the topology the ordering rule applies to: the read pool is rendered with
// server.cluster.system_database_mode=SECONDARY, which is what Neo4j's "secondaries before the
// system primary" rule names.
func clusterWithReadPool(running, desired string) *neo4jv1beta1.Neo4j {
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: desired,
			License: &neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Read: &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
				},
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
	n.Status.Version = running
	return n
}

func stsFor(t *testing.T, c client.Client, n *neo4jv1beta1.Neo4j, pool render.PoolID) *appsv1.StatefulSet {
	t.Helper()
	ctx := render.ContextForPool(n, pool)
	sts := &appsv1.StatefulSet{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: ctx.STSName(), Namespace: n.Namespace}, sts); err != nil {
		t.Fatalf("get %s statefulset: %v", pool, err)
	}
	return sts
}

// HoldPrimaries is unit-tested as a pure function elsewhere. What this covers is that the pool loop
// actually consults it — delete the branch in Reconcile and every one of those tests still passes
// while the operator upgrades primaries and secondaries together.
func TestPrimaryPoolKeepsItsImageWhileSecondariesLag(t *testing.T) {
	s := workloadScheme(t)
	n := clusterWithReadPool("2026.05.0", "2026.05.0")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).WithStatusSubresource(n).Build()
	r := New(c, s)

	// Install both pools at the running version.
	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("initial reconcile: %v", out.Err)
	}
	oldImage := poolImage(*stsFor(t, c, n, render.PoolPrimary))
	if oldImage == "" {
		t.Fatal("primary pool has no neo4j container")
	}

	// Ask for a new version. The read pool's StatefulSet reports nothing yet, so it cannot have
	// converged — the primary pool must not be given the new image on this pass.
	n.Spec.Version = "2026.07.1"
	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("reconcile after the version change: %v", out.Err)
	}

	primary := stsFor(t, c, n, render.PoolPrimary)
	if got := poolImage(*primary); got != oldImage {
		t.Errorf("primary pool moved to %q while the read pool was still behind; want it held on %q", got, oldImage)
	}
	readPool := stsFor(t, c, n, render.PoolRead)
	if got := poolImage(*readPool); got == oldImage {
		t.Errorf("read pool was held too: image %q unchanged. Only the primaries wait", got)
	}
}

// The counterpart: once the read pool is on the target image and fully back, the primaries are
// released. A hold that never lifts is as broken as one that never holds.
func TestPrimaryPoolIsReleasedOnceSecondariesConverge(t *testing.T) {
	s := workloadScheme(t)
	n := clusterWithReadPool("2026.05.0", "2026.05.0")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).WithStatusSubresource(n).Build()
	r := New(c, s)

	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("initial reconcile: %v", out.Err)
	}
	oldImage := poolImage(*stsFor(t, c, n, render.PoolPrimary))

	n.Spec.Version = "2026.07.1"
	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("reconcile after the version change: %v", out.Err)
	}

	// Report the read pool as finished: on the new image, every member updated and ready, and no
	// revision left in flight. The fake client does not run controllers, so status is ours to set.
	readPool := stsFor(t, c, n, render.PoolRead)
	readPool.Status = appsv1.StatefulSetStatus{
		Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1,
		CurrentRevision: "rev-new", UpdateRevision: "rev-new",
		ObservedGeneration: readPool.Generation,
	}
	if err := c.Status().Update(t.Context(), readPool); err != nil {
		t.Fatalf("set read pool status: %v", err)
	}

	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("reconcile after the read pool converged: %v", out.Err)
	}
	primary := stsFor(t, c, n, render.PoolPrimary)
	if got := poolImage(*primary); got == oldImage {
		t.Errorf("primary pool is still on %q after the read pool finished; the hold never lifted", got)
	}
}

// A configuration change must roll every pool as it always has. If the hold leaked into changes
// where the versions agree, it would silently delay config rollouts that are already covered by
// assert/cluster-config-restart.
func TestConfigChangeIsNotHeld(t *testing.T) {
	s := workloadScheme(t)
	n := clusterWithReadPool("2026.05.0", "2026.05.0")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).WithStatusSubresource(n).Build()
	r := New(c, s)

	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("initial reconcile: %v", out.Err)
	}
	before := stsFor(t, c, n, render.PoolPrimary).Spec.Template.Annotations

	// Same version, different config — the roll every other suite relies on.
	n.Spec.Config = &neo4jv1beta1.ConfigSpec{
		Neo4j: map[string]string{"db.transaction.timeout": "37s"},
	}
	if out := r.Reconcile(t.Context(), n); out.Err != nil {
		t.Fatalf("reconcile after the config change: %v", out.Err)
	}
	after := stsFor(t, c, n, render.PoolPrimary).Spec.Template.Annotations
	if after[rendercfg.ConfigChecksumAnnotation] == before[rendercfg.ConfigChecksumAnnotation] {
		t.Error("the primary pool's config checksum did not change: a config roll was held, but only version changes are ordered")
	}
}
