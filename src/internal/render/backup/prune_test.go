package backup

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestPruneJob(t *testing.T) {
	job, err := PruneJob(testNeo4j(), PruneJobName("sch-20260902-1100"), "backups",
		[]string{"neo4j-2026-09-02T11-00-00.backup", "neo4j-2026-09-02T11-05-00.backup"})
	if err != nil {
		t.Fatalf("PruneJob: %v", err)
	}
	if job.Name != "prune-sch-20260902-1100" {
		t.Errorf("job name = %q, want prune-sch-20260902-1100", job.Name)
	}

	c := job.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 3 || c.Command[0] != "sh" || c.Command[1] != "-c" {
		t.Fatalf("command = %v, want [sh -c <script>]", c.Command)
	}
	script := c.Command[2]
	// Each file is removed at the destination mount, quoted (artifact names carry colons).
	for _, want := range []string{
		"rm -f '/destination/neo4j-2026-09-02T11-00-00.backup'",
		"rm -f '/destination/neo4j-2026-09-02T11-05-00.backup'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q; got %q", want, script)
		}
	}
	// set -e so a failed delete fails the Job (retention retries) rather than silently passing.
	if !strings.HasPrefix(script, "set -e") {
		t.Errorf("script must start with set -e; got %q", script)
	}

	// The claim is mounted at the backup sub-path so recorded paths resolve.
	if n := len(job.Spec.Template.Spec.Volumes); n != 1 {
		t.Fatalf("volumes = %d, want 1 (the backups claim)", n)
	}
	v := job.Spec.Template.Spec.Volumes[0]
	if v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != "backups" {
		t.Errorf("volume = %+v, want PVC claim backups", v)
	}
	if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container must run with a hardened security context (allowPrivilegeEscalation=false)")
	}
}

func TestPruneScriptEmptyIsNoop(t *testing.T) {
	if got := pruneScript("/destination", nil); got != "set -e" {
		t.Errorf("empty prune script = %q, want just \"set -e\"", got)
	}
}

func TestPruneScriptRemovesEmptyChainSubDir(t *testing.T) {
	// Chain-isolated artifacts live under a per-chain sub-dir; once their files are removed the now-
	// empty directory must be reclaimed (rmdir only removes it if empty, so a kept recovered full
	// leaves it intact). The sub-dir is removed exactly once, after the files.
	got := pruneScript("/destination", []string{
		"c-20260904-1040/neo4j-a.backup",
		"c-20260904-1040/neo4j-b.backup",
	})
	for _, want := range []string{
		"rm -f '/destination/c-20260904-1040/neo4j-a.backup'",
		"rm -f '/destination/c-20260904-1040/neo4j-b.backup'",
		"rmdir '/destination/c-20260904-1040' 2>/dev/null || true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("script missing %q; got %q", want, got)
		}
	}
	if n := strings.Count(got, "rmdir '/destination/c-20260904-1040'"); n != 1 {
		t.Errorf("chain sub-dir must be rmdir'd exactly once, got %d; script %q", n, got)
	}
	// The rmdir must come after the deletes.
	if strings.Index(got, "rmdir") < strings.LastIndex(got, "rm -f") {
		t.Errorf("rmdir must follow the file deletes; got %q", got)
	}
}

func TestPruneScriptFlatFilesLeaveRootAlone(t *testing.T) {
	// Ad-hoc (flat) backups have no sub-dir component; the destination root must never be rmdir'd.
	got := pruneScript("/destination", []string{"neo4j-a.backup"})
	if strings.Contains(got, "rmdir") {
		t.Errorf("flat files must not trigger any rmdir; got %q", got)
	}
}

func TestObjectStorePruneJob(t *testing.T) {
	dest := neo4jv1beta1.BackupDestination{
		Type:        neo4jv1beta1.BackupDestinationS3,
		URL:         "s3://bkt/prod/",
		Credentials: &neo4jv1beta1.BackupCredentials{SecretName: "s3-creds"},
	}
	job, err := ObjectStorePruneJob(testNeo4j(), PruneJobName("old"), "", dest, "s3://bkt/prod/old-chain/")
	if err != nil {
		t.Fatalf("ObjectStorePruneJob: %v", err)
	}
	if job.Name != "prune-old" {
		t.Errorf("job name = %q, want prune-old", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Image != DefaultObjectStorePruneImage {
		t.Errorf("image = %q, want default %q", c.Image, DefaultObjectStorePruneImage)
	}
	script := c.Command[2]
	// Purges exactly the chain prefix, then verifies it is empty so a swallowed error can't let the
	// caller delete records with blobs still present.
	if !strings.Contains(script, "rclone purge ':s3:bkt/prod/old-chain'") {
		t.Errorf("script must purge the chain prefix; got %q", script)
	}
	if !strings.Contains(script, "rclone lsf ':s3:bkt/prod/old-chain'") || !strings.Contains(script, "exit 1") {
		t.Errorf("script must verify the prefix is empty and fail otherwise; got %q", script)
	}
	// The same static-key Secret the backup Job used is projected so rclone env_auth authorizes the delete.
	if len(c.EnvFrom) != 1 || c.EnvFrom[0].SecretRef == nil || c.EnvFrom[0].SecretRef.Name != "s3-creds" {
		t.Errorf("EnvFrom = %+v, want the credentials Secret projected", c.EnvFrom)
	}
	// No PVC mount (the object store is remote).
	if len(job.Spec.Template.Spec.Volumes) != 0 {
		t.Errorf("object-store prune must mount no volumes; got %v", job.Spec.Template.Spec.Volumes)
	}
	if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container must run with a hardened security context")
	}
}

func TestObjectStorePruneScriptPerProvider(t *testing.T) {
	cases := []struct {
		name     string
		dest     neo4jv1beta1.BackupDestinationType
		url      string
		wantAny  []string
		wantFail bool
	}{
		{"s3", neo4jv1beta1.BackupDestinationS3, "s3://bkt/p/old/",
			[]string{"RCLONE_S3_ENV_AUTH=true", "AWS_ENDPOINT_URL_S3", ":s3:bkt/p/old"}, false},
		{"gcs", neo4jv1beta1.BackupDestinationGCS, "gs://bkt/p/old/",
			[]string{"RCLONE_GCS_ENV_AUTH=true", ":gcs:bkt/p/old"}, false},
		{"azure", neo4jv1beta1.BackupDestinationAzure, "azb://acct/cont/p/old/",
			[]string{"RCLONE_AZUREBLOB_ACCOUNT='acct'", ":azureblob:cont/p/old"}, false},
		// Guards: never purge a bucket/container root, and reject malformed azure urls.
		{"s3 root", neo4jv1beta1.BackupDestinationS3, "s3://bkt/", nil, true},
		{"azure no container", neo4jv1beta1.BackupDestinationAzure, "azb://acct", nil, true},
		{"pvc unsupported", neo4jv1beta1.BackupDestinationPVC, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := objectStorePruneScript(neo4jv1beta1.BackupDestination{Type: tc.dest}, tc.url)
			if tc.wantFail {
				if err == nil {
					t.Fatalf("want error for %s url %q; got script %q", tc.dest, tc.url, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("objectStorePruneScript: %v", err)
			}
			for _, w := range tc.wantAny {
				if !strings.Contains(got, w) {
					t.Errorf("script missing %q; got %q", w, got)
				}
			}
		})
	}
}
