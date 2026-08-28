package backup

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func testNeo4j() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2025.01.0",
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestBackupJobObjectStore(t *testing.T) {
	compress := true
	verbose := true
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"neo4j", "movies"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://bucket/prod/"},
			Type:        neo4jv1beta1.BackupTypeAuto,
			Options:     &neo4jv1beta1.BackupOptions{Compress: &compress, Verbose: &verbose},
		},
	}
	job, err := BackupJob(testNeo4j(), b)
	if err != nil {
		t.Fatalf("BackupJob: %v", err)
	}
	if job.Name != "nb-backup" {
		t.Errorf("job name = %q, want nb-backup", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if !strings.HasSuffix(c.Image, "-enterprise") {
		t.Errorf("image %q should be the enterprise tag", c.Image)
	}
	for _, want := range []string{
		"--from=g-admin.ns.svc:6362",
		"--to-path=s3://bucket/prod/",
		"--temp-path=" + scratchMountPath,
		"--type=AUTO",
		"--compress=true",
		"--verbose",
	} {
		if !hasArg(c.Args, want) {
			t.Errorf("args missing %q; got %v", want, c.Args)
		}
	}
	// Databases are the trailing operand, comma-joined.
	if last := c.Args[len(c.Args)-1]; last != "neo4j,movies" {
		t.Errorf("trailing databases = %q, want neo4j,movies", last)
	}
	// Scratch staging volume must be present (ADR-015).
	if !hasVolume(job.Spec.Template.Spec.Volumes, scratchVolume) {
		t.Errorf("scratch volume missing; got %v", job.Spec.Template.Spec.Volumes)
	}
	if job.Labels["app.kubernetes.io/managed-by"] != "neo4j-operator" {
		t.Errorf("missing managed-by label; got %v", job.Labels)
	}
	if *job.Spec.TTLSecondsAfterFinished != backupTTLSeconds {
		t.Errorf("ttl = %d, want %d", *job.Spec.TTLSecondsAfterFinished, backupTTLSeconds)
	}
}

func TestBackupJobPVCExistingClaim(t *testing.T) {
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"*"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationPVC, PVC: &neo4jv1beta1.BackupPVC{ClaimName: "backups"}},
		},
	}
	job, err := BackupJob(testNeo4j(), b)
	if err != nil {
		t.Fatalf("BackupJob: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if !hasArg(c.Args, "--to-path=/"+pvcMountPath) {
		t.Errorf("expected pvc to-path; got %v", c.Args)
	}
	if !hasVolume(job.Spec.Template.Spec.Volumes, pvcVolume) {
		t.Errorf("pvc volume missing; got %v", job.Spec.Template.Spec.Volumes)
	}
}

func TestBackupJobPVCProvisioningUnsupported(t *testing.T) {
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationPVC, PVC: &neo4jv1beta1.BackupPVC{Size: "10Gi"}},
		},
	}
	if _, err := BackupJob(testNeo4j(), b); err == nil {
		t.Fatal("expected error for PVC provisioning (only claimName supported)")
	}
}

func hasVolume(vols []corev1.Volume, name string) bool {
	for _, v := range vols {
		if v.Name == name {
			return true
		}
	}
	return false
}
