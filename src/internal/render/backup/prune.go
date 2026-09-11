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
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// DefaultObjectStorePruneImage is the rclone image the object-store prune Job runs when the operator
// is not configured with an override (--object-store-prune-image / OBJECT_STORE_PRUNE_IMAGE). rclone
// is the one small, mirrorable tool that deletes across S3/GCS/Azure with the same env-configured
// auth the backup Job already uses (ADR-016's named prune-tool seam).
const DefaultObjectStorePruneImage = "rclone/rclone:1.69"

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

	job := &batchv1.Job{
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
	}
	workload.ApplyBackupPodIdentity(neo4j, &job.Spec.Template)
	return job, nil
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

// ObjectStorePruneJob builds the run-to-completion Job that deletes one expired backup chain's whole
// prefix from an object store (S3/GCS/Azure) with rclone (ADR-016 — the operator's own images carry
// no cloud CLI, so a dedicated prune tool authenticated by the same identity model does the delete).
// url is the chain's recorded folder (Neo4jBackup.status.artifacts[].uri); credentials, when set,
// are the same static-key Secret the backup Job used, projected as env so rclone's env_auth picks
// them up; workload identity is applied via ApplyBackupPodIdentity exactly as for the backup pods.
// image defaults to DefaultObjectStorePruneImage. It is a pure function; the owner reference is
// applied by shared.Apply.
func ObjectStorePruneJob(neo4j *neo4jv1beta1.Neo4j, name, image string, dest neo4jv1beta1.BackupDestination, url string) (*batchv1.Job, error) {
	ctx := render.ClientServiceContext(neo4j)
	script, err := objectStorePruneScript(dest, url)
	if err != nil {
		return nil, err
	}
	if image == "" {
		image = DefaultObjectStorePruneImage
	}

	labels := ctx.CommonLabels("backup-prune")
	ttl := backupTTLSeconds
	backoff := backupBackoff

	container := corev1.Container{
		Name:                     containerName,
		Image:                    image,
		Command:                  []string{"sh", "-c", script},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		SecurityContext:          workload.ContainerSecurityContext(ctx),
	}
	if dest.Credentials != nil {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: dest.Credentials.SecretName}},
		}}
	}
	if neo4j.Spec.Image != nil && neo4j.Spec.Image.PullPolicy != "" {
		container.ImagePullPolicy = corev1.PullPolicy(neo4j.Spec.Image.PullPolicy)
	}

	job := &batchv1.Job{
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
					SecurityContext: workload.PodSecurityContext(ctx),
				},
			},
		},
	}
	workload.ApplyBackupPodIdentity(neo4j, &job.Spec.Template)
	return job, nil
}

// objectStorePruneScript builds the rclone script that purges one chain's folder from an object
// store. Auth is env-configured (RCLONE_<BACKEND>_ENV_AUTH=true) so the same static-key Secret
// (projected as envFrom) or workload identity the backup Job uses also authorizes the delete —
// which requires s3:DeleteObject / Azure "Storage Blob Data Contributor" / GCS storage.objects.delete
// on the identity. `purge` removes the prefix and everything under it; the follow-up `lsf` makes the
// Job succeed only when the prefix is actually empty, so a swallowed purge error can never let the
// caller delete the Neo4jBackup records while blobs remain (retention must never orphan objects).
// Purging an already-gone prefix is tolerated (idempotent re-runs). The url always carries the
// per-chain sub-path, and the empty-remote guard refuses to purge a bucket/container root.
func objectStorePruneScript(dest neo4jv1beta1.BackupDestination, url string) (string, error) {
	var backend, remote, pre string
	switch dest.Type {
	case neo4jv1beta1.BackupDestinationS3:
		backend = "s3"
		remote = strings.TrimPrefix(url, "s3://")
		// MinIO / non-AWS endpoints (AWS_ENDPOINT_URL_S3, set by the same creds Secret neo4j-admin
		// reads) need path-style addressing and a non-AWS provider; plain AWS otherwise.
		pre = `export RCLONE_S3_ENV_AUTH=true; ` +
			`if [ -n "${AWS_ENDPOINT_URL_S3:-}" ]; then export RCLONE_S3_ENDPOINT="$AWS_ENDPOINT_URL_S3" RCLONE_S3_PROVIDER=Other RCLONE_S3_FORCE_PATH_STYLE=true; else export RCLONE_S3_PROVIDER=AWS; fi; `
	case neo4jv1beta1.BackupDestinationGCS:
		backend = "gcs"
		remote = strings.TrimPrefix(url, "gs://")
		pre = `export RCLONE_GCS_ENV_AUTH=true; `
	case neo4jv1beta1.BackupDestinationAzure:
		backend = "azureblob"
		rest := strings.TrimPrefix(url, "azb://")
		i := strings.IndexByte(rest, '/')
		if i <= 0 {
			return "", fmt.Errorf("azure url %q must be azb://<account>/<container>/<path>", url)
		}
		remote = rest[i+1:]
		pre = `export RCLONE_AZUREBLOB_ENV_AUTH=true RCLONE_AZUREBLOB_ACCOUNT=` + singleQuote(rest[:i]) + `; `
	default:
		return "", fmt.Errorf("object-store prune unsupported for destination type %q", dest.Type)
	}
	remote = strings.TrimSuffix(remote, "/")
	if remote == "" || !strings.Contains(remote, "/") {
		// Refuse to purge a bucket/container root — a chain folder always carries a sub-path.
		return "", fmt.Errorf("object-store prune requires a chain sub-path; got url %q", url)
	}
	target := singleQuote(":" + backend + ":" + remote)
	return pre +
		`rclone purge ` + target + ` || true; ` +
		`if rclone lsf ` + target + ` 2>/dev/null | grep -q . ; then echo "object-store prune left objects under ` + remote + `" >&2; exit 1; fi`, nil
}

// singleQuote wraps s for safe use inside a POSIX sh single-quoted context (bucket names, chain ids,
// and prefixes are path-safe, but a stray quote must not break out).
func singleQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
