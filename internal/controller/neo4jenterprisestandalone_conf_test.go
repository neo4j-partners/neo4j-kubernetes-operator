package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// TestStandaloneCreateConfigMap_DedupesQueryLogKeys guards NEO3-16: the
// single-node example enables monitoring (which emits db.logs.query.enabled /
// db.logs.query.threshold) AND sets those same keys in spec.config. The rendered
// /conf/neo4j.conf must declare each key only once, or CalVer Neo4j (2025.x+)
// refuses to start with "<key> declared multiple times" → CrashLoopBackOff.
func TestStandaloneCreateConfigMap_DedupesQueryLogKeys(t *testing.T) {
	r := &Neo4jEnterpriseStandaloneReconciler{}
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "standalone-neo4j", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:      neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "2026.04-enterprise"},
			Monitoring: &neo4jv1beta1.MonitoringSpec{Enabled: true, SlowQueryThreshold: "5s"},
			Config: map[string]string{
				"db.logs.query.enabled":   "true",
				"db.logs.query.threshold": "1s",
			},
		},
	}

	conf := r.createConfigMap(standalone).Data["neo4j.conf"]

	assert.Equal(t, 1, strings.Count(conf, "db.logs.query.threshold="),
		"db.logs.query.threshold must be declared exactly once")
	assert.Equal(t, 1, strings.Count(conf, "db.logs.query.enabled="),
		"db.logs.query.enabled must be declared exactly once")
	// User spec.config is appended last, so its value wins after de-duplication.
	assert.Contains(t, conf, "db.logs.query.threshold=1s")
}

// duplicateConfKeys returns any neo4j.conf key that is declared more than once.
// CalVer Neo4j (2025.x+) treats a repeated key as fatal ("<key> declared
// multiple times") and refuses to start. server.jvm.additional /
// dbms.jvm.additional are legitimately repeatable (one line per JVM arg) and so
// are excluded.
func duplicateConfKeys(conf string) []string {
	counts := map[string]int{}
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "server.jvm.additional" || key == "dbms.jvm.additional" {
			continue
		}
		counts[key]++
	}
	var dups []string
	for k, n := range counts {
		if n > 1 {
			dups = append(dups, k)
		}
	}
	return dups
}

// TestStandaloneCreateConfigMap_NoDuplicateKeys is the general invariant behind
// NEO3-16: across realistic config layerings (monitoring + audit + auth + user
// spec.config overrides on shared keys), the rendered standalone neo4j.conf must
// never declare a key twice. This is a pure function of config assembly, so it
// guards the whole class of "CalVer won't boot" regressions at every version in
// milliseconds — the integration suite only exercised CalVer on manual dispatch
// and never with the colliding layers, so it couldn't catch this.
func TestStandaloneCreateConfigMap_NoDuplicateKeys(t *testing.T) {
	r := &Neo4jEnterpriseStandaloneReconciler{}
	base := neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "2026.04-enterprise"}

	cases := []struct {
		name string
		spec neo4jv1beta1.Neo4jEnterpriseStandaloneSpec
	}{
		{
			name: "minimal",
			spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{Image: base},
		},
		{
			name: "monitoring + audit (both touch db.logs.query.obfuscate_literals)",
			spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
				Image:      base,
				Monitoring: &neo4jv1beta1.MonitoringSpec{Enabled: true, SlowQueryThreshold: "5s", QueryLogLevel: "INFO"},
				Audit:      &neo4jv1beta1.AuditSpec{Enabled: true, ObfuscateQueryLiterals: boolPtr(false), ParameterLogging: boolPtr(true)},
			},
		},
		{
			name: "monitoring + audit + user overrides on shared keys",
			spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
				Image:      base,
				Monitoring: &neo4jv1beta1.MonitoringSpec{Enabled: true, SlowQueryThreshold: "5s", QueryLogLevel: "VERBOSE"},
				Audit:      &neo4jv1beta1.AuditSpec{Enabled: true},
				Config: map[string]string{
					"db.logs.query.enabled":            "true",
					"db.logs.query.threshold":          "1s",
					"db.logs.query.obfuscate_literals": "true",
					"server.memory.heap.max_size":      "2G",
				},
			},
		},
		{
			name: "auth + monitoring + user config",
			spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
				Image:      base,
				Monitoring: &neo4jv1beta1.MonitoringSpec{Enabled: true},
				Auth:       &neo4jv1beta1.AuthSpec{AdminSecret: "admin-secret"},
				Config:     map[string]string{"server.memory.heap.initial_size": "1G"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default"},
				Spec:       tc.spec,
			}
			conf := r.createConfigMap(standalone).Data["neo4j.conf"]
			assert.Empty(t, duplicateConfKeys(conf),
				"rendered neo4j.conf must not declare any key twice (CalVer Neo4j fails to start otherwise)")
		})
	}
}
