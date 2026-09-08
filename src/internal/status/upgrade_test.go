package status

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// upgradeCR is a Standalone whose status already reports a running version, so a spec change reads
// as an upgrade rather than a first install.
func upgradeCR(running, desired string) *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: desired,
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeStandalone,
			},
		},
		Status: neo4jv1beta1.Neo4jStatus{Version: running},
	}
}

// sts builds the pool StatefulSet the writer reads back, with the revisions that decide whether
// Kubernetes considers it still rolling.
func sts(n *neo4jv1beta1.Neo4j, image string, replicas, ready, updated int32, current, update string) *appsv1.StatefulSet {
	ctx := render.ContextForPool(n, render.PoolServer)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: ctx.STSName(), Namespace: n.Namespace},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: renderwl.Neo4jContainerName, Image: image}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:   ready,
			UpdatedReplicas: updated,
			CurrentRevision: current,
			UpdateRevision:  update,
		},
	}
}

// writerFor seeds a fake API server and hands back the CR as the server holds it, so the status
// update the writer issues carries a resourceVersion the tracker agrees with.
func writerFor(n *neo4jv1beta1.Neo4j, objs ...client.Object) (*Writer, *neo4jv1beta1.Neo4j) {
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(append([]client.Object{n}, objs...)...).
		WithStatusSubresource(&neo4jv1beta1.Neo4j{}).
		Build()

	var live neo4jv1beta1.Neo4j
	if err := c.Get(context.Background(), types.NamespacedName{Name: n.Name, Namespace: n.Namespace}, &live); err != nil {
		panic(err)
	}
	live.Status = *n.Status.DeepCopy()
	return NewWriter(c), &live
}

// The point of the change: every pod can be Ready in the moment before Kubernetes takes the first
// one down. Advancing status.version there would publish a version nothing is running yet.
func TestVersionDoesNotAdvanceWhileRolling(t *testing.T) {
	n := upgradeCR("2026.05.0", "2026.07.0")
	pool := render.ContextForPool(n, render.PoolServer)
	// New image on the template, all pods Ready, but the revisions differ — the roll has not started.
	w, cr := writerFor(n, sts(n, pool.ImageRef(), 1, 1, 0, "rev-old", "rev-new"))

	if err := w.ObserveAndWrite(context.Background(), cr); err != nil {
		t.Fatalf("ObserveAndWrite: %v", err)
	}
	if cr.Status.Version != "2026.05.0" {
		t.Errorf("status.version = %q during a roll, want it to keep reporting the running 2026.05.0", cr.Status.Version)
	}
	if cr.Status.Upgrade == nil {
		t.Fatal("status.upgrade must report the version change in flight")
	}
	if got := cr.Status.Upgrade.TargetVersion; got != "2026.07.0" {
		t.Errorf("targetVersion = %q, want 2026.07.0", got)
	}
	if cr.Status.LastUpgradeTime != nil {
		t.Error("lastUpgradeTime must not be stamped while the upgrade is still running")
	}
}

// Once the roll converges the version advances, the block clears, and the completion is recorded.
func TestVersionAdvancesAndUpgradeClearsOnceConverged(t *testing.T) {
	n := upgradeCR("2026.05.0", "2026.07.0")
	n.Status.Upgrade = &neo4jv1beta1.UpgradeStatus{
		Phase:         neo4jv1beta1.UpgradePhaseRolling,
		TargetVersion: "2026.07.0",
	}
	pool := render.ContextForPool(n, render.PoolServer)
	w, cr := writerFor(n, sts(n, pool.ImageRef(), 1, 1, 1, "rev-new", "rev-new"))

	if err := w.ObserveAndWrite(context.Background(), cr); err != nil {
		t.Fatalf("ObserveAndWrite: %v", err)
	}
	if cr.Status.Version != "2026.07.0" {
		t.Errorf("status.version = %q after the roll converged, want 2026.07.0", cr.Status.Version)
	}
	if cr.Status.Upgrade != nil {
		t.Errorf("status.upgrade must clear once nothing is in flight, got phase %q", cr.Status.Upgrade.Phase)
	}
	if cr.Status.LastUpgradeTime == nil {
		t.Error("lastUpgradeTime must be stamped when an upgrade finishes")
	}
}

// A first install is not an upgrade, however the revisions look.
func TestFirstInstallReportsNoUpgrade(t *testing.T) {
	n := upgradeCR("", "2026.07.0")
	pool := render.ContextForPool(n, render.PoolServer)
	w, cr := writerFor(n, sts(n, pool.ImageRef(), 1, 1, 1, "", "rev-new"))

	if err := w.ObserveAndWrite(context.Background(), cr); err != nil {
		t.Fatalf("ObserveAndWrite: %v", err)
	}
	if cr.Status.Upgrade != nil {
		t.Errorf("a first install must not report an upgrade, got phase %q", cr.Status.Upgrade.Phase)
	}
	if cr.Status.Version != "2026.07.0" {
		t.Errorf("status.version = %q, want the installed 2026.07.0", cr.Status.Version)
	}
	if cr.Status.LastUpgradeTime != nil {
		t.Error("a first install is not an upgrade, so lastUpgradeTime must stay empty")
	}
}
