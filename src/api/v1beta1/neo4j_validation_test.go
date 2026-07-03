package v1beta1

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

func loadSample(t *testing.T, name string) Neo4j {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..", "config", "samples", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample %s: %v", name, err)
	}
	var neo4j Neo4j
	if err := yaml.Unmarshal(data, &neo4j); err != nil {
		t.Fatalf("unmarshal sample %s: %v", name, err)
	}
	return neo4j
}

func TestSampleStandaloneUnmarshals(t *testing.T) {
	neo4j := loadSample(t, "neo4j_v1beta1_neo4j.yaml")
	if neo4j.Spec.Topology.Mode != TopologyModeStandalone {
		t.Fatalf("mode = %q, want Standalone", neo4j.Spec.Topology.Mode)
	}
	if neo4j.Spec.Edition != EditionEnterprise {
		t.Fatalf("edition = %q, want enterprise", neo4j.Spec.Edition)
	}
	if neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil || neo4j.Spec.Storage.Volumes.Data.Dynamic == nil {
		t.Fatal("expected dynamic data volume in standalone sample")
	}
}

func TestSampleClusterUnmarshals(t *testing.T) {
	neo4j := loadSample(t, "neo4j_v1beta1_neo4j_cluster.yaml")
	if neo4j.Spec.Topology.Mode != TopologyModeCluster {
		t.Fatalf("mode = %q, want Cluster", neo4j.Spec.Topology.Mode)
	}
	if neo4j.Spec.Topology.Primaries == nil || neo4j.Spec.Topology.Primaries.Members != 1 {
		t.Fatal("expected primaries.members: 1 in cluster sample")
	}
	if neo4j.Spec.Topology.Secondaries == nil || neo4j.Spec.Topology.Secondaries.Read == nil {
		t.Fatal("expected read pool in cluster sample for scale subresource")
	}
	if neo4j.Spec.PluginDefinitions["gds"].LicenseSecretRef != "gds-license" {
		t.Fatal("expected gds licenseSecretRef in cluster sample")
	}
}

func TestCRDContainsCELValidations(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..", "config", "crd", "bases", "neo4j.com_neo4js.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}
	content := string(data)
	for _, fragment := range []string{
		"x-kubernetes-validations",
		"primaries.members is required when mode is Cluster",
		"topology.mode cannot change",
		"statusReplicasPath: .status.readPoolReplicas",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("CRD missing expected fragment %q", fragment)
		}
	}
}
