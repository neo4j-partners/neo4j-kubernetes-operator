package backup

import (
	"strings"
	"testing"
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
