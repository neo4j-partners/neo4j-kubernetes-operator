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

// Package backup renders the Kubernetes objects that execute a Neo4jBackup (ADR-015):
// one run-to-completion Job per backup that runs `neo4j-admin database backup` against the
// target's backup listener and streams artifacts to the destination.
package backup

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	// scratchMountPath is the local staging dir neo4j-admin uses before streaming to the
	// destination (--temp-path). Under-provisioning here is a common backup failure (ADR-015).
	scratchMountPath = "/tmp/neo4j-backup"
	scratchVolume    = "scratch"
	// pvcMountPath is where a PVC destination is mounted inside the Job pod.
	pvcMountPath = "destination"
	pvcVolume    = "destination"
	// backupTTLSeconds keeps a finished Job (and its logs) around for a day so the cause of a
	// failure survives GC (ADR-015 — logs & observability).
	// ponytail: fixed 24h; upgrade path is a schedule/operator flag when someone needs it.
	backupTTLSeconds int32 = 86400
	backupBackoff    int32 = 3
	containerName          = "neo4j-admin"
)

// JobName is the deterministic Job name for a Neo4jBackup (owner-referenced by it).
func JobName(backup *neo4jv1beta1.Neo4jBackup) string { return backup.Name + "-backup" }

// DestinationURI is the stable, user-facing location of a backup's artifacts, recorded on
// each Neo4jBackup.status.artifacts[] so Neo4jRestore.source.backupRef can resolve it (BDR-014
// §13). Object stores report the url; a PVC reports pvc://<claimName>.
func DestinationURI(d neo4jv1beta1.BackupDestination) string {
	if d.Type == neo4jv1beta1.BackupDestinationPVC {
		if d.PVC != nil && d.PVC.ClaimName != "" {
			return "pvc://" + d.PVC.ClaimName
		}
		return "pvc://"
	}
	return d.URL
}

// AdminType maps the API backup type to the neo4j-admin --type value (BDR-014 §9).
func AdminType(t neo4jv1beta1.BackupType) string {
	switch t {
	case neo4jv1beta1.BackupTypeFull:
		return "FULL"
	case neo4jv1beta1.BackupTypeIncremental:
		return "DIFF"
	default:
		return "AUTO"
	}
}

// FromAddress is the backup-listener address neo4j-admin dials (--from). It uses the derived
// admin Service (created when backup is enabled) over short in-cluster DNS — never the
// CR-author-controlled clusterDomain (mirrors formation.ClientBoltURI).
func FromAddress(ctx render.Context) string {
	return fmt.Sprintf("%s.%s.svc:%d", ctx.AdminServiceName(), ctx.Namespace(), ctx.BackupPort())
}

// BackupJob builds the run-to-completion Job for a Neo4jBackup. It is a pure function; the
// owner/controller reference is applied by shared.Apply.
func BackupJob(neo4j *neo4jv1beta1.Neo4j, backup *neo4jv1beta1.Neo4jBackup) (*batchv1.Job, error) {
	ctx := render.ClientServiceContext(neo4j)

	toPath, volumes, mounts, err := destination(backup.Spec.Destination)
	if err != nil {
		return nil, err
	}
	// Scratch staging volume for --temp-path.
	volumes = append(volumes, corev1.Volume{
		Name:         scratchVolume,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	mounts = append(mounts, corev1.VolumeMount{Name: scratchVolume, MountPath: scratchMountPath})

	labels := ctx.CommonLabels("backup")
	ttl := backupTTLSeconds
	backoff := backupBackoff

	container := corev1.Container{
		Name:         containerName,
		Image:        ctx.ImageRef(),
		Command:      []string{"neo4j-admin"},
		Args:         backupArgs(ctx, backup, toPath),
		VolumeMounts: mounts,
	}
	if neo4j.Spec.Image != nil && neo4j.Spec.Image.PullPolicy != "" {
		container.ImagePullPolicy = corev1.PullPolicy(neo4j.Spec.Image.PullPolicy)
	}
	// Cloud credentials (if any) are projected as env into the Job pod only (ADR-015): the
	// user's Secret keys (AWS_ACCESS_KEY_ID, GOOGLE_APPLICATION_CREDENTIALS, …) go verbatim.
	if creds := backup.Spec.Destination.Credentials; creds != nil {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: creds.SecretName}},
		}}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobName(backup),
			Namespace: ctx.Namespace(),
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{container},
					Volumes:       volumes,
				},
			},
		},
	}, nil
}

// destination resolves the neo4j-admin --to-path plus any volumes/mounts the store needs.
// Object stores map straight to the url; a PVC is mounted and the path is the mount.
func destination(d neo4jv1beta1.BackupDestination) (toPath string, volumes []corev1.Volume, mounts []corev1.VolumeMount, err error) {
	if d.Type == neo4jv1beta1.BackupDestinationPVC {
		if d.PVC == nil || d.PVC.ClaimName == "" {
			// ponytail: PVC provisioning (size/storageClassName) is a follow-up; require an
			// existing claim for now. Upgrade path: render a PVC like BDR-005 dynamic volumes.
			return "", nil, nil, fmt.Errorf("pvc destination requires an existing pvc.claimName (provisioning not yet supported)")
		}
		volumes = []corev1.Volume{{
			Name: pvcVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: d.PVC.ClaimName},
			},
		}}
		mounts = []corev1.VolumeMount{{Name: pvcVolume, MountPath: "/" + pvcMountPath}}
		return "/" + pvcMountPath, volumes, mounts, nil
	}
	if d.URL == "" {
		return "", nil, nil, fmt.Errorf("object-store destination requires url")
	}
	return d.URL, nil, nil, nil
}

// backupArgs composes the neo4j-admin argument vector from operator-owned flags, typed
// options, and the allow-listed extraArgs (BDR-014 §12 / ADR-015).
func backupArgs(ctx render.Context, backup *neo4jv1beta1.Neo4jBackup, toPath string) []string {
	args := []string{
		"database", "backup",
		"--from=" + FromAddress(ctx),
		"--to-path=" + toPath,
		"--temp-path=" + scratchMountPath,
		"--type=" + AdminType(backup.Spec.Type),
	}
	if o := backup.Spec.Options; o != nil {
		if o.Compress != nil {
			args = append(args, fmt.Sprintf("--compress=%t", *o.Compress))
		}
		if o.KeepFailed != nil {
			args = append(args, fmt.Sprintf("--keep-failed=%t", *o.KeepFailed))
		}
		if o.Verbose != nil && *o.Verbose {
			args = append(args, "--verbose")
		}
		if o.IncludeMetadata != "" {
			args = append(args, "--include-metadata="+string(o.IncludeMetadata))
		}
		args = append(args, o.ExtraArgs...)
	}
	// Databases are the trailing operands. neo4j-admin accepts a comma-separated list / pattern
	// (including "*"); it names artifacts <db>-<timestamp>.backup so chains stay per-database.
	args = append(args, strings.Join(databases(backup.Spec.Databases), ","))
	return args
}

func databases(dbs []string) []string {
	if len(dbs) == 0 {
		return []string{"*"}
	}
	return dbs
}
