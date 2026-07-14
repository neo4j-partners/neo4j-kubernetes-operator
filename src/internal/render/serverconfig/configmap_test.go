package serverconfig

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConfigMapRendersNeo4jKeys(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				Neo4j: map[string]string{
					"db.transaction.timeout":                 "42s",
					"dbms.security.auth_max_failed_attempts": "5",
				},
			},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	for key, want := range map[string]string{
		"db.transaction.timeout":                 "42s",
		"dbms.security.auth_max_failed_attempts": "5",
	} {
		if data[key] != want {
			t.Fatalf("config key %q = %q, want %q", key, data[key], want)
		}
	}
	if _, ok := data["neo4j.conf"]; ok {
		t.Fatal("config must use per-setting keys, not a neo4j.conf blob")
	}
}

func TestConfigMapRendersApocOnlyWhenAssigned(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Config: &neo4jv1beta1.ConfigSpec{
				Apoc: map[string]string{"apoc.trigger.enabled": "true"},
			},
		},
	}
	if ApocConfigMap(render.StandaloneContext(neo4j)) != nil {
		t.Fatal("apoc ConfigMap should not render without plugins: [apoc]")
	}

	neo4j.Spec.Plugins = []string{"apoc"}
	apocCM := ApocConfigMap(render.StandaloneContext(neo4j))
	if apocCM == nil || apocCM.Data["apoc.conf"] == "" {
		t.Fatal("expected apoc.conf when apoc plugin is assigned")
	}
}

func TestConfigChecksumChangesWithSpec(t *testing.T) {
	base := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				Neo4j: map[string]string{"db.transaction.timeout": "42s"},
			},
		},
	}
	updated := base.DeepCopy()
	updated.Spec.Config.Neo4j["dbms.security.auth_max_failed_attempts"] = "5"

	before := ConfigChecksum(render.StandaloneContext(base))
	after := ConfigChecksum(render.StandaloneContext(updated))
	if before == after {
		t.Fatalf("checksum should change when spec.config.neo4j changes")
	}
	if before == "" || after == "" {
		t.Fatalf("checksum must not be empty")
	}
}
