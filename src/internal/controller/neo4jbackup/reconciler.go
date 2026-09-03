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

// Package neo4jbackup reconciles Neo4jBackup records into a run-to-completion Job (ADR-015).
package neo4jbackup

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/controller/neo4jbackupschedule"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	renderbackup "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/backup"
)

// BackupReconciler drives a Neo4jBackup to Succeeded/Failed via one owned Job.
type BackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func NewReconciler(mgr ctrl.Manager) *BackupReconciler {
	return &BackupReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("neo4jbackup-controller"),
	}
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithName("neo4jbackup")

	var backup neo4jv1beta1.Neo4jBackup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Immutable record: a terminal run never spawns a second Job (GitOps re-apply safe).
	if backup.Status.Phase == neo4jv1beta1.RunPhaseSucceeded || backup.Status.Phase == neo4jv1beta1.RunPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Resolve the target workload (same namespace).
	var neo4j neo4jv1beta1.Neo4j
	if err := r.Get(ctx, types.NamespacedName{Name: backup.Spec.Neo4jRef.Name, Namespace: backup.Namespace}, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			// Target may still be provisioning — wait rather than failing the record terminally.
			return r.retryable(ctx, &backup, oracle.ReasonBackupTargetNotFound,
				"waiting for Neo4j "+backup.Spec.Neo4jRef.Name+" to exist")
		}
		return ctrl.Result{}, err
	}

	if neo4j.Spec.Edition != neo4jv1beta1.EditionEnterprise {
		return r.fail(ctx, &backup, oracle.ReasonBackupEditionUnsupported,
			"backup requires Enterprise edition; target is "+string(neo4j.Spec.Edition))
	}
	if !render.ClientServiceContext(&neo4j).BackupListenerEnabled() {
		return r.retryable(ctx, &backup, oracle.ReasonBackupListenerDisabled,
			"target has no backup listener (set features.backup and connectivity.listeners.backup)")
	}

	job, err := renderbackup.BackupJob(&neo4j, &backup, chainSubDir(&backup))
	if err != nil {
		return r.fail(ctx, &backup, oracle.ReasonBackupDestinationUnsupported, err.Error())
	}
	if err := shared.Apply(ctx, r.Client, r.Scheme, &backup, job, func() error { return nil }); err != nil {
		return ctrl.Result{}, err
	}

	// Read the owned Job's terminal state and mirror it.
	var owned batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: renderbackup.JobName(&backup), Namespace: backup.Namespace}, &owned); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setRunning(ctx, &backup)
		}
		return ctrl.Result{}, err
	}
	switch complete, failed, msg := shared.JobTerminal(&owned); {
	case complete:
		return r.succeed(ctx, &backup, &owned)
	case failed:
		// Prefer the real neo4j-admin cause (tailed into the pod's termination message) over the
		// Job controller's generic "backoff limit exceeded", so status.message is actionable.
		if podMsg := shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name); podMsg != "" {
			msg = podMsg
		}
		log.Info("backup job failed", "job", owned.Name, "detail", msg)
		return r.fail(ctx, &backup, oracle.ReasonBackupJobFailed, msg)
	default:
		return r.setRunning(ctx, &backup)
	}
}

// chainSubDir isolates a schedule-managed chain's artifacts under <destination>/<chainId> so an
// aggregation of one chain can never make a later differential of another chain mis-parent onto it.
// It applies only to PVC-backed, seedable (named-database) backups that carry a schedule's chain
// label; ad-hoc backups (no label) and non-seedable/object-store backups stay flat ("").
func chainSubDir(b *neo4jv1beta1.Neo4jBackup) string {
	if _, ok := renderbackup.SeedableDatabases(b); !ok {
		return ""
	}
	return b.Labels[neo4jbackupschedule.LabelChain]
}

func (r *BackupReconciler) setRunning(ctx context.Context, b *neo4jv1beta1.Neo4jBackup) (ctrl.Result, error) {
	b.Status.Phase = neo4jv1beta1.RunPhaseRunning
	setCondition(b, oracle.ConditionBackupReady, metav1.ConditionFalse, oracle.ReasonBackupInProgress, "backup Job running")
	if err := r.writeStatus(ctx, b); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *BackupReconciler) succeed(ctx context.Context, b *neo4jv1beta1.Neo4jBackup, job *batchv1.Job) (ctrl.Result, error) {
	b.Status.Phase = neo4jv1beta1.RunPhaseSucceeded
	b.Status.Reason = ""
	b.Status.Message = ""
	b.Status.Artifacts = artifactsFor(b, r.artifactPaths(ctx, job))
	// A scheduled backup carries its chain id as a label (the schedule owns cross-backup chains);
	// an ad-hoc backup anchors its own chain, so it falls back to the record name.
	if b.Status.Chain == "" {
		if chain := b.Labels[neo4jbackupschedule.LabelChain]; chain != "" {
			b.Status.Chain = chain
		} else {
			b.Status.Chain = b.Name
		}
	}
	setCondition(b, oracle.ConditionBackupReady, metav1.ConditionTrue, oracle.ReasonBackupSucceeded, "backup completed")
	return ctrl.Result{}, r.writeStatus(ctx, b)
}

// artifactsFor records one artifact per requested database, pointing at the destination
// (BDR-014 §13 — restore-by-backupRef resolves this). The requested type is recorded as-is.
// For PVC destinations it records Path, the real artifact filename the Job reported (arts[db].Name),
// so restore can seed file:/backups/<path> — the chain's last link — without parsing filenames,
// plus SizeBytes when the Job could stat it.
func artifactsFor(b *neo4jv1beta1.Neo4jBackup, arts map[string]shared.NamedArtifact) []neo4jv1beta1.BackupArtifact {
	uri := renderbackup.DestinationURI(b.Spec.Destination)
	now := metav1.Now()
	dbs := b.Spec.Databases
	if len(dbs) == 0 {
		dbs = []string{"*"}
	}
	out := make([]neo4jv1beta1.BackupArtifact, 0, len(dbs))
	for _, db := range dbs {
		a := neo4jv1beta1.BackupArtifact{
			Database:    db,
			Type:        b.Spec.Type,
			URI:         uri,
			CompletedAt: &now,
		}
		if art, ok := arts[db]; ok {
			a.Path = art.Name
			a.SizeBytes = art.SizeBytes
		}
		out = append(out, a)
	}
	return out
}

// artifactPaths reads the real artifact filename (and size) the backup Job recorded per database.
// The Job echoes "<db>=<file>|<bytes>" to /dev/termination-log on success, which the kubelet
// surfaces in the pod's terminated message (terminationMessagePolicy=FallbackToLogsOnError).
// Restore seeds file:/backups/<file> from these — the chain's last link, not a renamed pointer.
// Best-effort: an empty/unreadable message yields no paths, and restore then reports the gap.
func (r *BackupReconciler) artifactPaths(ctx context.Context, job *batchv1.Job) map[string]shared.NamedArtifact {
	return shared.ParseNamedArtifacts(shared.JobPodTerminationMessage(ctx, r.Client, job.Namespace, job.Name))
}

// fail records a terminal failure with a catalogued reason and a Warning Event.
func (r *BackupReconciler) fail(ctx context.Context, b *neo4jv1beta1.Neo4jBackup, reason oracle.Reason, msg string) (ctrl.Result, error) {
	b.Status.Phase = neo4jv1beta1.RunPhaseFailed
	b.Status.Reason = reason.String()
	b.Status.Message = msg
	setCondition(b, oracle.ConditionBackupReady, metav1.ConditionFalse, reason, msg)
	if r.Recorder != nil {
		r.Recorder.Event(b, corev1.EventTypeWarning, reason.String(), msg)
	}
	return ctrl.Result{}, r.writeStatus(ctx, b)
}

// retryable records a non-terminal wait (target/listener not ready yet) and requeues.
func (r *BackupReconciler) retryable(ctx context.Context, b *neo4jv1beta1.Neo4jBackup, reason oracle.Reason, msg string) (ctrl.Result, error) {
	if b.Status.Phase == "" {
		b.Status.Phase = neo4jv1beta1.RunPhasePending
	}
	b.Status.Reason = reason.String()
	b.Status.Message = msg
	setCondition(b, oracle.ConditionBackupReady, metav1.ConditionFalse, reason, msg)
	if err := r.writeStatus(ctx, b); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *BackupReconciler) writeStatus(ctx context.Context, b *neo4jv1beta1.Neo4jBackup) error {
	b.Status.ObservedGeneration = b.Generation
	if err := r.Status().Update(ctx, b); err != nil {
		if apierrors.IsConflict(err) {
			return nil // next reconcile recomputes from fresh state
		}
		return err
	}
	return nil
}

// setCondition takes catalogued values only — see internal/oracle and status.setCondition.
func setCondition(b *neo4jv1beta1.Neo4jBackup, ctype oracle.Condition, status metav1.ConditionStatus, reason oracle.Reason, message string) {
	meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
		Type:               ctype.String(),
		Status:             status,
		Reason:             reason.String(),
		Message:            message,
		ObservedGeneration: b.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jBackup{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
