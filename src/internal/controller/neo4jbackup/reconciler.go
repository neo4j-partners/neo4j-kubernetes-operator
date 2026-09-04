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
	"strings"
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

	// Aggregate is a file-only operation on the backup PVC (no live server, no backup listener):
	// it collapses source's chain into a recovered full. Diverge here before the listener gate.
	if backup.Spec.Type == neo4jv1beta1.BackupTypeAggregate {
		return r.reconcileAggregate(ctx, &backup, &neo4j)
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

// reconcileAggregate drives a type: Aggregate backup: it collapses spec.source's chain into a
// single recovered full via an owned Job and catalogs that recovered artifact as this record's
// output (a Full the restore path can seed directly, no chain replay). It reuses the same owned
// Job name/lifecycle as a normal backup, only the inputs (from the source chain) and the recorded
// artifact (Type Full, path = the recovered file) differ.
func (r *BackupReconciler) reconcileAggregate(ctx context.Context, b *neo4jv1beta1.Neo4jBackup, neo4j *neo4jv1beta1.Neo4j) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithName("neo4jbackup")

	if b.Spec.Source == nil || b.Spec.Source.BackupRef == "" {
		return r.fail(ctx, b, oracle.ReasonBackupSourceUnsupported, "type Aggregate requires spec.source.backupRef (the chain to aggregate)")
	}
	for _, db := range b.Spec.Databases {
		if db == "*" {
			return r.fail(ctx, b, oracle.ReasonBackupSourceUnsupported, "type Aggregate does not support wildcard '*'; list databases explicitly")
		}
	}

	// The source chain must exist and have Succeeded before we can aggregate it — wait otherwise.
	var src neo4jv1beta1.Neo4jBackup
	if err := r.Get(ctx, types.NamespacedName{Name: b.Spec.Source.BackupRef, Namespace: b.Namespace}, &src); err != nil {
		if apierrors.IsNotFound(err) {
			return r.retryable(ctx, b, oracle.ReasonBackupSourceNotFound, "source.backupRef "+b.Spec.Source.BackupRef+" not found")
		}
		return ctrl.Result{}, err
	}
	if src.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		return r.retryable(ctx, b, oracle.ReasonBackupSourceNotFound,
			"source.backupRef "+b.Spec.Source.BackupRef+" has not Succeeded (phase "+string(src.Status.Phase)+")")
	}

	claim, dbArtifacts, reason, msg := aggregateInputs(b, &src)
	if reason != nil {
		return r.fail(ctx, b, *reason, msg)
	}

	job, err := renderbackup.AggregateJob(neo4j, renderbackup.JobName(b), claim, dbArtifacts)
	if err != nil {
		return r.fail(ctx, b, oracle.ReasonBackupSourceUnsupported, err.Error())
	}
	if err := shared.Apply(ctx, r.Client, r.Scheme, b, job, func() error { return nil }); err != nil {
		return ctrl.Result{}, err
	}

	var owned batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: renderbackup.JobName(b), Namespace: b.Namespace}, &owned); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setRunning(ctx, b)
		}
		return ctrl.Result{}, err
	}
	switch complete, failed, jmsg := shared.JobTerminal(&owned); {
	case complete:
		return r.succeedAggregate(ctx, b, &src, claim, &owned)
	case failed:
		if podMsg := shared.JobPodTerminationMessage(ctx, r.Client, owned.Namespace, owned.Name); podMsg != "" {
			jmsg = podMsg
		}
		log.Info("aggregate job failed", "job", owned.Name, "detail", jmsg)
		return r.fail(ctx, b, oracle.ReasonBackupJobFailed, jmsg)
	default:
		return r.setRunning(ctx, b)
	}
}

// aggregateInputs validates the source is PVC-backed with recorded artifacts and returns the claim
// plus each database's latest artifact path (the chain's last link, chain-sub-dir included) for the
// aggregate Job. All databases must live on the one claim. Unlike restore's aggregate, it does not
// require the target to mount the claim — the Job mounts it directly.
func aggregateInputs(b *neo4jv1beta1.Neo4jBackup, src *neo4jv1beta1.Neo4jBackup) (claim string, dbArtifacts map[string]string, reason *oracle.Reason, msg string) {
	unsupported := oracle.ReasonBackupSourceUnsupported
	notFound := oracle.ReasonBackupSourceNotFound
	dbArtifacts = map[string]string{}
	for _, db := range b.Spec.Databases {
		a, ok := artifactFor(src, db)
		if !ok {
			return "", nil, &notFound, "source.backupRef " + src.Name + " has no artifact for database " + db
		}
		if !strings.HasPrefix(a.URI, "pvc://") {
			return "", nil, &unsupported, "aggregate supports only PVC-backed backups; artifact for " + db + " is " + a.URI
		}
		if a.Path == "" {
			return "", nil, &unsupported, "source recorded no artifact filename for " + db + "; cannot aggregate"
		}
		c := strings.TrimPrefix(a.URI, "pvc://")
		if claim == "" {
			claim = c
		} else if claim != c {
			return "", nil, &unsupported, "aggregate requires all databases on the same backup claim"
		}
		dbArtifacts[db] = a.Path
	}
	return claim, dbArtifacts, nil, ""
}

// artifactFor finds the recorded artifact for a database (exact match, or a "*" artifact standing
// for all databases).
func artifactFor(src *neo4jv1beta1.Neo4jBackup, db string) (*neo4jv1beta1.BackupArtifact, bool) {
	for i := range src.Status.Artifacts {
		if a := &src.Status.Artifacts[i]; a.Database == db || a.Database == "*" {
			return a, true
		}
	}
	return nil, false
}

// succeedAggregate catalogs the recovered full(s) the aggregate Job produced (chain-prefixed path,
// Type Full, pvc://<source claim> URI) so a restore can seed them directly. It belongs to the same
// chain as its source. An empty recorded path means the aggregate produced nothing usable → fail.
func (r *BackupReconciler) succeedAggregate(ctx context.Context, b *neo4jv1beta1.Neo4jBackup, src *neo4jv1beta1.Neo4jBackup, claim string, job *batchv1.Job) (ctrl.Result, error) {
	arts := r.artifactPaths(ctx, job)
	now := metav1.Now()
	out := make([]neo4jv1beta1.BackupArtifact, 0, len(b.Spec.Databases))
	for _, db := range b.Spec.Databases {
		art, ok := arts[db]
		if !ok || art.Name == "" {
			return r.fail(ctx, b, oracle.ReasonBackupJobFailed, "aggregate produced no recovered artifact for database "+db)
		}
		out = append(out, neo4jv1beta1.BackupArtifact{
			Database:    db,
			Type:        neo4jv1beta1.BackupTypeFull, // the recovered artifact is a standalone full
			URI:         "pvc://" + claim,
			Path:        art.Name,
			SizeBytes:   art.SizeBytes,
			CompletedAt: &now,
		})
	}
	b.Status.Phase = neo4jv1beta1.RunPhaseSucceeded
	b.Status.Reason = ""
	b.Status.Message = ""
	b.Status.Artifacts = out
	if b.Status.Chain == "" {
		if chain := b.Labels[neo4jbackupschedule.LabelChain]; chain != "" {
			b.Status.Chain = chain
		} else {
			b.Status.Chain = src.Status.Chain
		}
	}
	setCondition(b, oracle.ConditionBackupReady, metav1.ConditionTrue, oracle.ReasonBackupSucceeded, "aggregate completed")
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
