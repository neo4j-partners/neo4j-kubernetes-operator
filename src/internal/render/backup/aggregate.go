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
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// AggregateJob builds the run-to-completion Job (named jobName, owner-referenced by the caller) that
// collapses each database's backup chain into a single recovered full artifact (`neo4j-admin backup
// aggregate`). It mounts the backups claim at the sub-path the backup Job wrote to and runs one
// aggregate per database over that database's latest artifact (dbArtifacts[db], the chain's last
// link recorded by the backup). dbArtifacts[db] is relative to the destination root, so it may
// carry a chain sub-directory (e.g. "<chain>/<file>"): the recovered full lands beside its inputs
// in that sub-dir, and the Job records the new artifact's path — chain-prefixed the same way — back
// to /dev/termination-log so the caller can catalog/seed file:/backups/<path>.
//
// --keep-old-backup=true is mandatory: aggregation must never delete the user's existing chain
// (it is a read of the backups plus a new artifact, not a rewrite of them).
func AggregateJob(neo4j *neo4jv1beta1.Neo4j, jobName, claim string, dbArtifacts map[string]string) (*batchv1.Job, error) {
	ctx := render.ClientServiceContext(neo4j)

	// Reuse the backup Job's PVC wiring so the aggregate reads/writes at the same sub-path the
	// backup wrote to (storage.BackupsSubPath) — otherwise it would aggregate an empty directory.
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
	mounts = append(mounts, corev1.VolumeMount{Name: scratchVolume, MountPath: scratchMountPath})

	labels := ctx.CommonLabels("backup-aggregate")
	ttl := backupTTLSeconds
	backoff := backupBackoff

	container := corev1.Container{
		Name:                     containerName,
		Image:                    ctx.ImageRef(),
		Command:                  []string{"sh", "-c", aggregateScript(toPath, dbArtifacts)},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             mounts,
		SecurityContext:          workload.ContainerSecurityContext(ctx),
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

// aggregateScript runs `neo4j-admin backup aggregate` per database over its latest artifact, then
// records the new recovered artifact's path to /dev/termination-log (same channel as backup, read
// back by the reconciler). The artifact path may carry a chain sub-directory, in which case the
// recovered full lands in that sub-dir: the ls scans there and the recorded path is chain-prefixed
// the same way, so restore/catalog resolve file:/backups/<path> identically to a plain backup.
// Databases are sorted for a deterministic script. The recording is best-effort (trailing `true`)
// so it never fails an otherwise-good aggregation.
func aggregateScript(toPath string, dbArtifacts map[string]string) string {
	dbs := make([]string, 0, len(dbArtifacts))
	for db := range dbArtifacts {
		dbs = append(dbs, db)
	}
	sort.Strings(dbs)

	var b strings.Builder
	for i, db := range dbs {
		if i > 0 {
			b.WriteString(" && ")
		}
		// The recovered full lands in the same directory as its inputs. When the artifact path is
		// chain-prefixed ("<chain>/<file>") that directory — and the recorded path — is the sub-dir.
		outDir, prefix := toPath, ""
		if idx := strings.LastIndex(dbArtifacts[db], "/"); idx >= 0 {
			sub := dbArtifacts[db][:idx]
			outDir = toPath + "/" + sub
			prefix = sub + "/"
		}
		fmt.Fprintf(&b,
			"neo4j-admin backup aggregate --from-path=%s/%s --keep-old-backup=true --temp-path=%s",
			toPath, dbArtifacts[db], scratchMountPath)
		fmt.Fprintf(&b,
			" && { a=\"$(ls -t %s/%s-*.backup 2>/dev/null | head -1)\"; [ -n \"$a\" ] && echo \"%s=%s$(basename \"$a\")|$(stat -c%%s \"$a\" 2>/dev/null)\" >> /dev/termination-log 2>/dev/null; true; }",
			outDir, db, db, prefix)
	}
	return b.String()
}
