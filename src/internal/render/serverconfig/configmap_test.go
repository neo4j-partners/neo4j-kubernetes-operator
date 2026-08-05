package serverconfig

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
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

func TestValidateConfigRejectsNeo4jKeysInApoc(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				Apoc: map[string]string{
					"apoc.trigger.enabled":                  "true",
					"dbms.security.procedures.unrestricted": "apoc.*",
				},
			},
		},
	}
	if err := ValidateConfig(neo4j); err == nil {
		t.Fatal("expected error for dbms.* under config.apoc")
	}
}

func TestRenderApocConfSkipsNonApocKeys(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Plugins:  []string{"apoc"},
			Config: &neo4jv1beta1.ConfigSpec{
				Apoc: map[string]string{
					"apoc.trigger.enabled":                  "true",
					"dbms.security.procedures.unrestricted": "apoc.*",
				},
			},
		},
	}
	body := renderApocConf(render.StandaloneContext(neo4j))
	if !strings.Contains(body, "apoc.trigger.enabled=true") {
		t.Fatalf("missing apoc key: %q", body)
	}
	if strings.Contains(body, "dbms.security") {
		t.Fatalf("dbms.security must not appear in apoc.conf: %q", body)
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

func TestConfigMapRendersJVMDefaults(t *testing.T) {
	trueVal, falseVal := true, false
	cases := []struct {
		name    string
		jvm     *neo4jv1beta1.JVMSpec
		wantKey bool
		wantIn  []string
		wantOut []string
	}{
		{
			name:    "nil jvm uses defaults",
			jvm:     nil,
			wantKey: true,
			wantIn:  []string{"-XX:+UseG1GC", "-Dlog4j2.disable.jmx=true"},
		},
		{
			name:    "useDefaults true alone",
			jvm:     &neo4jv1beta1.JVMSpec{UseDefaults: &trueVal},
			wantKey: true,
			wantIn:  []string{"-XX:+UseG1GC"},
		},
		{
			name: "defaults then additionalArguments",
			jvm: &neo4jv1beta1.JVMSpec{
				UseDefaults:         &trueVal,
				AdditionalArguments: []string{"-XX:+ExitOnOutOfMemoryError"},
			},
			wantKey: true,
			wantIn:  []string{"-XX:+UseG1GC", "-XX:+ExitOnOutOfMemoryError"},
		},
		{
			name: "same key overrides default in place",
			jvm: &neo4jv1beta1.JVMSpec{
				UseDefaults: &trueVal,
				AdditionalArguments: []string{
					"-XX:MaxMetaspaceSize=1024m",
					"-XX:-OmitStackTraceInFastThrow",
					"-Djdk.nio.maxCachedBufferSize=1024",
					"-Djdk.nio.maxCachedBufferSize=1026",
					"-XX:-OmitStackTraceInFastThrow",
				},
			},
			wantKey: true,
			wantIn: []string{
				"-XX:-OmitStackTraceInFastThrow",
				"-Djdk.nio.maxCachedBufferSize=1026",
				"-XX:MaxMetaspaceSize=1024m",
			},
			wantOut: []string{"-Djdk.nio.maxCachedBufferSize=1024"},
		},
		{
			name: "useDefaults false only additional",
			jvm: &neo4jv1beta1.JVMSpec{
				UseDefaults:         &falseVal,
				AdditionalArguments: []string{"-XX:MaxMetaspaceSize=1024m"},
			},
			wantKey: true,
			wantIn:  []string{"-XX:MaxMetaspaceSize=1024m"},
			wantOut: []string{"-XX:+UseG1GC"},
		},
		{
			name:    "useDefaults false empty args omits key",
			jvm:     &neo4jv1beta1.JVMSpec{UseDefaults: &falseVal},
			wantKey: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			neo4j := &neo4jv1beta1.Neo4j{
				ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jSpec{
					Config: &neo4jv1beta1.ConfigSpec{JVM: tc.jvm},
				},
			}
			data := ConfigMap(render.StandaloneContext(neo4j)).Data
			got, ok := data["server.jvm.additional"]
			if ok != tc.wantKey {
				t.Fatalf("server.jvm.additional present=%v, want %v (value=%q)", ok, tc.wantKey, got)
			}
			for _, s := range tc.wantIn {
				if !strings.Contains(got, s) {
					t.Fatalf("server.jvm.additional missing %q:\n%s", s, got)
				}
			}
			for _, s := range tc.wantOut {
				if strings.Contains(got, s) {
					t.Fatalf("server.jvm.additional unexpectedly contains %q:\n%s", s, got)
				}
			}
			if tc.name == "same key overrides default in place" {
				if n := strings.Count(got, "-XX:-OmitStackTraceInFastThrow"); n != 1 {
					t.Fatalf("expected OmitStackTrace once, got %d:\n%s", n, got)
				}
				if n := strings.Count(got, "maxCachedBufferSize"); n != 1 {
					t.Fatalf("expected maxCachedBufferSize once, got %d:\n%s", n, got)
				}
				// New keys append after defaults; overrides keep default position.
				meta := strings.Index(got, "-XX:MaxMetaspaceSize=1024m")
				buf := strings.Index(got, "-Djdk.nio.maxCachedBufferSize=1026")
				if meta < 0 || buf < 0 || !(buf < meta) {
					t.Fatalf("override should keep default position before new keys:\n%s", got)
				}
			}
		})
	}
}
