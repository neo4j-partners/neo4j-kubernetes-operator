package serverconfig

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLoggingDefaultsOmitConfigMaps(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
	}
	ctx := render.StandaloneContext(neo4j)
	if ServerLogsConfigMap(ctx) != nil || UserLogsConfigMap(ctx) != nil {
		t.Fatal("LOG-01: no logging ConfigMaps without spec.logging")
	}
	if HasServerLogsConfig(ctx) || HasUserLogsConfig(ctx) {
		t.Fatal("expected no custom logging")
	}
}

func TestLoggingCustomXML(t *testing.T) {
	serverXML := `<?xml version="1.0"?><Configuration status="ERROR"></Configuration>`
	userXML := `<?xml version="1.0"?><Configuration status="WARN"></Configuration>`
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Logging: &neo4jv1beta1.LoggingSpec{
				ServerLogsXml: serverXML,
				UserLogsXml:   userXML,
			},
		},
	}
	ctx := render.StandaloneContext(neo4j)
	scm := ServerLogsConfigMap(ctx)
	if scm == nil || scm.Data["server-logs.xml"] != serverXML {
		t.Fatalf("server logs cm = %#v", scm)
	}
	data := ConfigMap(ctx).Data
	if data["server.logs.config"] != "/config/server-logs.xml" {
		t.Fatalf("server.logs.config = %q", data["server.logs.config"])
	}
	before := ConfigChecksum(ctx)
	neo4j.Spec.Logging.ServerLogsXml = serverXML + "<!--x-->"
	after := ConfigChecksum(render.StandaloneContext(neo4j))
	if before == after {
		t.Fatal("checksum should change when logging XML changes")
	}
	if !strings.Contains(scm.Name, "server-logs-config") {
		t.Fatalf("cm name = %q", scm.Name)
	}
}

func TestLoggingConfigMapRef(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Logging: &neo4jv1beta1.LoggingSpec{
				ServerLogsConfigMapRef: &neo4jv1beta1.LoggingConfigMapRef{
					Name: "my-server-logs",
					Key:  "custom-server.xml",
				},
				UserLogsConfigMapRef: &neo4jv1beta1.LoggingConfigMapRef{Name: "my-user-logs"},
			},
		},
	}
	ctx := render.StandaloneContext(neo4j)
	if ServerLogsConfigMap(ctx) != nil || UserLogsConfigMap(ctx) != nil {
		t.Fatal("ref mode must not create operator ConfigMaps")
	}
	if ServerLogsConfigMapName(ctx) != "my-server-logs" || ServerLogsConfigMapKey(ctx) != "custom-server.xml" {
		t.Fatalf("server resolve = %s/%s", ServerLogsConfigMapName(ctx), ServerLogsConfigMapKey(ctx))
	}
	if UserLogsConfigMapName(ctx) != "my-user-logs" || UserLogsConfigMapKey(ctx) != "user-logs.xml" {
		t.Fatalf("user resolve = %s/%s", UserLogsConfigMapName(ctx), UserLogsConfigMapKey(ctx))
	}
	data := ConfigMap(ctx).Data
	if data["server.logs.config"] != "/config/server-logs.xml" || data["server.logs.user.config"] != "/config/user-logs.xml" {
		t.Fatalf("conf keys = %#v", data)
	}
}

func TestValidateLoggingXOR(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Logging: &neo4jv1beta1.LoggingSpec{
				ServerLogsXml: "<Configuration/>",
				ServerLogsConfigMapRef: &neo4jv1beta1.LoggingConfigMapRef{Name: "x"},
			},
		},
	}
	if err := ValidateLogging(neo4j); err == nil {
		t.Fatal("expected XOR error")
	}
}
