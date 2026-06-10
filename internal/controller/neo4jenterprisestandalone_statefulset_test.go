package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// standaloneForSTS builds a minimal standalone with the fields createStatefulSet
// needs (Image + Storage.Size — the latter is MustParse'd so it can't be empty).
func standaloneForSTS(tag string) *neo4jv1beta1.Neo4jEnterpriseStandalone {
	return &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: tag},
			Storage: neo4jv1beta1.StorageSpec{Size: "1Gi"},
		},
	}
}

func getSTS(t *testing.T, r *Neo4jEnterpriseStandaloneReconciler) *appsv1.StatefulSet {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "sa", Namespace: "default"}, sts); err != nil {
		t.Fatalf("get statefulset: %v", err)
	}
	return sts
}

func neo4jContainerImage(sts *appsv1.StatefulSet) string {
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name == "neo4j" {
			return c.Image
		}
	}
	return ""
}

// TestReconcileStatefulSet_ImageUpgradeApplies is the core regression for the
// latent empty-mutate bug: changing spec.Image.Tag must roll the new image to a
// running standalone. The old `CreateOrUpdate(..., func() error { return nil })`
// let Get write the stale stored template back, so image upgrades silently never
// applied (the upgrade event fired but no upgrade happened).
func TestReconcileStatefulSet_ImageUpgradeApplies(t *testing.T) {
	r, _ := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForSTS("5.26.0-enterprise")
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if got := neo4jContainerImage(getSTS(t, r)); got != "neo4j:5.26.0-enterprise" {
		t.Fatalf("after create, image = %q, want neo4j:5.26.0-enterprise", got)
	}

	// User bumps the tag.
	sa.Spec.Image.Tag = "2025.01.0-enterprise"
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if got := neo4jContainerImage(getSTS(t, r)); got != "neo4j:2025.01.0-enterprise" {
		t.Errorf("after upgrade, image = %q, want neo4j:2025.01.0-enterprise", got)
	}
}

// TestReconcileStatefulSet_ResourcesChangeApplies: a resources edit must reach a
// running standalone (another field the empty mutate silently dropped).
func TestReconcileStatefulSet_ResourcesChangeApplies(t *testing.T) {
	r, _ := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForSTS("5.26.0-enterprise")
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}

	sa.Spec.Resources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
	}
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	sts := getSTS(t, r)
	got := sts.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]
	if got.String() != "2Gi" {
		t.Errorf("memory request = %q, want 2Gi", got.String())
	}
}

// TestReconcileStatefulSet_PreservesForeignEnvVars: an env var patched onto the
// StatefulSet by another controller (the plugin controller adds NEO4J_PLUGINS
// directly) must survive a template-changing reconcile. Mirrors the cluster
// controller's mergeEnvVars + ownership-annotation contract.
func TestReconcileStatefulSet_PreservesForeignEnvVars(t *testing.T) {
	r, _ := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForSTS("5.26.0-enterprise")
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}

	// Simulate the plugin controller patching NEO4J_PLUGINS onto the container.
	sts := getSTS(t, r)
	sts.Spec.Template.Spec.Containers[0].Env = append(sts.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "NEO4J_PLUGINS", Value: `["apoc"]`})
	if err := r.Update(ctx, sts); err != nil {
		t.Fatalf("patch foreign env: %v", err)
	}

	// A template-changing reconcile (image bump) must keep the foreign var.
	sa.Spec.Image.Tag = "2025.01.0-enterprise"
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	sts = getSTS(t, r)
	if got := neo4jContainerImage(sts); got != "neo4j:2025.01.0-enterprise" {
		t.Errorf("image not upgraded: %q", got)
	}
	if !hasEnvVar(sts.Spec.Template.Spec.Containers[0].Env, "NEO4J_PLUGINS", `["apoc"]`) {
		t.Errorf("foreign NEO4J_PLUGINS env var was clobbered: %+v", sts.Spec.Template.Spec.Containers[0].Env)
	}
}

// TestReconcileStatefulSet_IdempotentNoChurn: reconciling with no spec change
// must not write the StatefulSet again (no ResourceVersion bump → no pod roll).
// The template-hash gate is what prevents API-server field defaulting from
// making every reconcile look like a change.
func TestReconcileStatefulSet_IdempotentNoChurn(t *testing.T) {
	r, _ := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForSTS("5.26.0-enterprise")
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	rv1 := getSTS(t, r).ResourceVersion

	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	rv2 := getSTS(t, r).ResourceVersion
	if rv1 != rv2 {
		t.Errorf("StatefulSet churned on a no-op reconcile: rv %s -> %s", rv1, rv2)
	}
}

// TestReconcileStatefulSet_PreservesConfigRestartAnnotation: the conf-path roll
// stamp (neo4j.com/config-restarted-at, applied by restartStandalonePod after a
// neo4j.conf change) lives on the pod-template annotations. A later template
// apply (image bump) must NOT wipe it, or the conf-driven pod roll would be
// undone within the same reconcile loop.
func TestReconcileStatefulSet_PreservesConfigRestartAnnotation(t *testing.T) {
	r, _ := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForSTS("5.26.0-enterprise")
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}

	// Simulate the conf path stamping a restart annotation on the template.
	sts := getSTS(t, r)
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = map[string]string{}
	}
	sts.Spec.Template.Annotations["neo4j.com/config-restarted-at"] = "2026-06-10T00:00:00Z"
	if err := r.Update(ctx, sts); err != nil {
		t.Fatalf("stamp restart annotation: %v", err)
	}

	sa.Spec.Image.Tag = "2025.01.0-enterprise"
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	sts = getSTS(t, r)
	if sts.Spec.Template.Annotations["neo4j.com/config-restarted-at"] != "2026-06-10T00:00:00Z" {
		t.Errorf("config-restart annotation was wiped by the template apply: %+v", sts.Spec.Template.Annotations)
	}
}

// TestReconcileStatefulSet_RemovedOwnedEnvVarDropped: a var the operator owned
// last reconcile but no longer renders must be removed (the removal half of the
// ownership contract), without disturbing foreign vars.
func TestReconcileStatefulSet_RemovedOwnedEnvVarDropped(t *testing.T) {
	r, _ := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForSTS("5.26.0-enterprise")
	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}

	// Forge an owned-env annotation claiming the operator rendered OWNED_GONE,
	// and put it (plus a foreign var) on the container — as if a prior version
	// had rendered it. The next reconcile's desired set won't contain it.
	sts := getSTS(t, r)
	sts.Spec.Template.Spec.Containers[0].Env = append(sts.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "OWNED_GONE", Value: "stale"},
		corev1.EnvVar{Name: "NEO4J_PLUGINS", Value: `["apoc"]`})
	existing := sts.Annotations[standaloneOwnedEnvVarsAnnotation]
	sts.Annotations[standaloneOwnedEnvVarsAnnotation] = existing + ",OWNED_GONE"
	// Force the template hash stale so the apply branch runs.
	sts.Annotations[standaloneTemplateHashAnnotation] = "stale"
	if err := r.Update(ctx, sts); err != nil {
		t.Fatalf("seed owned/foreign env: %v", err)
	}

	if err := r.reconcileStatefulSet(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	sts = getSTS(t, r)
	env := sts.Spec.Template.Spec.Containers[0].Env
	if hasEnvVarName(env, "OWNED_GONE") {
		t.Errorf("previously-owned OWNED_GONE should have been dropped: %+v", env)
	}
	if !hasEnvVar(env, "NEO4J_PLUGINS", `["apoc"]`) {
		t.Errorf("foreign NEO4J_PLUGINS should have been preserved: %+v", env)
	}
}

func hasEnvVar(env []corev1.EnvVar, name, value string) bool {
	for _, e := range env {
		if e.Name == name && e.Value == value {
			return true
		}
	}
	return false
}

func hasEnvVarName(env []corev1.EnvVar, name string) bool {
	for _, e := range env {
		if e.Name == name {
			return true
		}
	}
	return false
}
