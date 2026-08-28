package v1beta1

import (
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestBackupFamilyGVKAndScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	for _, kind := range []string{"Neo4jBackup", "Neo4jBackupSchedule", "Neo4jRestore"} {
		gvk := GroupVersion.WithKind(kind)
		if _, err := scheme.New(gvk); err != nil {
			t.Errorf("scheme.New(%s): %v", kind, err)
		}
	}
}

func TestBackupDeepCopyIndependence(t *testing.T) {
	compress := true
	orig := &Neo4jBackup{
		Spec: Neo4jBackupSpec{
			Neo4jRef:    Neo4jRef{Name: "my-graph"},
			Databases:   []string{"*"},
			Destination: BackupDestination{Type: BackupDestinationS3, URL: "s3://b/p/"},
			Type:        BackupTypeAuto,
			Options:     &BackupOptions{Compress: &compress},
		},
	}
	cp := orig.DeepCopy()
	if cp.Spec.Options == orig.Spec.Options {
		t.Fatal("DeepCopy shared the Options pointer")
	}
	cp.Spec.Databases[0] = "neo4j"
	if orig.Spec.Databases[0] != "*" {
		t.Fatal("DeepCopy shared the Databases slice backing array")
	}
}

// TestContractCELRulesPresent guards the load-bearing admission rules from BDR-014 so a
// refactor that silently drops a marker fails here rather than in production admission.
func TestContractCELRulesPresent(t *testing.T) {
	cases := []struct {
		file    string
		needles []string
	}{
		{"neo4j.com_neo4jbackups.yaml", []string{
			"Neo4jBackup spec is immutable",
			"object-store destinations require url",
		}},
		{"neo4j.com_neo4jrestores.yaml", []string{
			"system cannot be restored",
			"forceOffline requires overwrite",
			"source.backupRef",
			"Neo4jRestore spec is immutable",
		}},
		{"neo4j.com_neo4jbackupschedules.yaml", []string{
			"aggregate.schedule is required",
		}},
	}
	for _, c := range cases {
		raw, err := os.ReadFile(crdBasesDir + "/" + c.file)
		if err != nil {
			t.Fatalf("read %s (run make manifests): %v", c.file, err)
		}
		body := string(raw)
		for _, n := range c.needles {
			if !strings.Contains(body, n) {
				t.Errorf("%s: expected CEL rule message %q not found in generated CRD", c.file, n)
			}
		}
	}
}
