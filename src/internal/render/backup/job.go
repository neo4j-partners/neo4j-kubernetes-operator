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
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

const (
	// scratchMountPath is the local staging dir neo4j-admin uses before streaming to the
	// destination (--temp-path). Under-provisioning here is a common backup failure (ADR-015).
	scratchMountPath = "/tmp/neo4j-backup"
	scratchVolume    = "scratch"
	// pvcMountPath is where a PVC destination is mounted inside the Job pod.
	pvcMountPath = "destination"
	pvcVolume    = "destination"
	// pvcSubPath is the subdirectory within the claim the Job writes into. It is the workload's
	// backups sub-path (single-sourced in render/storage): the server mounts the same claim at
	// /backups via that subPath, so the Job must write there too or restore seeds
	// file:/backups/<ptr> against an empty dir and the server reports "not found" (ADR-015).
	pvcSubPath = storage.BackupsSubPath
	// backupTTLSeconds keeps a finished Job (and its logs) around for a day so the cause of a
	// failure survives GC (ADR-015 — logs & observability).
	// ponytail: fixed 24h; upgrade path is a schedule/operator flag when someone needs it.
	backupTTLSeconds int32 = 86400
	backupBackoff    int32 = 3
	containerName          = "neo4j-admin"
)

// JobName is the deterministic Job name for a Neo4jBackup (owner-referenced by it).
func JobName(backup *neo4jv1beta1.Neo4jBackup) string { return backup.Name + "-backup" }

// SeedableDatabases reports the databases a PVC-destination backup exposes as file: seeds, and
// true when that applies at all. Only explicitly named databases on a PVC qualify: a wildcard
// ("*") has no known names at render time (so the Job cannot record which artifact to seed), and
// object-store destinations seed from their URL directly. Incrementals qualify too — restore
// seeds the artifact this backup produced (the chain's last link) and Neo4j replays the whole
// chain from the same directory (ADR-015).
func SeedableDatabases(backup *neo4jv1beta1.Neo4jBackup) ([]string, bool) {
	if backup.Spec.Destination.Type != neo4jv1beta1.BackupDestinationPVC {
		return nil, false
	}
	dbs := backup.Spec.Databases
	if len(dbs) == 0 {
		return nil, false
	}
	for _, db := range dbs {
		if db == "*" || db == "" {
			return nil, false
		}
	}
	return dbs, true
}

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

	args := backupArgs(ctx, backup, toPath)
	container := corev1.Container{
		Name:    containerName,
		Image:   ctx.ImageRef(),
		Command: []string{"neo4j-admin"},
		Args:    args,
		// FallbackToLogsOnError copies the tail of the container's own output into the pod's
		// termination message when it exits non-zero, so the reconciler can surface the real
		// neo4j-admin cause (e.g. "Differential backups require ... a full backup") in
		// status.message instead of the Job controller's generic "backoff limit exceeded".
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             mounts,
		SecurityContext:          workload.ContainerSecurityContext(ctx),
	}
	// PVC destinations record the real artifact filename per database so restore can seed
	// file:/backups/<name> (the chain's last link) without a renamed pointer (ADR-015 round-trip).
	// This requires a shell, so wrap neo4j-admin in sh -c; object stores keep the bare command.
	if dbs, ok := SeedableDatabases(backup); ok {
		container.Command = []string{"sh", "-c", backupScript(args, toPath, dbs)}
		container.Args = nil
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
					RestartPolicy:   corev1.RestartPolicyNever,
					Containers:      []corev1.Container{container},
					Volumes:         volumes,
					SecurityContext: workload.PodSecurityContext(ctx),
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
		mounts = []corev1.VolumeMount{{Name: pvcVolume, MountPath: "/" + pvcMountPath, SubPath: pvcSubPath}}
		return "/" + pvcMountPath, volumes, mounts, nil
	}
	if d.URL == "" {
		return "", nil, nil, fmt.Errorf("object-store destination requires url")
	}
	return d.URL, nil, nil, nil
}

// backupScript runs neo4j-admin then records the real artifact filename neo4j-admin chose for
// each database (its newest <db>-<timestamp>.backup) so restore can seed file:/backups/<name> —
// the chain's true last link — without a renamed pointer. A renamed hardlink/copy would leave a
// duplicate .backup in the directory that confuses Neo4j's chain resolution (and copies are the
// only portable pointer on Azure Files SMB, which supports neither hardlinks nor symlinks), so
// we record the name instead of duplicating the file.
//
// The name is emitted as "<db>=<file>" to /dev/termination-log, which the kubelet surfaces in the
// pod's terminated message (terminationMessagePolicy=FallbackToLogsOnError) for the reconciler to
// read on success. Recording is best-effort: the group always exits 0 (trailing `true`) so a
// missing artifact or an unwritable termination-log never fails an otherwise-good backup —
// restore then reports the missing path. args are space-joined (neo4j-admin flags carry no
// spaces — switch to a quoted argv if extraArgs ever needs them).
func backupScript(args []string, toPath string, dbs []string) string {
	script := "neo4j-admin " + strings.Join(args, " ")
	for _, db := range dbs {
		script += fmt.Sprintf(
			" && { a=\"$(ls -t %s/%s-*.backup 2>/dev/null | head -1)\"; [ -n \"$a\" ] && echo \"%s=$(basename \"$a\")\" >> /dev/termination-log 2>/dev/null; true; }",
			toPath, db, db)
	}
	return script
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
