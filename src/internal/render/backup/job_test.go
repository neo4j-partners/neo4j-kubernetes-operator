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
	// The destination must mount the "backups" subPath so the Job writes where the workload's
	// backups volume reads (ADR-015 round-trip); otherwise restore seeds file:/backups/<ptr> and
	// the server can't find it.
	var mount *corev1.VolumeMount
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == pvcVolume {
			mount = &c.VolumeMounts[i]
		}
	}
	if mount == nil {
		t.Fatalf("pvc mount missing; got %v", c.VolumeMounts)
	}
	if mount.SubPath != pvcSubPath {
		t.Errorf("pvc mount subPath = %q, want %q (must match workload backups mount)", mount.SubPath, pvcSubPath)
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

func TestBackupJobPVCExplicitDBsRecordsArtifactName(t *testing.T) {
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"neo4j"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationPVC, PVC: &neo4jv1beta1.BackupPVC{ClaimName: "backups"}},
			Type:        neo4jv1beta1.BackupTypeFull,
		},
	}
	job, err := BackupJob(testNeo4j(), b)
	if err != nil {
		t.Fatalf("BackupJob: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	// A seedable PVC backup is wrapped in sh -c so it can record the real artifact filename.
	if len(c.Command) != 3 || c.Command[0] != "sh" || c.Command[1] != "-c" {
		t.Fatalf("expected sh -c wrapper; got command=%v args=%v", c.Command, c.Args)
	}
	if c.Args != nil {
		t.Errorf("args should be nil when wrapped; got %v", c.Args)
	}
	script := c.Command[2]
	for _, want := range []string{
		"neo4j-admin database backup",
		"--to-path=/" + pvcMountPath,
		"ls -t /" + pvcMountPath + "/neo4j-*.backup",
		"/dev/termination-log",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q; got: %s", want, script)
		}
	}
	// The fragile renamed pointer must be gone (it would duplicate a .backup and confuse chains).
	for _, unwanted := range []string{"ln -f", "cp -f", "latest.backup"} {
		if strings.Contains(script, unwanted) {
			t.Errorf("script must not create a renamed pointer (%q); got: %s", unwanted, script)
		}
	}
	if c.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Errorf("terminationMessagePolicy = %q, want FallbackToLogsOnError (surfaces recorded name)", c.TerminationMessagePolicy)
	}
}

func TestBackupJobSatisfiesRestrictedPodSecurity(t *testing.T) {
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"neo4j"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://b/"},
		},
	}
	job, err := BackupJob(testNeo4j(), b)
	if err != nil {
		t.Fatalf("BackupJob: %v", err)
	}
	pod := job.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Error("pod securityContext must set runAsNonRoot=true (restricted PSS)")
	}
	if pod.SecurityContext.SeccompProfile == nil || pod.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Error("pod securityContext must set seccompProfile=RuntimeDefault")
	}
	c := pod.Containers[0].SecurityContext
	if c == nil {
		t.Fatal("container securityContext must be set (restricted PSS)")
	}
	if c.AllowPrivilegeEscalation == nil || *c.AllowPrivilegeEscalation {
		t.Error("container must set allowPrivilegeEscalation=false")
	}
	if c.Capabilities == nil || len(c.Capabilities.Drop) == 0 || c.Capabilities.Drop[0] != "ALL" {
		t.Errorf("container must drop ALL capabilities; got %+v", c.Capabilities)
	}
}

func TestSeedableDatabases(t *testing.T) {
	pvc := func(dbs []string, typ neo4jv1beta1.BackupType) *neo4jv1beta1.Neo4jBackup {
		return &neo4jv1beta1.Neo4jBackup{Spec: neo4jv1beta1.Neo4jBackupSpec{
			Databases:   dbs,
			Type:        typ,
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationPVC, PVC: &neo4jv1beta1.BackupPVC{ClaimName: "b"}},
		}}
	}
	cases := []struct {
		name string
		b    *neo4jv1beta1.Neo4jBackup
		want bool
	}{
		{"pvc explicit full", pvc([]string{"neo4j"}, neo4jv1beta1.BackupTypeFull), true},
		{"pvc explicit auto", pvc([]string{"a", "b"}, neo4jv1beta1.BackupTypeAuto), true},
		// Incrementals are seedable now: restore points at this diff and Neo4j replays the chain.
		{"pvc incremental", pvc([]string{"neo4j"}, neo4jv1beta1.BackupTypeIncremental), true},
		{"pvc wildcard", pvc([]string{"*"}, neo4jv1beta1.BackupTypeFull), false},
		{"object store", &neo4jv1beta1.Neo4jBackup{Spec: neo4jv1beta1.Neo4jBackupSpec{
			Databases:   []string{"neo4j"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://b/"},
		}}, false},
	}
	for _, tc := range cases {
		if _, ok := SeedableDatabases(tc.b); ok != tc.want {
			t.Errorf("%s: SeedableDatabases ok=%v, want %v", tc.name, ok, tc.want)
		}
	}
}
