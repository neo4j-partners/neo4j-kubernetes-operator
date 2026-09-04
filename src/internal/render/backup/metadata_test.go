package backup

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMetadataJob(t *testing.T) {
	job, err := MetadataJob(testNeo4j(), "nr-metadata", "backups", map[string]string{"neo4j": "neo4j.latest.backup"})
	if err != nil {
		t.Fatalf("MetadataJob: %v", err)
	}
	if job.Name != "nr-metadata" {
		t.Errorf("job name = %q, want nr-metadata", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 3 || c.Command[0] != "sh" || c.Command[1] != "-c" {
		t.Fatalf("expected sh -c wrapper; got %v", c.Command)
	}
	script := c.Command[2]
	for _, want := range []string{
		"set -e",                            // probe + regeneration are infra-critical
		"set +e",                            // apply phase: statement errors are warnings, not failures
		"cypher-shell -a neo4j://g.ns.svc:", // dials the client Service over Bolt
		"-d system",                         // metadata is applied against the system database
		"--fail-at-end",                     // one bad statement never aborts the rest (warn+skip)
		"neo4j-admin database restore --from-path=/" + pvcMountPath + "/neo4j.latest.backup",
		"restore_metadata.cypher",
		"metadata-applied",
		"/dev/termination-log",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q; got: %s", want, script)
		}
	}

	// Credentials come from the auth Secret as NEO4J_AUTH (parsed to the password in-script).
	var auth *corev1.EnvVar
	for i := range c.Env {
		if c.Env[i].Name == "NEO4J_AUTH" {
			auth = &c.Env[i]
		}
	}
	if auth == nil || auth.ValueFrom == nil || auth.ValueFrom.SecretKeyRef == nil || auth.ValueFrom.SecretKeyRef.Key != "NEO4J_AUTH" {
		t.Errorf("expected NEO4J_AUTH env from a SecretKeyRef(key=NEO4J_AUTH); got %+v", auth)
	}

	// Reads the artifact at the same backups sub-path the backup Job wrote to.
	var mount *corev1.VolumeMount
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == pvcVolume {
			mount = &c.VolumeMounts[i]
		}
	}
	if mount == nil || mount.SubPath != pvcSubPath {
		t.Errorf("metadata PVC mount subPath = %v, want %q", mount, pvcSubPath)
	}

	// Hardened for restricted PSS (same as the other backup Jobs).
	if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container must set allowPrivilegeEscalation=false")
	}
	if c.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Errorf("terminationMessagePolicy = %q, want FallbackToLogsOnError", c.TerminationMessagePolicy)
	}
}

func TestMetadataJobChainSubDir(t *testing.T) {
	// A chain-prefixed artifact path makes the restore read the artifact inside that sub-dir.
	job, err := MetadataJob(testNeo4j(), "md", "backups",
		map[string]string{"neo4j": "sch-20260903-0100/neo4j-2026-09-03T01-04-00.backup"})
	if err != nil {
		t.Fatalf("MetadataJob: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Command[2]
	if want := "--from-path=/" + pvcMountPath + "/sch-20260903-0100/neo4j-2026-09-03T01-04-00.backup"; !strings.Contains(script, want) {
		t.Errorf("script missing %q; got: %s", want, script)
	}
}
