package serverconfig

import (
	"strings"
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
	conf := ConfigMap(render.StandaloneContext(neo4j)).Data["neo4j.conf"]
	for _, want := range []string{
		"db.transaction.timeout=42s",
		"dbms.security.auth_max_failed_attempts=5",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("neo4j.conf missing %q:\n%s", want, conf)
		}
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
