package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

func envValue(env []corev1.EnvVar, name string) string {
	for _, e := range env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

func envCount(env []corev1.EnvVar, name string) int {
	n := 0
	for _, e := range env {
		if e.Name == name {
			n++
		}
	}
	return n
}

// mergePluginSecurityEnv must UNION additive allowlists across plugins (so GDS
// and APOC both survive) and use the correct Neo4j env-var name form.
func TestMergePluginSecurityEnv_UnionsAllowlistsAcrossPlugins(t *testing.T) {
	unrestricted := resources.Neo4jSettingEnvVarName("dbms.security.procedures.unrestricted")
	allowlist := resources.Neo4jSettingEnvVarName("dbms.security.procedures.allowlist")
	// Correct convention: lowercase, dots->underscores (no upper-casing).
	assert.Equal(t, "NEO4J_dbms_security_procedures_unrestricted", unrestricted)

	env := mergePluginSecurityEnv(nil, map[string]string{
		"dbms.security.procedures.unrestricted": "gds.*",
		"dbms.security.procedures.allowlist":    "gds.*",
	})
	env = mergePluginSecurityEnv(env, map[string]string{
		"dbms.security.procedures.unrestricted": "apoc.*",
		"dbms.security.procedures.allowlist":    "apoc.*",
	})

	assert.Equal(t, "gds.*,apoc.*", envValue(env, unrestricted), "GDS allowlist must not be clobbered by APOC")
	assert.Equal(t, "gds.*,apoc.*", envValue(env, allowlist))
	assert.Equal(t, 1, envCount(env, unrestricted), "exactly one env var per key")

	// Idempotent.
	before := envValue(env, unrestricted)
	env = mergePluginSecurityEnv(env, map[string]string{"dbms.security.procedures.unrestricted": "gds.*"})
	assert.Equal(t, before, envValue(env, unrestricted))

	// Scalar key overwritten in place.
	scalar := resources.Neo4jSettingEnvVarName("apoc.export.file.enabled")
	env = mergePluginSecurityEnv(env, map[string]string{"apoc.export.file.enabled": "true"})
	env = mergePluginSecurityEnv(env, map[string]string{"apoc.export.file.enabled": "false"})
	assert.Equal(t, "false", envValue(env, scalar))
}

// removePluginSecurityEnv must subtract only the uninstalled plugin's tokens,
// keep other plugins' tokens, and drop the env var when nothing remains.
func TestRemovePluginSecurityEnv_PrunesOnUninstall(t *testing.T) {
	unrestricted := resources.Neo4jSettingEnvVarName("dbms.security.procedures.unrestricted")

	// GDS + APOC installed.
	env := mergePluginSecurityEnv(nil, map[string]string{"dbms.security.procedures.unrestricted": "gds.*"})
	env = mergePluginSecurityEnv(env, map[string]string{"dbms.security.procedures.unrestricted": "apoc.*"})
	assert.Equal(t, "gds.*,apoc.*", envValue(env, unrestricted))

	// Uninstall APOC → only gds.* remains (GDS not lost).
	env = removePluginSecurityEnv(env, map[string]string{"dbms.security.procedures.unrestricted": "apoc.*"})
	assert.Equal(t, "gds.*", envValue(env, unrestricted))

	// Uninstall GDS → env var dropped entirely.
	env = removePluginSecurityEnv(env, map[string]string{"dbms.security.procedures.unrestricted": "gds.*"})
	assert.Equal(t, 0, envCount(env, unrestricted), "env var removed when no tokens remain")
}

// TestPluginSecurityRemovalSettings_PrunesAccumulatedDefaults reproduces the
// orphaned-automatic-defaults trace: a GDS plugin is installed with no
// spec.security (automatic gds.*,apoc.load.* land in env), the user later adds a
// narrower allowedProcedures override (which the merge path UNIONS in), then
// uninstalls. The removal set must subtract BOTH the automatic defaults and the
// current override so nothing is left behind — pluginSecuritySettings alone would
// only know the current override and orphan gds.*,apoc.load.*.
func TestPluginSecurityRemovalSettings_PrunesAccumulatedDefaults(t *testing.T) {
	unrestricted := resources.Neo4jSettingEnvVarName("dbms.security.procedures.unrestricted")
	allowlist := resources.Neo4jSettingEnvVarName("dbms.security.procedures.allowlist")
	r := newPluginTestReconciler(t)

	gds := &neo4jv1beta1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "gds", Namespace: "default"},
		Spec:       neo4jv1beta1.Neo4jPluginSpec{ClusterRef: "c1", Name: "graph-data-science", Version: "2.13.0"},
	}

	// 1. Install with no spec.security → automatic defaults unioned into env.
	env := mergePluginSecurityEnv(nil, r.pluginSecuritySettings(gds))
	assert.Equal(t, "gds.*,apoc.load.*", envValue(env, unrestricted))

	// 2. User adds a narrower override; merge UNIONS it (so env now carries both
	//    the automatic defaults AND the custom list).
	gds.Spec.Security = &neo4jv1beta1.PluginSecurity{AllowedProcedures: []string{"gds.graph.*"}}
	env = mergePluginSecurityEnv(env, r.pluginSecuritySettings(gds))
	assert.Equal(t, "gds.*,apoc.load.*,gds.graph.*", envValue(env, unrestricted))
	assert.Equal(t, "gds.graph.*", envValue(env, allowlist))

	// 3. Uninstall via the removal set: every token this plugin ever contributed
	//    must go — automatic defaults included — so the vars drop entirely.
	env = removePluginSecurityEnv(env, r.pluginSecurityRemovalSettings(gds))
	assert.Equal(t, 0, envCount(env, unrestricted), "automatic defaults must not be orphaned on uninstall")
	assert.Equal(t, 0, envCount(env, allowlist), "override allowlist must be pruned on uninstall")
}
