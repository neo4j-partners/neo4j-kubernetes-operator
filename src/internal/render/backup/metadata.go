/*
Copyright Neo4j.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package backup

import (
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// metaScratchMountPath is the throwaway data dir the metadata Job restores into purely to make
// neo4j-admin emit restore_metadata.cypher; the store it writes there is never used.
const metaScratchMountPath = "/tmp/neo4j-meta"

// MetadataJobName is the deterministic Job name for a Neo4jRestore's post-seed metadata apply
// (owner-referenced by the restore).
func MetadataJobName(restore *neo4jv1beta1.Neo4jRestore) string { return restore.Name + "-metadata" }

// MetadataJob builds the run-to-completion Job (named jobName, owner-referenced by the caller) that
// reapplies the backed-up users, roles, and privileges after a seed-from-URI restore (which carries
// store data only). Seeding never emits Neo4j's restore_metadata.cypher, so per database the Job
// regenerates it — `neo4j-admin database restore` into a throwaway data dir emits
// <data>/scripts/<db>/restore_metadata.cypher — then executes it against the target's system
// database with cypher-shell (`cat script | cypher-shell -d system`, the documented flow).
//
// dbArtifacts[db] is the recorded artifact path relative to the backups destination root (may carry
// a chain sub-directory), mounted read at the same sub-path the backup wrote to. It connects to the
// client Service over Bolt with the NEO4J_AUTH secret.
//
// Failure split (matches spec.restoreMetadata's warn-on-conflict contract): a connectivity probe
// and the neo4j-admin regeneration run under `set -e`, so a bad artifact or an unreachable system
// database fails the Job (→ RestoreMetadataFailed). Only after the probe succeeds does it apply the
// scripts with `cypher-shell --fail-at-end`; statement errors there (a role/user already exists) are
// recorded as warnings and the Job still exits 0, so the restore Succeeds with a Warning event.
func MetadataJob(neo4j *neo4jv1beta1.Neo4j, jobName, claim string, dbArtifacts map[string]string) (*batchv1.Job, error) {
	ctx := render.ClientServiceContext(neo4j)

	// Mount the backups claim read-side at the same sub-path the backup Job wrote to, so
	// <toPath>/<dbArtifacts[db]> resolves the recorded artifact.
	toPath, volumes, mounts, err := destination(neo4jv1beta1.BackupDestination{
		Type: neo4jv1beta1.BackupDestinationPVC,
		PVC:  &neo4jv1beta1.BackupPVC{ClaimName: claim},
	}, "")
	if err != nil {
		return nil, err
	}
	volumes = append(volumes, corev1.Volume{
		Name:         scratchVolume,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	mounts = append(mounts, corev1.VolumeMount{Name: scratchVolume, MountPath: metaScratchMountPath})

	labels := ctx.CommonLabels("restore-metadata")
	ttl := backupTTLSeconds
	backoff := backupBackoff

	// Match the target's Bolt listener: plaintext by default, but when bolt TLS is enabled the
	// listener is REQUIRED-TLS so a neo4j:// dial is refused. cypher-shell has no flag to trust the
	// operator's custom CA file, so use +ssc — encrypted, skipping server-cert verification — which
	// still protects the metadata (users, roles, privileges) in transit.
	// ponytail: +ssc skips server-identity verification (in-cluster dial to the derived client
	// Service); the upgrade path is importing the bolt CA into a JVM truststore and using neo4j+s.
	scheme := "neo4j"
	if rendertrust.BoltTLSEnabled(neo4j) {
		scheme = "neo4j+ssc"
	}
	boltAddr := fmt.Sprintf("%s://%s.%s.svc:%d", scheme, ctx.ClientServiceName(), ctx.Namespace(), ctx.BoltPort())
	container := corev1.Container{
		Name:                     containerName,
		Image:                    ctx.ImageRef(),
		Command:                  []string{"sh", "-c", metadataScript(toPath, boltAddr, dbArtifacts)},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             mounts,
		SecurityContext:          workload.ContainerSecurityContext(ctx),
		Env: []corev1.EnvVar{{
			Name: "NEO4J_AUTH",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ctx.AuthSecretName()},
					Key:                  "NEO4J_AUTH",
				},
			},
		}},
	}
	if neo4j.Spec.Image != nil && neo4j.Spec.Image.PullPolicy != "" {
		container.ImagePullPolicy = corev1.PullPolicy(neo4j.Spec.Image.PullPolicy)
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ctx.Namespace(),
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyNever,
					Containers:      []corev1.Container{container},
					Volumes:         volumes,
					SecurityContext: workload.PodSecurityContext(ctx),
				},
			},
		},
	}, nil
}

// metadataScript regenerates and applies restore_metadata.cypher per database. Databases are sorted
// for a deterministic script. NEO4J_AUTH is "neo4j/<password>"; the password is everything after
// the first "/". The connectivity probe and neo4j-admin regeneration run under `set -e` (infra
// failures fail the Job); the apply loop runs under `set +e` so a clashing role/user is a recorded
// warning, not a Job failure. The script's location under --to-path-data is not precisely
// documented, so it is resolved with `find` (robust across Neo4j versions).
func metadataScript(toPath, boltAddr string, dbArtifacts map[string]string) string {
	dbs := make([]string, 0, len(dbArtifacts))
	for db := range dbArtifacts {
		dbs = append(dbs, db)
	}
	sort.Strings(dbs)

	var b strings.Builder
	b.WriteString("set -e; p=\"${NEO4J_AUTH#*/}\"; ")
	// Connectivity probe: a failure here (system db unreachable / bad creds) is infra → fail the Job.
	// Must be a system-database-legal statement — Neo4j 2025.x+ rejects data queries (e.g. RETURN 1)
	// on the system database — so probe with the admin command SHOW DATABASES.
	fmt.Fprintf(&b, "cypher-shell -a %s -u neo4j -p \"$p\" -d system 'SHOW DATABASES;' >/dev/null; ", boltAddr)
	// Regenerate each database's restore_metadata.cypher into a throwaway data dir (infra-critical).
	for _, db := range dbs {
		fmt.Fprintf(&b,
			"mkdir -p %s/%s; neo4j-admin database restore --from-path=%s/%s --to-path-data=%s/%s %s; ",
			metaScratchMountPath, db, toPath, dbArtifacts[db], metaScratchMountPath, db, db)
	}
	// Apply phase: statement errors (already-exists) are warnings, never Job failures.
	b.WriteString("set +e; warn=0; ")
	for _, db := range dbs {
		// restore_metadata.cypher's location under --to-path-data is not precisely documented and
		// may land in the image's default data dir instead, so search both (the path filter keeps
		// it to this database's script).
		fmt.Fprintf(&b,
			"s=\"$(find %s/%s /var/lib/neo4j/data /data -path '*/scripts/%s/restore_metadata.cypher' 2>/dev/null | head -1)\"; ",
			metaScratchMountPath, db, db)
		fmt.Fprintf(&b,
			"if [ -n \"$s\" ]; then out=\"$(cypher-shell -a %s -u neo4j -p \"$p\" -d system --fail-at-end --param \"database => '%s'\" -f \"$s\" 2>&1)\"; "+
				"[ $? -ne 0 ] && { warn=1; echo \"%s: $out\" | head -c 800 >> /dev/termination-log; }; fi; ",
			boltAddr, db, db)
	}
	b.WriteString("if [ \"$warn\" -ne 0 ]; then echo 'metadata-applied-with-warnings' >> /dev/termination-log; else echo 'metadata-applied' >> /dev/termination-log; fi; exit 0")
	return b.String()
}
