package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// standaloneCMTestReconciler wires a reconciler around a fake client for the
// ConfigMap-reconcile tests below.
func standaloneCMTestReconciler(t *testing.T) (*Neo4jEnterpriseStandaloneReconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &Neo4jEnterpriseStandaloneReconciler{Client: c, Scheme: scheme}, c
}

func standaloneForConf(config map[string]string) *neo4jv1beta1.Neo4jEnterpriseStandalone {
	return &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Config: config,
		},
	}
}

func renderedConf(t *testing.T, c client.Client) string {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	return cm.Data["neo4j.conf"]
}

// TestReconcileConfigMap_RemovedKeyClears is the core regression for issue #151:
// removing a key from spec.config must clear it from the rendered ConfigMap on a
// subsequent reconcile (the old empty-mutate CreateOrUpdate wrote stale data back,
// so removed keys persisted until the ConfigMap was deleted).
func TestReconcileConfigMap_RemovedKeyClears(t *testing.T) {
	r, c := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForConf(map[string]string{"foo.bar": "x", "db.transaction.timeout": "30s"})
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if got := renderedConf(t, c); !strings.Contains(got, "foo.bar=x") {
		t.Fatalf("expected foo.bar=x after first reconcile, conf:\n%s", got)
	}

	// User removes foo.bar from spec.config.
	sa.Spec.Config = map[string]string{"db.transaction.timeout": "30s"}
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	got := renderedConf(t, c)
	if strings.Contains(got, "foo.bar") {
		t.Errorf("foo.bar should be removed from the ConfigMap after deletion from spec.config; conf:\n%s", got)
	}
	if !strings.Contains(got, "db.transaction.timeout=30s") {
		t.Errorf("retained key db.transaction.timeout should still be present; conf:\n%s", got)
	}
}

// TestReconcileConfigMap_ChangedValuePropagates: editing a scalar value in
// spec.config must update the rendered ConfigMap (not be masked by add-if-absent).
func TestReconcileConfigMap_ChangedValuePropagates(t *testing.T) {
	r, c := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForConf(map[string]string{"db.transaction.timeout": "30s"})
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	sa.Spec.Config = map[string]string{"db.transaction.timeout": "90s"}
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	got := renderedConf(t, c)
	if !strings.Contains(got, "db.transaction.timeout=90s") {
		t.Errorf("changed value 90s should win; conf:\n%s", got)
	}
	if strings.Contains(got, "db.transaction.timeout=30s") {
		t.Errorf("stale value 30s should be gone; conf:\n%s", got)
	}
}

// TestReconcileConfigMap_PreservesForeignKeys: a key merged into the ConfigMap by
// another controller (e.g. the Neo4jPlugin controller upserting plugin settings)
// must survive a standalone reconcile — the operator only owns the keys it
// renders, tracked via the ownership annotation.
func TestReconcileConfigMap_PreservesForeignKeys(t *testing.T) {
	r, c := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForConf(map[string]string{"db.transaction.timeout": "30s"})
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}

	// Simulate the plugin controller adding a foreign key (it does not touch the
	// ownership annotation).
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("get: %v", err)
	}
	cm.Data["neo4j.conf"] += "\ndbms.bloom.license_file=/licenses/bloom.license\n"
	if err := c.Update(ctx, cm); err != nil {
		t.Fatalf("simulate plugin update: %v", err)
	}

	// Standalone reconciles again (spec unchanged).
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	got := renderedConf(t, c)
	if !strings.Contains(got, "dbms.bloom.license_file=/licenses/bloom.license") {
		t.Errorf("foreign (plugin) key should be preserved across standalone reconcile; conf:\n%s", got)
	}
	if !strings.Contains(got, "db.transaction.timeout=30s") {
		t.Errorf("operator key should still be present; conf:\n%s", got)
	}
}

// TestReconcileConfigMap_AdditiveForeignTokensPreserved: when a plugin unions
// tokens into an additive key the operator also owns (procedures.unrestricted),
// both the operator and plugin tokens must survive — and stay stable across
// reconciles (no tug-of-war / oscillation).
func TestReconcileConfigMap_AdditiveForeignTokensPreserved(t *testing.T) {
	r, c := standaloneCMTestReconciler(t)
	ctx := context.Background()

	sa := standaloneForConf(map[string]string{"dbms.security.procedures.unrestricted": "gds.*"})
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	// Plugin unions apoc.* into the additive key.
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm); err != nil {
		t.Fatalf("get: %v", err)
	}
	cm.Data["neo4j.conf"] = strings.ReplaceAll(cm.Data["neo4j.conf"],
		"dbms.security.procedures.unrestricted=gds.*",
		"dbms.security.procedures.unrestricted=gds.*,apoc.*")
	if err := c.Update(ctx, cm); err != nil {
		t.Fatalf("simulate plugin union: %v", err)
	}

	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	got := renderedConf(t, c)
	if !strings.Contains(got, "apoc.*") || !strings.Contains(got, "gds.*") {
		t.Errorf("both operator (gds.*) and plugin (apoc.*) tokens should survive; conf:\n%s", got)
	}
	// And the key must still be single-declared (no duplicate line).
	if n := strings.Count(got, "dbms.security.procedures.unrestricted="); n != 1 {
		t.Errorf("procedures.unrestricted should be declared exactly once, got %d; conf:\n%s", n, got)
	}
}
