package serverconfig

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterNeo4jConfInjected(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{
					Members: 3,
				},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				ClusterDomain: "cluster.local",
			},
		},
	}
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	for key, want := range map[string]string{
		"server.default_listen_address":              "0.0.0.0",
		"server.http.listen_address":                 ":7474",
		"server.http.enabled":                        "true",
		"server.bolt.listen_address":                 ":7687",
		"initial.dbms.default_primaries_count":       "1", // defaultPrimariesCount unset → 1
		"dbms.cluster.minimum_initial_system_primaries_count": "3", // derived: multi-primary cluster
		"dbms.cluster.discovery.resolver_type":       "K8S",
		"dbms.kubernetes.discovery.service_port_name": "tcp-tx",
		"dbms.kubernetes.label_selector": "app.kubernetes.io/name=neo4j,app.kubernetes.io/instance=prod,neo4j.com/service=internals,neo4j.com/clustering=true",
		"dbms.routing.enabled":           "true",
		"server.bolt.advertised_address": "$(bash -c 'echo ${SERVICE_NEO4J}')",
		"server.cluster.raft.advertised_address": "$(bash -c 'echo ${SERVICE_NEO4J_INTERNALS}')",
	} {
		if data[key] != want {
			t.Fatalf("primary config key %q = %q, want %q", key, data[key], want)
		}
	}

	analyticsData := ConfigMap(render.ContextForPool(neo4j, render.PoolAnalytics)).Data
	if analyticsData["server.cluster.system_database_mode"] != "SECONDARY" {
		t.Fatalf("analytics config missing SECONDARY mode: %#v", analyticsData)
	}
	if analyticsData["initial.server.mode_constraint"] != "SECONDARY" {
		t.Fatalf("analytics config missing mode_constraint: %#v", analyticsData)
	}
	// Secondaries discover all internals (no clustering=true) so they can find primaries.
	wantSec := "app.kubernetes.io/name=neo4j,app.kubernetes.io/instance=prod,neo4j.com/service=internals"
	if analyticsData["dbms.kubernetes.label_selector"] != wantSec {
		t.Fatalf("analytics discovery selector = %q, want %q", analyticsData["dbms.kubernetes.label_selector"], wantSec)
	}
}

// The bootstrap gate must not move with the pool size, or every scale would rewrite neo4j.conf and
// roll the pool in the middle of the resize. 1 primary → 1, any multi-primary shape → 3.
func TestSystemBootstrapGateIsScaleInvariant(t *testing.T) {
	key := "dbms.cluster.minimum_initial_system_primaries_count"
	for _, tc := range []struct {
		members int32
		want    string
	}{{1, "1"}, {3, "3"}, {5, "3"}, {7, "3"}} {
		neo4j := &neo4jv1beta1.Neo4j{
			ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jSpec{
				Topology: neo4jv1beta1.TopologySpec{
					Mode:      neo4jv1beta1.TopologyModeCluster,
					Primaries: &neo4jv1beta1.PrimariesSpec{Members: tc.members},
				},
			},
		}
		data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
		if data[key] != tc.want {
			t.Errorf("%d primaries: %s = %q, want %q", tc.members, key, data[key], tc.want)
		}
	}
}

// topology.minimumMembers raises the bar above the derived value, and keeps it: the field is
// immutable, so a later scale-in leaves the rendered gate alone rather than rewriting neo4j.conf.
func TestUserBootstrapGateOverridesDerivedAndSurvivesScaleIn(t *testing.T) {
	key := "dbms.cluster.minimum_initial_system_primaries_count"
	gate := int32(5)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:           neo4jv1beta1.TopologyModeCluster,
				Primaries:      &neo4jv1beta1.PrimariesSpec{Members: 5},
				MinimumMembers: &gate,
			},
		},
	}
	before := ConfigChecksum(render.ContextForPool(neo4j, render.PoolPrimary))
	if data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data; data[key] != "5" {
		t.Fatalf("%s = %q, want 5 from minimumMembers", key, data[key])
	}
	neo4j.Spec.Topology.Primaries.Members = 3
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if data[key] != "5" {
		t.Fatalf("after scale-in %s = %q, want the unchanged 5", key, data[key])
	}
	if got := ConfigChecksum(render.ContextForPool(neo4j, render.PoolPrimary)); got != before {
		t.Fatalf("scale-in changed the config checksum (%s → %s): the pool would roll during the resize", before, got)
	}
}

// An even gate is legal: Neo4j accepts any integer >= 1, and a two-server cluster needs 2.
func TestEvenBootstrapGateIsRendered(t *testing.T) {
	gate := int32(2)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:           neo4jv1beta1.TopologyModeCluster,
				Primaries:      &neo4jv1beta1.PrimariesSpec{Members: 3},
				MinimumMembers: &gate,
			},
		},
	}
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if data["dbms.cluster.minimum_initial_system_primaries_count"] != "2" {
		t.Fatalf("gate = %q, want 2", data["dbms.cluster.minimum_initial_system_primaries_count"])
	}
}

// The same cluster scaled 3 → 5 → 3 must render byte-identical config, so the checksum never moves.
func TestScalingDoesNotChangeConfigChecksum(t *testing.T) {
	def := int32(3)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:                  neo4jv1beta1.TopologyModeCluster,
				Primaries:             &neo4jv1beta1.PrimariesSpec{Members: 3},
				DefaultPrimariesCount: &def,
			},
		},
	}
	before := ConfigChecksum(render.ContextForPool(neo4j, render.PoolPrimary))
	neo4j.Spec.Topology.Primaries.Members = 5
	if got := ConfigChecksum(render.ContextForPool(neo4j, render.PoolPrimary)); got != before {
		t.Fatalf("scaling out changed the config checksum (%s → %s): the pool would roll during the resize", before, got)
	}
	neo4j.Spec.Topology.Primaries.Members = 3
	if got := ConfigChecksum(render.ContextForPool(neo4j, render.PoolPrimary)); got != before {
		t.Fatalf("scaling back in changed the config checksum (%s → %s)", before, got)
	}
}

// Resizing a secondary pool used to roll every member, primaries included: the read/analytics
// total lands in initial.dbms.default_secondaries_count, which every pool carries. Neo4j never
// reads that key again after initialisation — formation pushes the new value over Bolt — so the
// restart bought nothing, and on a single-primary cluster it took the whole DBMS down each time
// somebody scaled a read pool.
func TestSecondaryScalingDoesNotChangeConfigChecksum(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 1},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Read: &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
				},
			},
		},
	}
	pools := []render.PoolID{render.PoolPrimary, render.PoolRead}
	before := map[render.PoolID]string{}
	for _, pool := range pools {
		before[pool] = ConfigChecksum(render.ContextForPool(neo4j, pool))
	}

	for _, members := range []int32{2, 1} {
		neo4j.Spec.Topology.Secondaries.Read.Members = members
		for _, pool := range pools {
			if got := ConfigChecksum(render.ContextForPool(neo4j, pool)); got != before[pool] {
				t.Errorf("read pool resized to %d changed the %s checksum (%s → %s): the pool would roll",
					members, pool, before[pool], got)
			}
		}
	}

	// The ConfigMap still has to carry the current count: a member created after the resize reads
	// it at first start, which is the one moment Neo4j uses it.
	neo4j.Spec.Topology.Secondaries.Read.Members = 2
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if data["initial.dbms.default_secondaries_count"] != "2" {
		t.Errorf("default_secondaries_count = %q, want 2 — the key is only kept out of the checksum, not out of the ConfigMap",
			data["initial.dbms.default_secondaries_count"])
	}
}

func TestDefaultPrimariesCountDrivesDefaultDBTopology(t *testing.T) {
	def := int32(3)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:                  neo4jv1beta1.TopologyModeCluster,
				Primaries:             &neo4jv1beta1.PrimariesSpec{Members: 3},
				DefaultPrimariesCount: &def,
			},
		},
	}
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if data["initial.dbms.default_primaries_count"] != "3" {
		t.Fatalf("default_primaries_count = %q, want 3 from defaultPrimariesCount", data["initial.dbms.default_primaries_count"])
	}
	if data["dbms.cluster.minimum_initial_system_primaries_count"] != "3" {
		t.Fatalf("minimum_initial_system_primaries_count = %q, want 3", data["dbms.cluster.minimum_initial_system_primaries_count"])
	}
}

func TestDefaultPrimariesCountClampedToPrimaries(t *testing.T) {
	def := int32(5)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:                  neo4jv1beta1.TopologyModeCluster,
				Primaries:             &neo4jv1beta1.PrimariesSpec{Members: 3},
				DefaultPrimariesCount: &def,
			},
		},
	}
	data := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if data["initial.dbms.default_primaries_count"] != "3" {
		t.Fatalf("expected clamp to primaries.members=3, got %q", data["initial.dbms.default_primaries_count"])
	}
}


func TestReadPoolCannotBootstrapAsPrimary(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Analytics: &neo4jv1beta1.SecondaryPoolSpec{Members: 1, Plugins: []string{"gds"}},
					Read:      &neo4jv1beta1.SecondaryPoolSpec{Members: 1, Plugins: []string{"apoc"}},
				},
			},
		},
	}
	read := ConfigMap(render.ContextForPool(neo4j, render.PoolRead)).Data
	if read["server.cluster.system_database_mode"] != "SECONDARY" ||
		read["initial.server.mode_constraint"] != "SECONDARY" {
		t.Fatalf("read pool must be SECONDARY: %#v", read)
	}
	primary := ConfigMap(render.ContextForPool(neo4j, render.PoolPrimary)).Data
	if primary["server.cluster.system_database_mode"] == "SECONDARY" {
		t.Fatalf("primary must not force SECONDARY system mode: %#v", primary)
	}
	if primary["initial.server.mode_constraint"] == "SECONDARY" {
		t.Fatalf("primary must not have SECONDARY mode_constraint: %#v", primary)
	}
}

func TestStandaloneNeo4jConfInjectedK8sDefaults(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	for key, want := range map[string]string{
		"server.default_listen_address": "0.0.0.0",
		"server.http.listen_address":    ":7474",
		"server.http.enabled":           "true",
	} {
		if data[key] != want {
			t.Fatalf("standalone config key %q = %q, want %q", key, data[key], want)
		}
	}
	if _, ok := data["dbms.cluster.discovery.resolver_type"]; ok {
		t.Fatalf("standalone config must not contain cluster keys: %#v", data)
	}
}

func TestListenerConfKeysHTTPSBackupMetrics(t *testing.T) {
	https := int32(7473)
	backup := int32(6362)
	metrics := int32(2004)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Features: &neo4jv1beta1.FeaturesSpec{
				Backup: &neo4jv1beta1.BackupFeatureSpec{Enabled: true},
				Monitoring: &neo4jv1beta1.MonitoringFeaturesSpec{
					Prometheus: &neo4jv1beta1.PrometheusMonitoringSpec{Enabled: true},
				},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{
					HTTPS:   &https,
					Backup:  &backup,
					Metrics: &metrics,
				},
			},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	for key, want := range map[string]string{
		"server.https.listen_address":            ":7473",
		"server.https.enabled":                   "true",
		"server.backup.listen_address":           "0.0.0.0:6362",
		"server.backup.enabled":                  "true",
		"server.metrics.prometheus.enabled":      "true",
		"server.metrics.prometheus.endpoint":     "localhost:2004",
	} {
		if data[key] != want {
			t.Fatalf("config key %q = %q, want %q", key, data[key], want)
		}
	}
}

func TestPluginConfKeysAPOC(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Plugins:  []string{"apoc"},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	if data["server.directories.plugins"] != "/plugins" {
		t.Fatalf("directories.plugins = %q", data["server.directories.plugins"])
	}
	if data["dbms.security.procedures.allowlist"] != "apoc.*" {
		t.Fatalf("allowlist = %q", data["dbms.security.procedures.allowlist"])
	}
	if _, ok := data["dbms.security.procedures.unrestricted"]; ok {
		t.Fatalf("unrestricted must not be auto-set (NEO-024), got %q", data["dbms.security.procedures.unrestricted"])
	}
	if _, ok := data["dbms.security.http_auth_allowlist"]; ok {
		t.Fatalf("http_auth_allowlist must not be auto-set (NEO-024), got %q", data["dbms.security.http_auth_allowlist"])
	}
}

func TestPluginConfKeysPluginsVolumeOnly(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data:    neo4jv1beta1.DataVolumeSpec{Mode: neo4jv1beta1.VolumeModeDynamic, Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "1Gi"}},
					Plugins: &neo4jv1beta1.AuxiliaryVolumeSpec{Mode: neo4jv1beta1.VolumeModeShare},
				},
			},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	if data["server.directories.plugins"] != "/plugins" {
		t.Fatalf("directories.plugins = %q", data["server.directories.plugins"])
	}
	if _, ok := data["dbms.security.procedures.unrestricted"]; ok {
		t.Fatalf("unrestricted should be unset without catalog plugins, got %q", data["dbms.security.procedures.unrestricted"])
	}
}

func TestPluginConfKeysAPOCWithPluginsVolume(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Plugins:  []string{"apoc"},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data:    neo4jv1beta1.DataVolumeSpec{Mode: neo4jv1beta1.VolumeModeDynamic, Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "1Gi"}},
					Plugins: &neo4jv1beta1.AuxiliaryVolumeSpec{Mode: neo4jv1beta1.VolumeModeShare},
				},
			},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	if data["server.directories.plugins"] != "/plugins" {
		t.Fatalf("directories.plugins = %q", data["server.directories.plugins"])
	}
	if data["dbms.security.procedures.allowlist"] != "apoc.*" {
		t.Fatalf("allowlist = %q", data["dbms.security.procedures.allowlist"])
	}
}

func TestUserNeo4jConfigOverridesPluginDefaults(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Plugins:  []string{"apoc"},
			Config: &neo4jv1beta1.ConfigSpec{
				Neo4j: map[string]string{
					"dbms.security.procedures.unrestricted": "apoc.algo.aStar",
					"dbms.security.procedures.allowlist":    "apoc.algo.aStar",
				},
			},
		},
	}
	data := ConfigMap(render.StandaloneContext(neo4j)).Data
	if data["dbms.security.procedures.unrestricted"] != "apoc.algo.aStar" {
		t.Fatalf("unrestricted = %q, want user override", data["dbms.security.procedures.unrestricted"])
	}
	if data["dbms.security.procedures.allowlist"] != "apoc.algo.aStar" {
		t.Fatalf("allowlist = %q, want user override", data["dbms.security.procedures.allowlist"])
	}
	if data["server.directories.plugins"] != "/plugins" {
		t.Fatalf("directories.plugins default should remain, got %q", data["server.directories.plugins"])
	}
}

func TestNeo4jConfDataHasNoNeo4jConfBlobKey(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	for k := range ConfigMap(render.StandaloneContext(neo4j)).Data {
		if strings.Contains(k, "\n") || k == "neo4j.conf" {
			t.Fatalf("unexpected config key %q", k)
		}
	}
}
