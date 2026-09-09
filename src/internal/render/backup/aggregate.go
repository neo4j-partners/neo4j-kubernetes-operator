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

// AggregateInputs is a resolved aggregate source for AggregateJob. Exactly one store is set:
//   - PVCClaim: the Job mounts the claim and aggregates each DBArtifacts[db] (the chain's last link,
//     chain-sub-dir included) in place, recording the recovered filename via /dev/termination-log.
//   - ObjectURL: an s3/gs/azb folder the Job aggregates each Database's chain under directly (ADR-016),
//     authenticated by Credentials and/or the target's workload identity. There is no mounted
//     filesystem to record the recovered filename from, so the caller seeds the folder instead.
type AggregateInputs struct {
	PVCClaim    string
	DBArtifacts map[string]string // PVC only: db -> artifact path (chain's last link)
	ObjectURL   string
	Databases   []string                        // object store: db operands to aggregate
	Credentials *neo4jv1beta1.BackupCredentials // object store: static-key Secret (nil → workload identity)
}

// AggregateJob builds the run-to-completion Job (named jobName, owner-referenced by the caller) that
// collapses each database's backup chain into a single recovered full artifact (`neo4j-admin backup
// aggregate`). For a PVC source it mounts the backups claim at the sub-path the backup Job wrote to
// and aggregates each database's latest artifact (dbArtifacts[db], possibly chain-sub-dir prefixed:
// the recovered full lands beside its inputs and the Job records the new path to
// /dev/termination-log). For an object-store source it runs the same command with --from-path set to
// the url and no mount — neo4j-admin reads/writes the chain in the bucket directly (S3 ≥5.19, GCS
// ≥5.21, Azure ≥5.24; MinIO via AWS_ENDPOINT_URL_S3), authenticated by the projected Credentials
// Secret and/or the target's workload identity.
//
// --keep-old-backup=true is mandatory: aggregation must never delete the user's existing chain
// (it is a read of the backups plus a new artifact, not a rewrite of them).
func AggregateJob(neo4j *neo4jv1beta1.Neo4j, jobName string, in AggregateInputs) (*batchv1.Job, error) {
	ctx := render.ClientServiceContext(neo4j)

	var (
		volumes []corev1.Volume
		mounts  []corev1.VolumeMount
		script  string
	)
	switch {
	case in.PVCClaim != "":
		// Reuse the backup Job's PVC wiring so the aggregate reads/writes at the same sub-path the
		// backup wrote to (storage.BackupsSubPath) — otherwise it would aggregate an empty directory.
		toPath, vols, mnts, err := destination(neo4jv1beta1.BackupDestination{
			Type: neo4jv1beta1.BackupDestinationPVC,
			PVC:  &neo4jv1beta1.BackupPVC{ClaimName: in.PVCClaim},
		}, "")
		if err != nil {
			return nil, err
		}
		volumes, mounts, script = vols, mnts, aggregateScript(toPath, in.DBArtifacts)
	case in.ObjectURL != "":
		// No mount: neo4j-admin streams the chain in/out of the object store itself. Credentials
		// are projected below; workload identity (if any) is applied via ApplyBackupPodIdentity.
		script = aggregateScriptObjectStore(in.ObjectURL, in.Databases)
	default:
		return nil, fmt.Errorf("aggregate inputs specify neither a PVC claim nor an object-store url")
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
		Command:                  []string{"sh", "-c", script},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             mounts,
		SecurityContext:          workload.ContainerSecurityContext(ctx),
	}
	// Object-store static keys (AWS_*, etc.) go verbatim as env — same projection the backup Job
	// uses. Absent (nil) means the chain is reachable via the target's workload identity.
	if in.Credentials != nil {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: in.Credentials.SecretName}},
		}}
	}
	if neo4j.Spec.Image != nil && neo4j.Spec.Image.PullPolicy != "" {
		container.ImagePullPolicy = corev1.PullPolicy(neo4j.Spec.Image.PullPolicy)
	}

	job := &batchv1.Job{
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
	}
	workload.ApplyBackupPodIdentity(neo4j, &job.Spec.Template)
	return job, nil
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

// aggregateScriptObjectStore runs `neo4j-admin backup aggregate` per database against the chain
// under the object-store url (in place, --keep-old-backup=true so the source chain is never
// deleted). Unlike the PVC path it records no recovered filename: there is no mounted filesystem to
// ls (and no cloud CLI in the image), and neo4j-admin writes the recovered full back under the same
// url, so restore seeds that folder — which now recovers to the aggregated full. Databases are
// sorted for a deterministic script.
func aggregateScriptObjectStore(url string, dbs []string) string {
	sorted := append([]string(nil), dbs...)
	sort.Strings(sorted)
	var b strings.Builder
	for i, db := range sorted {
		if i > 0 {
			b.WriteString(" && ")
		}
		fmt.Fprintf(&b,
			"neo4j-admin backup aggregate --from-path=%s --keep-old-backup=true --temp-path=%s %s",
			url, scratchMountPath, db)
	}
	return b.String()
}
