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
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// PruneJobName is the deterministic Job name for pruning one expired chain's PVC artifacts. Chain
// ids are unique per schedule tick, so the name never collides across prune waves.
func PruneJobName(chain string) string { return "prune-" + chain }

// PruneJob builds the run-to-completion Job that removes an expired backup chain's artifact files
// from a PVC-backed backups claim (BDR-014 §10 — retention prunes whole chains, never mid-chain
// links). It mounts the claim at the same sub-path the backup Job wrote to (storage.BackupsSubPath),
// so each file — recorded on Neo4jBackup.status.artifacts[].path — resolves under the mount. It is
// a pure function; the owner reference (the Neo4jBackupSchedule) is applied by shared.Apply.
func PruneJob(neo4j *neo4jv1beta1.Neo4j, name, claim string, files []string) (*batchv1.Job, error) {
	ctx := render.ClientServiceContext(neo4j)

	// Reuse the backup Job's PVC wiring so we delete at the exact path the backup wrote to.
	toPath, volumes, mounts, err := destination(neo4jv1beta1.BackupDestination{
		Type: neo4jv1beta1.BackupDestinationPVC,
		PVC:  &neo4jv1beta1.BackupPVC{ClaimName: claim},
	}, "")
	if err != nil {
		return nil, err
	}

	labels := ctx.CommonLabels("backup-prune")
	ttl := backupTTLSeconds
	backoff := backupBackoff

	container := corev1.Container{
		Name:                     containerName,
		Image:                    ctx.ImageRef(),
		Command:                  []string{"sh", "-c", pruneScript(toPath, files)},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             mounts,
		SecurityContext:          workload.ContainerSecurityContext(ctx),
	}
	if neo4j.Spec.Image != nil && neo4j.Spec.Image.PullPolicy != "" {
		container.ImagePullPolicy = corev1.PullPolicy(neo4j.Spec.Image.PullPolicy)
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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

// pruneScript removes each recorded artifact file from the destination directory. `set -e` fails
// the Job if a delete errors (so retention can be retried), while `rm -f` stays idempotent across a
// re-run after a partial delete. Filenames are single-quoted because artifacts carry timestamps
// with colons. Deleting a whole chain's files is safe: they are contiguous on disk and nothing
// outside the chain depends on them (BDR-014 §10). A chain with no recorded files yields a no-op.
//
// After the files are gone it removes each per-chain sub-directory the files lived in. `rmdir` only
// deletes an empty directory, so a compaction prune that keeps the chain's recovered full leaves the
// directory intact; a full-chain expiry empties it and reclaims the folder. Failures (directory not
// empty, or already gone) are swallowed with `|| true` so retention stays idempotent and never
// touches the destination root (only paths that carry a sub-directory component are considered).
func pruneScript(toPath string, files []string) string {
	var b strings.Builder
	b.WriteString("set -e")
	seen := map[string]bool{}
	var subDirs []string
	for _, f := range files {
		b.WriteString(" && rm -f '")
		b.WriteString(toPath)
		b.WriteString("/")
		b.WriteString(f)
		b.WriteString("'")
		if i := strings.LastIndex(f, "/"); i > 0 {
			if d := f[:i]; !seen[d] {
				seen[d] = true
				subDirs = append(subDirs, d)
			}
		}
	}
	for _, d := range subDirs {
		b.WriteString(" ; rmdir '")
		b.WriteString(toPath)
		b.WriteString("/")
		b.WriteString(d)
		b.WriteString("' 2>/dev/null || true")
	}
	return b.String()
}
