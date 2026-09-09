package backup

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestAggregateJob(t *testing.T) {
	job, err := AggregateJob(testNeo4j(), "nr-aggregate", AggregateInputs{PVCClaim: "backups", DBArtifacts: map[string]string{"neo4j": "neo4j-2026-09-01T15-08-49.backup"}})
	if err != nil {
		t.Fatalf("AggregateJob: %v", err)
	}
	if job.Name != "nr-aggregate" {
		t.Errorf("job name = %q, want nr-aggregate", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 3 || c.Command[0] != "sh" || c.Command[1] != "-c" {
		t.Fatalf("expected sh -c wrapper; got %v", c.Command)
	}
	script := c.Command[2]
	for _, want := range []string{
		"neo4j-admin backup aggregate",
		"--from-path=/" + pvcMountPath + "/neo4j-2026-09-01T15-08-49.backup",
		"--keep-old-backup=true",
		"--temp-path=" + scratchMountPath,
		"ls -t /" + pvcMountPath + "/neo4j-*.backup",
		"/dev/termination-log",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q; got: %s", want, script)
		}
	}
	// Must read/write the same backups sub-path the backup Job used, or it aggregates an empty dir.
	var mount *corev1.VolumeMount
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == pvcVolume {
			mount = &c.VolumeMounts[i]
		}
	}
	if mount == nil || mount.SubPath != pvcSubPath {
		t.Errorf("aggregate PVC mount subPath = %v, want %q", mount, pvcSubPath)
	}
	// Hardened for restricted PSS (same as the backup Job).
	if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container must set allowPrivilegeEscalation=false")
	}
	if c.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Errorf("terminationMessagePolicy = %q, want FallbackToLogsOnError", c.TerminationMessagePolicy)
	}
}

func TestAggregateJobChainSubDir(t *testing.T) {
	// A chain-prefixed artifact path makes the aggregate read/write and record inside that sub-dir.
	job, err := AggregateJob(testNeo4j(), "agg", AggregateInputs{PVCClaim: "backups",
		DBArtifacts: map[string]string{"neo4j": "sch-20260903-0100/neo4j-2026-09-03T01-04-00.backup"}})
	if err != nil {
		t.Fatalf("AggregateJob: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Command[2]
	for _, want := range []string{
		"--from-path=/" + pvcMountPath + "/sch-20260903-0100/neo4j-2026-09-03T01-04-00.backup",
		// recovered full is scanned for — and recorded — inside the same chain sub-dir.
		"ls -t /" + pvcMountPath + "/sch-20260903-0100/neo4j-*.backup",
		"neo4j=sch-20260903-0100/$(basename",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q; got: %s", want, script)
		}
	}
}

func TestAggregateJobObjectStore(t *testing.T) {
	// Object-store source: aggregate the chain under the url in place, no PVC mount, credentials
	// projected as env. Two databases must produce a deterministic (sorted) &&-chained script.
	job, err := AggregateJob(testNeo4j(), "agg-obj", AggregateInputs{
		ObjectURL:   "s3://bkt/backups/neo4j/",
		Databases:   []string{"neo4j", "customers"},
		Credentials: &neo4jv1beta1.BackupCredentials{SecretName: "s3-creds"},
	})
	if err != nil {
		t.Fatalf("AggregateJob: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	script := c.Command[2]
	want := "neo4j-admin backup aggregate --from-path=s3://bkt/backups/neo4j/ --keep-old-backup=true --temp-path=" +
		scratchMountPath + " customers && neo4j-admin backup aggregate --from-path=s3://bkt/backups/neo4j/ --keep-old-backup=true --temp-path=" +
		scratchMountPath + " neo4j"
	if script != want {
		t.Errorf("object-store aggregate script =\n  %q\nwant\n  %q", script, want)
	}
	if strings.Contains(script, "/dev/termination-log") {
		t.Error("object-store aggregate must not try to record a recovered filename (no mounted filesystem)")
	}
	if len(c.EnvFrom) != 1 || c.EnvFrom[0].SecretRef == nil || c.EnvFrom[0].SecretRef.Name != "s3-creds" {
		t.Errorf("EnvFrom = %+v, want the credentials Secret projected", c.EnvFrom)
	}
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			t.Errorf("object-store aggregate must not mount a PVC; got %q", v.Name)
		}
	}
	// Scratch temp-path is still needed for neo4j-admin's working files.
	var scratch bool
	for _, m := range c.VolumeMounts {
		if m.Name == scratchVolume && m.MountPath == scratchMountPath {
			scratch = true
		}
	}
	if !scratch {
		t.Error("object-store aggregate must still mount the scratch temp volume")
	}
}
