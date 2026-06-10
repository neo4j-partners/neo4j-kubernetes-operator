package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

// TestReconcileConfigMap_UserAndPluginAdditiveUnioned: a user's spec.config
// allowlist and a plugin's additive allowlist are BOTH operator-owned and are
// unioned in the rendered conf (single declaration), and the result is stable
// across reconciles (no churn). This replaces the old "foreign tokens" case —
// after #146 plugin tokens flow through the single owner (a plugin CR), not an
// external patch to the ConfigMap.
func TestReconcileConfigMap_UserAndPluginAdditiveUnioned(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	gds := pluginCR("gds-plugin", "gds", "sa", true, nil)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gds).Build()
	r := &Neo4jEnterpriseStandaloneReconciler{Client: c, Scheme: scheme}
	ctx := context.Background()

	sa := standaloneForConf(map[string]string{"dbms.security.procedures.unrestricted": "myapp.*"})
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	got := renderedConf(t, c)
	for _, want := range []string{"myapp.*", "gds.*", "apoc.load.*"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected user + plugin token %q unioned; conf:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "dbms.security.procedures.unrestricted="); n != 1 {
		t.Errorf("procedures.unrestricted should be declared exactly once, got %d; conf:\n%s", n, got)
	}

	// Stable across a repeat reconcile (no churn).
	cm1 := &corev1.ConfigMap{}
	_ = c.Get(ctx, types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm1)
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	cm2 := &corev1.ConfigMap{}
	_ = c.Get(ctx, types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm2)
	if cm1.ResourceVersion != cm2.ResourceVersion {
		t.Errorf("ConfigMap churned on repeat reconcile (rv %s -> %s)", cm1.ResourceVersion, cm2.ResourceVersion)
	}
}

// TestReconcileConfigMap_Idempotent guards the #1 regression risk of the #151
// fix: re-rendering on every reconcile must NOT churn the ConfigMap, or the
// config-hash would change each loop and roll the pod perpetually. A second
// reconcile with unchanged spec must not bump the ConfigMap's resourceVersion
// (controllerutil.CreateOrUpdate skips the Update when nothing changed).
func TestReconcileConfigMap_Idempotent(t *testing.T) {
	r, c := standaloneCMTestReconciler(t)
	ctx := context.Background()
	sa := standaloneForConf(map[string]string{
		"dbms.security.procedures.unrestricted": "gds.*,apoc.*",
		"db.transaction.timeout":                "30s",
	})

	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	cm1 := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm1); err != nil {
		t.Fatalf("get 1: %v", err)
	}

	// Reconcile twice more with the SAME spec.
	for i := 0; i < 2; i++ {
		if err := r.reconcileConfigMap(ctx, sa); err != nil {
			t.Fatalf("reconcile repeat %d: %v", i, err)
		}
	}
	cm2 := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: "sa-config", Namespace: "default"}, cm2); err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if cm1.ResourceVersion != cm2.ResourceVersion {
		t.Errorf("ConfigMap changed on repeat reconcile (rv %s -> %s) — would churn/restart the pod",
			cm1.ResourceVersion, cm2.ResourceVersion)
	}
}

// --- #146: plugin-derived conf is owned & unioned by the standalone controller ---

func pluginCR(name, pluginName, clusterRef string, enabled bool, config map[string]string) *neo4jv1beta1.Neo4jPlugin {
	return &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jPluginSpec{
			Name: pluginName, ClusterRef: clusterRef, Enabled: enabled, Config: config,
		},
	}
}

// TestUnionPluginConfSettings: union across plugins (additive keys merged),
// honoring clusterRef + enabled filtering.
func TestUnionPluginConfSettings(t *testing.T) {
	plugins := []neo4jv1beta1.Neo4jPlugin{
		*pluginCR("gds", "gds", "sa", true, nil),
		*pluginCR("bloom", "bloom", "sa", true, nil),
		*pluginCR("elsewhere", "gds", "other", true, nil),      // different target → ignored
		*pluginCR("off", "fleet-management", "sa", false, nil), // disabled → ignored
	}
	got := unionPluginConfSettings(plugins, "sa")

	u := got["dbms.security.procedures.unrestricted"]
	if !strings.Contains(u, "gds.*") || !strings.Contains(u, "bloom.*") {
		t.Errorf("expected gds.* and bloom.* unioned in procedures.unrestricted, got %q", u)
	}
	if strings.Contains(u, "fleetManagement.*") {
		t.Errorf("disabled/other-target plugins must not contribute, got %q", u)
	}
	if got["server.unmanaged_extension_classes"] == "" {
		t.Error("bloom's unmanaged_extension_classes should be present")
	}
}

// TestReconcileConfigMap_PluginUnionRenderedAndPruned: the standalone renders the
// union of its plugins' conf as operator-owned, and prunes a plugin's keys when
// it's uninstalled (#146 — the missing piece beyond #151's foreign-key preserve).
func TestReconcileConfigMap_PluginUnionRenderedAndPruned(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	gds := pluginCR("gds-plugin", "gds", "sa", true, nil)
	bloom := pluginCR("bloom-plugin", "bloom", "sa", true, nil)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gds, bloom).Build()
	r := &Neo4jEnterpriseStandaloneReconciler{Client: c, Scheme: scheme}
	ctx := context.Background()
	sa := standaloneForConf(nil) // name "sa"

	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	conf := renderedConf(t, c)
	for _, want := range []string{"gds.*", "bloom.*", "server.unmanaged_extension_classes"} {
		if !strings.Contains(conf, want) {
			t.Errorf("expected plugin-derived %q in rendered conf; conf:\n%s", want, conf)
		}
	}

	// Uninstall GDS.
	if err := c.Delete(ctx, gds); err != nil {
		t.Fatalf("delete gds: %v", err)
	}
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	conf2 := renderedConf(t, c)
	if strings.Contains(conf2, "gds.*") {
		t.Errorf("gds.* should be pruned after GDS uninstall; conf:\n%s", conf2)
	}
	if !strings.Contains(conf2, "bloom.*") {
		t.Errorf("bloom.* should remain after GDS uninstall; conf:\n%s", conf2)
	}
}

// TestReconcileConfigMap_PluginListFailureIsFatal: if listing Neo4jPlugin fails,
// reconcileConfigMap must error (→ requeue) rather than render a plugin-pruned
// conf and roll the pod with security settings stripped. Regression for Bugbot
// "Plugin list failure prunes config".
func TestReconcileConfigMap_PluginListFailureIsFatal(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	listErr := errors.New("apiserver unavailable")
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*neo4jv1beta1.Neo4jPluginList); ok {
				return listErr
			}
			return cl.List(ctx, list, opts...)
		},
	}).Build()
	r := &Neo4jEnterpriseStandaloneReconciler{Client: c, Scheme: scheme}

	sa := standaloneForConf(map[string]string{"db.transaction.timeout": "30s"})
	if err := r.reconcileConfigMap(context.Background(), sa); err == nil {
		t.Fatal("expected reconcileConfigMap to fail when the plugin List fails, got nil")
	}
}

// TestReconcileConfigMap_SameNamedClusterPluginsNotFolded: a Neo4jPlugin whose
// clusterRef matches a Neo4jEnterpriseCluster of the same name targets the
// CLUSTER (plugin controller resolves cluster-first), so its settings must NOT
// be folded into a same-named standalone's neo4j.conf. Regression for Bugbot
// "Cluster plugins merged into standalone".
func TestReconcileConfigMap_SameNamedClusterPluginsNotFolded(t *testing.T) {
	r, c := standaloneCMTestReconciler(t)
	ctx := context.Background()

	// A cluster shares the standalone's name "sa".
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: "default"},
	}
	if err := c.Create(ctx, cluster); err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	// A GDS plugin referencing "sa" — intended for the cluster.
	if err := c.Create(ctx, pluginCR("gds", "gds", "sa", true, nil)); err != nil {
		t.Fatalf("create plugin: %v", err)
	}

	sa := standaloneForConf(map[string]string{"db.transaction.timeout": "30s"})
	if err := r.reconcileConfigMap(ctx, sa); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	conf := renderedConf(t, c)
	if strings.Contains(conf, "gds.*") {
		t.Errorf("cluster-targeted plugin must not be folded into the same-named standalone; conf:\n%s", conf)
	}
}
