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
