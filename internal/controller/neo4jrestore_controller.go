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
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Status constants
const (
	StatusCompleted = "Completed"
	StatusFailed    = "Failed"
	StatusRunning   = "Running"
	StatusPending   = "Pending"
)

// Restore source type constants
const (
	SourceTypeBackup = "backup"
	SourceTypeS3     = "s3"
	SourceTypeGCS    = "gcs"
)

// errBackupNotReady is the package-internal alias for ErrBackupNotReady.
// Kept for backward compatibility with the restore-controller's internal
// usage; new code should use ErrBackupNotReady (defined in
// backup_resolver.go) directly.
var errBackupNotReady = ErrBackupNotReady

// restoreServiceAccountName is the ServiceAccount used by all restore Job
// pods. Mirrors neo4j-backup-sa on the backup side; intentionally separate
// so cluster operators can scope IAM permissions narrowly (read-only for
// restore, write for backup). Operators can attach workload-identity
// annotations (IRSA / GKE Workload Identity / Azure Workload Identity)
// via the resolved CloudBlock.Identity.AutoCreate.Annotations on the
// restore source — for source.type=backup that comes from the referenced
// Neo4jBackup's cloud config.
const restoreServiceAccountName = "neo4j-restore-sa"

// RestoreInProgressAnnotation is set on the target Neo4jEnterpriseCluster /
// Neo4jEnterpriseStandalone CR while a Neo4jRestore is actively coordinating
// a scale-down → restore → scale-up cycle. Its value is the name of the
// owning Neo4jRestore CR.
//
// The Neo4jEnterpriseClusterReconciler reads this annotation and, when set,
// stops forcing sts.Spec.Replicas back to spec.topology.servers — so the
// restore controller's scale-to-0 actually sticks. Without this coordination
// the two controllers race on every reconcile (issue #117) and the cluster
// never goes offline, leaving neo4j-admin restore unable to acquire the
// database file lock.
//
// The annotation is set immediately before stopCluster() and cleared in
// every restore exit path: startCluster() on success, the finalizer on CR
// delete, and the failure path when stopCluster itself fails.
const RestoreInProgressAnnotation = "neo4j.com/restore-in-progress"

// Neo4jRestoreReconciler reconciles a Neo4jRestore object
type Neo4jRestoreReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
}

// Pod hardening for restore Jobs delegates to the single source of truth
// in internal/resources/security_context.go.
func hardenedRestorePodSecurityContext() *corev1.PodSecurityContext {
	return resources.DefaultNeo4jPodSecurityContext()
}

func hardenedRestoreContainerSecurityContext() *corev1.SecurityContext {
	return resources.DefaultNeo4jContainerSecurityContext()
}

const (
	// RestoreFinalizer is the finalizer for Neo4j restore resources
	RestoreFinalizer = "neo4j.com/restore-finalizer"

	// AnnotationCypherRestoreIssued marks that the cluster-native Cypher
	// restore (the asynchronous `dbms.recreateDatabase` path) has already been
	// issued against the live cluster. Its value is the RFC3339 timestamp of
	// the issue. The annotation serves two purposes:
	//   1. Guard: re-entering startClusterCypherRestore must NOT re-issue the
	//      recreate (that would wipe the partially-seeded database and restart
	//      the seed from scratch).
	//   2. Deadline: pollClusterRestoreOnline derives the online-wait deadline
	//      from this timestamp so the worker is never held blocking — it polls
	//      one SHOW DATABASE per reconcile and requeues.
	AnnotationCypherRestoreIssued = "neo4j.com/cypher-restore-issued"

	// cypherRestoreOnlineTimeout bounds how long pollClusterRestoreOnline will
	// wait (across requeues) for an asynchronously-recreated database to
	// converge to online before marking the restore Failed.
	cypherRestoreOnlineTimeout = 5 * time.Minute
)

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jrestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jrestores/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jbackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch

// Reconcile handles the reconciliation of Neo4jRestore resources
func (r *Neo4jRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jRestore instance
	restore := &neo4jv1beta1.Neo4jRestore{}
	if err := r.Get(ctx, req.NamespacedName, restore); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jRestore resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jRestore")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if restore.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, restore)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(restore, RestoreFinalizer) {
		controllerutil.AddFinalizer(restore, RestoreFinalizer)
		if err := r.Update(ctx, restore); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get target cluster
	targetCluster, err := r.getClusterRef(ctx, restore)
	if err != nil {
		logger.Error(err, "Failed to get target cluster")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to get target cluster: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Validate Neo4j version compatibility (5.26+ or 2025.01+)
	if err := r.validateNeo4jVersion(targetCluster); err != nil {
		logger.Error(err, "Neo4j version validation failed")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Neo4j version not supported: %v", err))
		return ctrl.Result{}, err
	}

	// Check if restore is already completed
	if restore.Status.Phase == StatusCompleted && restore.Status.ObservedGeneration == restore.Generation {
		logger.Info("Restore already completed")
		return ctrl.Result{}, nil
	}

	// Check if restore is running
	if restore.Status.Phase == StatusRunning {
		return r.checkRestoreProgress(ctx, restore, targetCluster)
	}

	// A previously failed restore for the same spec generation must not silently retry.
	// The user must bump spec (new generation) or delete/recreate the CR to trigger a retry.
	if restore.Status.Phase == StatusFailed && restore.Status.ObservedGeneration == restore.Generation {
		logger.Info("Restore previously failed; not retrying until spec changes or resource is recreated",
			"message", restore.Status.Message)
		return ctrl.Result{}, nil
	}

	// Start restore process
	return r.startRestore(ctx, restore, targetCluster)
}

func (r *Neo4jRestoreReconciler) handleDeletion(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(restore, RestoreFinalizer) {
		return ctrl.Result{}, nil
	}

	// Clean up restore jobs
	if err := r.cleanupRestoreJobs(ctx, restore); err != nil {
		logger.Error(err, "Failed to cleanup restore jobs")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Release the cluster controller's hold on STS replicas if this restore
	// died mid-cycle (e.g. user deleted the CR while stopCluster was in
	// progress). Without this the target cluster is permanently un-scalable.
	// Idempotent — only clears the annotation if THIS restore set it. Issue #117.
	if restore.Spec.ClusterRef != "" {
		if err := r.clearRestoreInProgressAnnotation(ctx, restore, restore.Spec.ClusterRef, restore.Namespace); err != nil {
			logger.Error(err, "Failed to clear restore-in-progress annotation during finalizer cleanup")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(restore, RestoreFinalizer)
	return ctrl.Result{}, r.Update(ctx, restore)
}

func (r *Neo4jRestoreReconciler) startRestore(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Set start time
	now := metav1.Now()
	restore.Status.StartTime = &now
	restore.Status.ObservedGeneration = restore.Generation

	// Validate restore request
	if err := r.validateRestore(ctx, restore); err != nil {
		logger.Error(err, "Restore validation failed")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Validation failed: %v", err))
		return ctrl.Result{}, err
	}

	// Cluster targets bypass the Job + `neo4j-admin restore` path entirely
	// (the docs flag it as unsafe on clusters — `--overwrite-destination`
	// "is not safe on a cluster since clusters have additional state that
	// would be inconsistent with the restored database"). Use the Cypher
	// path documented at:
	//   https://neo4j.com/docs/operations-manual/current/clustering/databases/#restore-database-using-uri-approach
	//   https://neo4j.com/docs/operations-manual/current/clustering/databases/#restore-database-using-recreate-procedure
	// Standalone targets keep the existing Job-based flow.
	isTrueCluster, _, lookupErr := r.isRestoreTargetTrueCluster(ctx, restore)
	if lookupErr != nil {
		logger.Error(lookupErr, "Failed to determine target type")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Target lookup failed: %v", lookupErr))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, lookupErr
	}
	if isTrueCluster {
		return r.startClusterCypherRestore(ctx, restore, cluster)
	}

	// Check if database exists and handle accordingly
	if !restore.Spec.Force {
		if err := r.checkDatabaseExists(ctx, restore, cluster); err != nil {
			logger.Error(err, "Database existence check failed")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Database check failed: %v", err))
			return ctrl.Result{}, err
		}
	}

	// Stop cluster if required
	if restore.Spec.StopCluster {
		// Mark the cluster as having a restore in progress BEFORE scaling
		// the STS to 0 — otherwise the cluster controller scales it right
		// back up on its next reconcile (issue #117).
		if err := r.setRestoreInProgressAnnotation(ctx, restore, cluster); err != nil {
			logger.Error(err, "Failed to set restore-in-progress annotation")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to coordinate cluster scale-down: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
		if err := r.stopCluster(ctx, cluster); err != nil {
			logger.Error(err, "Failed to stop cluster")
			// Clear the annotation so the cluster controller resumes
			// reconciling replicas — otherwise a failed stopCluster
			// leaves the cluster permanently un-scalable.
			if cleanupErr := r.clearRestoreInProgressAnnotation(ctx, restore, cluster.Name, cluster.Namespace); cleanupErr != nil {
				logger.Error(cleanupErr, "Failed to clear restore-in-progress annotation after stopCluster failure")
			}
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to stop cluster: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	} else {
		// stopCluster=false means "the cluster is already quiesced — don't
		// touch the STS." Refuse if any server pods are still running:
		// silently writing the restore into a fresh PVC mount (or, worse
		// pre-fix, into an EmptyDir) is invisible data loss (issue #117).
		if err := r.refuseRestoreIfPodsRunning(ctx, restore, cluster); err != nil {
			logger.Error(err, "Refusing restore against running cluster")
			r.updateRestoreStatus(ctx, restore, StatusFailed, err.Error())
			return ctrl.Result{}, err
		}
	}

	// Run pre-restore hooks
	if restore.Spec.Options != nil && restore.Spec.Options.PreRestore != nil {
		if err := r.runRestoreHooks(ctx, restore, cluster, restore.Spec.Options.PreRestore, "pre-restore"); err != nil {
			logger.Error(err, "Pre-restore hooks failed")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Pre-restore hooks failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Create restore job
	job, err := r.createRestoreJob(ctx, restore, cluster)
	if err != nil {
		// "Backup has no Succeeded run yet" is a TRANSIENT condition:
		// the user may have created the restore before the backup
		// completed. Route to Pending (which the Reconcile guard
		// requeues) instead of Failed (which the guard pins as
		// terminal until the CR is recreated). The restore will
		// auto-promote to Running once the backup's history gains a
		// Succeeded entry on a future reconcile.
		if stderrors.Is(err, errBackupNotReady) {
			logger.Info("Restore is waiting for the referenced backup to complete", "error", err.Error())
			r.updateRestoreStatus(ctx, restore, StatusPending, fmt.Sprintf("Waiting for backup to complete: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		logger.Error(err, "Failed to create restore job")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to create restore job: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status
	r.updateRestoreStatus(ctx, restore, StatusRunning, fmt.Sprintf("Restore job %s created", job.Name))
	r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreStarted, fmt.Sprintf("Restore job %s started", job.Name))

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jRestoreReconciler) checkRestoreProgress(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Cluster-native Cypher restores have no Job — the asynchronous
	// `dbms.recreateDatabase` was issued in a prior reconcile (marked by the
	// cypher-restore-issued annotation). Poll the live database's online state
	// instead of looking for a Job.
	if _, issued := restore.Annotations[AnnotationCypherRestoreIssued]; issued {
		return r.pollClusterRestoreOnline(ctx, restore, cluster)
	}

	// Get restore job
	jobName := restore.Name + "-restore"
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: restore.Namespace}, job)

	if err != nil {
		if errors.IsNotFound(err) {
			// The Job's TTLSecondsAfterFinished GC'd it before we observed a
			// terminal state (e.g. the operator was down longer than the TTL).
			// We can't tell success from failure, so fail safe: restore cluster
			// availability and mark Failed rather than requeueing forever with
			// the cluster scaled to 0.
			logger.Info("Restore Job not found (likely TTL-collected before completion was observed); failing the restore", "job", jobName)
			r.restoreClusterAfterFailure(ctx, restore, cluster)
			r.updateRestoreStatus(ctx, restore, StatusFailed,
				"restore Job disappeared before completion was observed (TTL-collected); re-create the Neo4jRestore to retry")
			r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreFailed,
				"restore Job disappeared before completion was observed")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get restore job")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Decide on terminal Job CONDITIONS, not raw pod counts: with BackoffLimit>0
	// `Status.Failed` counts failed pod attempts and can be >0 while Kubernetes
	// is still retrying — which would flip the restore to a (pinned) terminal
	// Failed before a retry that might succeed.
	switch {
	case jobConditionTrue(job, batchv1.JobComplete) || job.Status.Succeeded > 0:
		return r.handleRestoreSuccess(ctx, restore, cluster, job)
	case jobConditionTrue(job, batchv1.JobFailed):
		// Terminal failure (BackoffLimit exhausted). Restore cluster
		// availability BEFORE marking Failed — a StopCluster restore otherwise
		// leaves the cluster scaled to 0 indefinitely (the cluster controller
		// honours the restore-in-progress annotation until it is cleared).
		r.restoreClusterAfterFailure(ctx, restore, cluster)
		r.updateRestoreStatus(ctx, restore, StatusFailed, "Restore job failed")
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreFailed, "Restore job failed")
		return ctrl.Result{}, nil
	default:
		// Job still running, or retrying within BackoffLimit.
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}
}

// jobConditionTrue reports whether the Job has the given condition set to True.
func jobConditionTrue(job *batchv1.Job, condType batchv1.JobConditionType) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == condType && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// restoreClusterAfterFailure scales the cluster back up and releases the
// restore-in-progress hold after a terminal restore failure — mirroring the
// teardown handleRestoreSuccess performs on success. Without it, a
// StopCluster=true restore that fails leaves the cluster at 0 replicas until a
// human deletes the CR. Both calls are idempotent and best-effort (logged, not
// fatal) so the restore still reaches a terminal Failed state.
func (r *Neo4jRestoreReconciler) restoreClusterAfterFailure(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) {
	if !restore.Spec.StopCluster || cluster == nil {
		return
	}
	logger := log.FromContext(ctx)
	if err := r.startCluster(ctx, cluster); err != nil {
		logger.Error(err, "Failed to scale cluster back up after restore failure")
	}
	if err := r.clearRestoreInProgressAnnotation(ctx, restore, cluster.Name, cluster.Namespace); err != nil {
		logger.Error(err, "Failed to clear restore-in-progress annotation after restore failure")
	}
}

func (r *Neo4jRestoreReconciler) handleRestoreSuccess(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, job *batchv1.Job) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Set completion time
	now := metav1.Now()
	restore.Status.CompletionTime = &now

	// Update statistics
	r.updateRestoreStats(ctx, restore, job)

	// Start cluster if it was stopped
	if restore.Spec.StopCluster {
		if err := r.startCluster(ctx, cluster); err != nil {
			logger.Error(err, "Failed to start cluster after restore")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to start cluster after restore: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}

		// Release the cluster controller's hold on Replicas now that we've
		// scaled the STS back to the original count. Subsequent cluster
		// reconciles re-assert sts.Spec.Replicas = spec.topology.servers,
		// which equals the value startCluster just wrote — so this is
		// safe with no flap. Issue #117.
		if err := r.clearRestoreInProgressAnnotation(ctx, restore, cluster.Name, cluster.Namespace); err != nil {
			logger.Error(err, "Failed to clear restore-in-progress annotation")
			// Non-fatal — the finalizer path will clean it up if needed.
		}

		// Wait for cluster to be ready
		if err := r.waitForClusterReady(ctx, cluster); err != nil {
			logger.Error(err, "Cluster not ready after restore")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Cluster not ready after restore: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Run post-restore hooks
	if restore.Spec.Options != nil && restore.Spec.Options.PostRestore != nil {
		if err := r.runRestoreHooks(ctx, restore, cluster, restore.Spec.Options.PostRestore, "post-restore"); err != nil {
			logger.Error(err, "Post-restore hooks failed")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Post-restore hooks failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Register the restored database with Neo4j so it becomes accessible
	if err := r.createOrStartDatabase(ctx, restore, cluster); err != nil {
		logger.Error(err, "Failed to create/start database after restore")
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonDatabaseCreateFailed,
			fmt.Sprintf("Restore succeeded but failed to create database %q: %v", restore.Spec.DatabaseName, err))
	}

	// For multi-server clusters, force all servers to re-seed the
	// restored database from server-0's PVC (which is the only one the
	// restore Job wrote to). Without this step, post-restart cluster
	// bootstrap picks the primary non-deterministically; if the stale-
	// data server wins consensus the restored data is overwritten when
	// other servers re-sync from it. Skipped silently for standalone /
	// single-server topologies (nothing to re-seed) and for Neo4j
	// versions that don't expose the recreate procedure (pre-5.24
	// SemVer or pre-2025.02 CalVer). Non-fatal — if recreate fails the
	// restore still completes; the operator events surface the issue.
	if err := r.recreateRestoredDatabaseOnCluster(ctx, restore, cluster); err != nil {
		logger.Error(err, "Failed to recreate restored database from seed server")
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonDatabaseCreateFailed,
			fmt.Sprintf("Restore succeeded but recreate-from-seed failed for %q: %v",
				restore.Spec.DatabaseName, err))
	}

	// Restore completed successfully
	r.updateRestoreStatus(ctx, restore, StatusCompleted, "Restore completed successfully")
	r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreCompleted, "Restore completed successfully")

	return ctrl.Result{}, nil
}

// createOrStartDatabase registers the restored database with Neo4j.
// If the database already exists (overwrite restore) it starts it; otherwise it creates it.
func (r *Neo4jRestoreReconciler) createOrStartDatabase(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer func() { _ = neo4jClient.Close() }()

	exists, err := neo4jClient.DatabaseExists(ctx, restore.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to check database existence: %w", err)
	}

	if exists {
		return neo4jClient.StartDatabase(ctx, restore.Spec.DatabaseName, false)
	}
	return neo4jClient.CreateDatabase(ctx, restore.Spec.DatabaseName, nil, false, false)
}

// recreateRestoredDatabaseOnCluster invokes `dbms.[cluster.]recreateDatabase`
// against the live cluster to force every server to re-seed its store from
// server-0 — the only PVC the restore Job wrote to. Without this step, the
// post-restart bootstrap picks the database's primary non-deterministically,
// and if a stale-data server wins consensus the restored data is overwritten
// when others re-sync from it.
//
// Skipped (no-op, no error) when:
//   - The target is standalone or a single-server cluster (Topology.Servers
//     < 2): nothing to re-seed across.
//   - Neo4j version doesn't support the recreate procedure (pre-5.24 SemVer
//     / pre-2025.02 CalVer): `RecreateDatabaseProcedure()` returns "".
//   - server-0 can't be located via SHOW SERVERS (defensive — if the
//     cluster's topology has drifted from spec, fall back to Neo4j's
//     auto-seed which picks the most up-to-date allocation; that's still
//     better than no-op since the restored server is by definition the
//     most up-to-date).
//
// Required Neo4j privileges: CREATE DATABASE + DROP DATABASE (per the
// recreate procedure docs). The admin secret used by the operator has both.
func (r *Neo4jRestoreReconciler) recreateRestoredDatabaseOnCluster(
	ctx context.Context,
	restore *neo4jv1beta1.Neo4jRestore,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) error {
	if cluster.Spec.Topology.Servers < 2 {
		// Standalone / single-server: server-0's PVC IS the cluster's
		// only data, no cross-server seeding needed.
		return nil
	}

	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		// Same fallback used elsewhere in the controller — assume
		// 5.26 defaults so we don't lose recreate on exotic tags.
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}
	if version.RecreateDatabaseProcedure() == "" {
		// Version doesn't support recreate. Log so operators see why
		// the post-restore deterministic-seed guarantee was skipped.
		log.FromContext(ctx).Info(
			"Skipping post-restore recreate: Neo4j version doesn't support the procedure",
			"version", fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch),
			"database", restore.Spec.DatabaseName)
		return nil
	}

	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client for recreate: %w", err)
	}
	defer func() { _ = neo4jClient.Close() }()

	servers, err := neo4jClient.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers for recreate seed: %w", err)
	}

	// Match server-0 by Address, not Name. SHOW SERVERS's `name` column
	// is a free-form display label (often empty or a UUID-derived
	// string) — the Pod hostname only appears in `address` (e.g.
	// `mycluster-server-0.mycluster-headless.ns.svc.cluster.local:7687`).
	// This is the same matching idiom used in WaitForServerAvailable
	// (internal/neo4j/client.go:WaitForServerAvailable) — keep it
	// consistent so a single change to Neo4j's naming would only break
	// one place.
	seedHostname := cluster.Name + "-server-0"
	var seedID string
	for _, s := range servers {
		if strings.Contains(s.Address, seedHostname) {
			seedID = s.ID
			break
		}
	}

	// Empty seeders → Neo4j auto-picks the most up-to-date allocation,
	// which post-restore IS server-0 (it has the freshest data). Used
	// as the fallback when we couldn't resolve server-0 by name.
	var seeders []string
	if seedID != "" {
		seeders = []string{seedID}
	} else {
		log.FromContext(ctx).Info(
			"Could not match server-0 by name in SHOW SERVERS; falling back to auto-seed",
			"expectedName", seedHostname, "serverCount", len(servers))
	}

	applied, err := neo4jClient.RecreateDatabase(ctx, version, restore.Spec.DatabaseName, seeders)
	if err != nil {
		return err
	}
	if applied {
		log.FromContext(ctx).Info(
			"Re-seeded restored database across all cluster servers",
			"database", restore.Spec.DatabaseName,
			"seedServerID", seedID, "seedServerName", seedHostname,
			"procedure", version.RecreateDatabaseProcedure())
	}
	return nil
}

func (r *Neo4jRestoreReconciler) validateRestore(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	// Validate source
	switch restore.Spec.Source.Type {
	case SourceTypeBackup:
		if restore.Spec.Source.BackupRef == "" {
			return fmt.Errorf("backupRef is required when source type is 'backup'")
		}
		backup := &neo4jv1beta1.Neo4jBackup{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      restore.Spec.Source.BackupRef,
			Namespace: restore.Namespace,
		}, backup); err != nil {
			return fmt.Errorf("backup %q not found: %w", restore.Spec.Source.BackupRef, err)
		}

	case "storage", SourceTypeS3, SourceTypeGCS, "azure":
		if restore.Spec.Source.BackupPath == "" {
			return fmt.Errorf("backupPath is required when source type is %q", restore.Spec.Source.Type)
		}

	case "pitr":
		if restore.Spec.Source.PITR == nil {
			return fmt.Errorf("pitr configuration is required when source type is 'pitr'")
		}
		if restore.Spec.Source.PITR.BaseBackup == nil && restore.Spec.Source.PointInTime == nil {
			return fmt.Errorf("pitr requires baseBackup configuration or pointInTime (or both)")
		}
		// PITR via Neo4jRestore is the neo4j-admin `--restore-until` path, which
		// only runs against a Neo4jEnterpriseStandalone target. Cluster restores
		// route to the in-place Cypher path (rule 75), which has no
		// point-in-time mechanism — so a cluster PITR would silently misbehave.
		// Reject it up front with an actionable pointer to the cluster-native
		// path (Neo4jDatabase.spec.seedConfig.restoreUntil).
		if isCluster, _, terr := r.isRestoreTargetTrueCluster(ctx, restore); terr == nil && isCluster {
			return fmt.Errorf("source.type=pitr is not supported for cluster targets (clusterRef %q resolves to a Neo4jEnterpriseCluster); Neo4jRestore PITR applies to Neo4jEnterpriseStandalone targets only. For cluster point-in-time recovery, create a Neo4jDatabase with spec.seedConfig.restoreUntil instead", restore.Spec.ClusterRef)
		}

	default:
		return fmt.Errorf("invalid source type %q: must be one of: backup, storage, s3, gcs, azure, pitr", restore.Spec.Source.Type)
	}

	if restore.Spec.DatabaseName == "" {
		return fmt.Errorf("databaseName is required")
	}
	// The database name is interpolated into the restore Job's shell command
	// and Cypher; restrict it to the Neo4j database-name grammar (no shell or
	// Cypher metacharacters) so it can't inject either. Defense-in-depth on top
	// of the CRD Pattern marker.
	if !validation.IsValidDatabaseName(restore.Spec.DatabaseName) {
		return fmt.Errorf("databaseName %q is invalid: must start with a letter, contain only letters, digits, dots or dashes, and be at most %d characters",
			restore.Spec.DatabaseName, validation.MaxDatabaseNameLength)
	}

	// Sharded databases must NOT be restored via Neo4jRestore — the Cypher
	// shape (`CREATE DATABASE … SET GRAPH SHARD … SET PROPERTY SHARDS …`)
	// is owned by Neo4jShardedDatabase, and the destructive restore path
	// is gated by `replaceExisting=true` + `force=true` (rule 63) on that
	// CR. Detect via Neo4jShardedDatabase lookup in the same namespace.
	shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{}
	if err := r.Get(ctx, types.NamespacedName{Name: restore.Spec.DatabaseName, Namespace: restore.Namespace}, shardedDB); err == nil {
		return fmt.Errorf(
			"database %q is a Neo4jShardedDatabase — use the Neo4jShardedDatabase restore path instead:\n"+
				"  spec:\n"+
				"    seedBackupRef: %q\n"+
				"    replaceExisting: true\n"+
				"    force: true\n"+
				"Sharded restores require the SET GRAPH SHARD / SET PROPERTY SHARDS clauses that only CREATE DATABASE accepts; dbms.recreateDatabase doesn't support sharded topology",
			restore.Spec.DatabaseName, restore.Spec.Source.BackupRef,
		)
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check whether %q is a Neo4jShardedDatabase: %w", restore.Spec.DatabaseName, err)
	}

	return nil
}

func (r *Neo4jRestoreReconciler) checkDatabaseExists(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			log.FromContext(ctx).Error(err, "Failed to close Neo4j client")
		}
	}()

	// Check if database exists
	exists, err := neo4jClient.DatabaseExists(ctx, restore.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if exists && (restore.Spec.Options == nil || !restore.Spec.Options.ReplaceExisting) {
		return fmt.Errorf("database %s already exists. Use replaceExisting option or force flag to overwrite", restore.Spec.DatabaseName)
	}

	return nil
}

// ensureRestoreTempStagingPVC creates a PVC for temporary staging if tempStorage is configured.
func (r *Neo4jRestoreReconciler) ensureRestoreTempStagingPVC(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	if restore.Spec.Options == nil || restore.Spec.Options.TempStorage == nil {
		return nil
	}
	pvcName := restore.Name + "-temp-staging"
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: restore.Namespace}, pvc); err == nil {
		return nil // already exists
	}

	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: restore.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(restore.Spec.Options.TempStorage.Size),
				},
			},
		},
	}
	if restore.Spec.Options.TempStorage.StorageClassName != "" {
		pvc.Spec.StorageClassName = &restore.Spec.Options.TempStorage.StorageClassName
	}
	if err := controllerutil.SetControllerReference(restore, pvc, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on temp PVC: %w", err)
	}
	return r.Create(ctx, pvc)
}

func (r *Neo4jRestoreReconciler) createRestoreJob(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (*batchv1.Job, error) {
	// Create temp staging PVC if configured
	if err := r.ensureRestoreTempStagingPVC(ctx, restore); err != nil {
		return nil, fmt.Errorf("failed to create temp staging PVC: %w", err)
	}

	jobName := restore.Name + "-restore"

	// Resolve source.type=backup into a concrete StorageLocation + per-run
	// subfolder once, then swap it onto a shallow restore copy so every
	// downstream builder (command, env vars, volumes, volume mounts) sees
	// the same concrete view. Without this dereference, type=backup
	// restores silently pointed at an empty volume (recheck gap 1).
	resolvedSource, err := r.resolveRestoreSource(ctx, restore)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve restore source: %w", err)
	}
	resolvedRestore := *restore
	resolvedRestore.Spec.Source = resolvedSource

	// Ensure the restore ServiceAccount exists with the resolved cloud
	// identity's annotations (IRSA / GKE WI / Azure WI). Without this the
	// restore Pod runs as the namespace's `default` SA, so any cloud
	// access that relies on workload identity instead of static creds
	// silently fails with "no creds" — backup worked, restore couldn't
	// see the same bucket (recheck gap 1 follow-up).
	if err := r.ensureRestoreServiceAccount(ctx, &resolvedRestore); err != nil {
		return nil, fmt.Errorf("failed to ensure restore ServiceAccount: %w", err)
	}

	// Build restore command
	restoreCmd, err := r.buildRestoreCommand(ctx, &resolvedRestore, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build restore command: %w", err)
	}

	// Create job spec
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: restore.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j-restore",
				"app.kubernetes.io/instance":   restore.Name,
				"app.kubernetes.io/component":  "restore",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: func() *int32 { v := int32(300); return &v }(),
			BackoffLimit:            func(i int32) *int32 { return &i }(1), // Restore should not retry
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyNever,
					SecurityContext: hardenedRestorePodSecurityContext(),
					// Mirror the backup path: a dedicated SA carries
					// workload-identity annotations (IRSA / GKE WI / Azure
					// WI). Without this the restore Pod ran as the
					// namespace `default` SA and any IAM-via-workload-
					// identity flow silently failed.
					ServiceAccountName: restoreServiceAccountName,
					// Restore pod uses the cluster's Neo4j image; propagate
					// pull secrets so private-registry clusters can restore.
					// Without this, a private-image cluster restore fails
					// with ImagePullBackOff before any neo4j-admin runs.
					ImagePullSecrets: resources.ImagePullSecretsFromNames(cluster.Spec.Image.PullSecrets),
					Containers: []corev1.Container{
						{
							Name:            "neo4j-restore",
							Image:           fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
							SecurityContext: hardenedRestoreContainerSecurityContext(),
							Command:         []string{"/bin/sh"},
							Args:            []string{"-c", restoreCmd},
							Env: func() []corev1.EnvVar {
								envs := []corev1.EnvVar{
									{
										Name: "NEO4J_ADMIN_PASSWORD",
										ValueFrom: &corev1.EnvVarSource{
											SecretKeyRef: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: cluster.Spec.Auth.AdminSecret,
												},
												Key: "password",
											},
										},
									},
								}
								// Inject cloud provider credentials for S3/GCS/Azure restores
								if cloudEnvs := r.buildRestoreCloudEnvVars(&resolvedRestore); cloudEnvs != nil {
									envs = append(envs, cloudEnvs...)
								}
								return envs
							}(),
							VolumeMounts: r.buildRestoreVolumeMounts(&resolvedRestore),
							Resources:    resolveRestoreJobResources(restore.Spec.Options),
						},
					},
					Volumes: r.buildRestoreVolumes(ctx, &resolvedRestore),
				},
			},
		},
	}

	// Set controller reference
	if err := controllerutil.SetControllerReference(restore, job, r.Scheme); err != nil {
		return nil, err
	}

	// Create the job. Two reconciles can race here: when stopCluster=true,
	// the scale-down path goes through a 10s wait, so the controller queues
	// a fresh reconcile via watches before the original reconcile has
	// finished creating the Job. Both reconciles then call Create — one
	// wins, the other gets AlreadyExists and (without this fallback)
	// terminal-fails the restore via the "Restore previously failed"
	// guard, even though the Job actually ran and succeeded.
	// AlreadyExists is treated as "another reconcile got there first";
	// re-fetch the existing Job so the caller sees a populated object.
	if err := r.Create(ctx, job); err != nil {
		if !errors.IsAlreadyExists(err) {
			return nil, err
		}
		existing := &batchv1.Job{}
		if getErr := r.Get(ctx, types.NamespacedName{
			Name: job.Name, Namespace: job.Namespace,
		}, existing); getErr != nil {
			return nil, fmt.Errorf("restore Job %s/%s already exists but cannot be re-fetched: %w",
				job.Namespace, job.Name, getErr)
		}
		return existing, nil
	}

	return job, nil
}

// cloudBlockForRestore returns the CloudBlock from the restore's storage config.
func cloudBlockForRestore(restore *neo4jv1beta1.Neo4jRestore) *neo4jv1beta1.CloudBlock {
	if restore.Spec.Source.Storage != nil && restore.Spec.Source.Storage.Cloud != nil {
		return restore.Spec.Source.Storage.Cloud
	}
	return nil
}

// ensureRestoreServiceAccount creates (or updates) the neo4j-restore-sa
// ServiceAccount in the restore's namespace and applies any
// workload-identity annotations declared in the resolved cloud block.
// Mirrors ensureBackupServiceAccount on the backup side. No Role or
// RoleBinding is created — the restore Job runs neo4j-admin directly and
// does not need Kubernetes API access.
//
// Called with the RESOLVED restore (after resolveRestoreSource has
// dereferenced source.type=backup), so for backupRef-based restores the
// annotations correctly come from the referenced Neo4jBackup's cloud
// config rather than the empty restore.Spec.Source.Storage.Cloud.
func (r *Neo4jRestoreReconciler) ensureRestoreServiceAccount(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	namespace := restore.Namespace

	// Collect workload-identity annotations from the resolved cloud block
	// (if any). Static-credential paths (CredentialsSecretRef) need no
	// SA annotations; the env vars feed the SDK directly.
	wiAnnotations := map[string]string{}
	cloud := cloudBlockForRestore(restore)
	if cloud != nil && cloud.Identity != nil && cloud.Identity.AutoCreate != nil {
		for k, v := range cloud.Identity.AutoCreate.Annotations {
			wiAnnotations[k] = v
		}
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreServiceAccountName,
			Namespace: namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		// Apply workload-identity annotations; preserve any other
		// annotations already present (e.g. set by cloud-controller or
		// the user directly).
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

// resolveBackupRef delegates to the package-shared ResolveBackupRef. Kept as
// a method on the receiver so existing call sites in the restore controller
// can stay unchanged; new controllers should call ResolveBackupRef directly.
func (r *Neo4jRestoreReconciler) resolveBackupRef(ctx context.Context, backupRef, namespace string) (storage neo4jv1beta1.StorageLocation, backupPath string, err error) {
	return ResolveBackupRef(ctx, r.Client, backupRef, namespace)
}

// resolveRestoreSource dereferences source.type=backup into a concrete
// RestoreSource (storage type, bucket/path, cloud creds, per-run subfolder).
//
// For source.type=storage|pitr|s3|gcs|azure the input is already concrete
// and returned unchanged.
//
// Gap-1 fix from the recheck pass: the previous source.type=backup
// implementation hardcoded `/backup/<backup-name>` over an EmptyDir volume,
// which ignored spec.storage.type, spec.storage.path, and the per-run
// subfolder layout — every type=backup restore pointed at an empty mount.
// The resolved view feeds the existing build* helpers unchanged: callers
// get s3:// / gs:// / azb:// / PVC paths and matching cloud creds for free.
func (r *Neo4jRestoreReconciler) resolveRestoreSource(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) (neo4jv1beta1.RestoreSource, error) {
	if restore.Spec.Source.Type != SourceTypeBackup {
		return restore.Spec.Source, nil
	}

	storage, backupPath, err := r.resolveBackupRef(ctx, restore.Spec.Source.BackupRef, restore.Namespace)
	if err != nil {
		return neo4jv1beta1.RestoreSource{}, err
	}

	return neo4jv1beta1.RestoreSource{
		// Normalize Type to "storage" so the existing buildRestoreCommand
		// switch matches the cloud / pvc branch unconditionally. The
		// underlying storage.type (s3 / gcs / azure / pvc) still drives
		// URI construction inside buildRestoreFromPath.
		Type:        "storage",
		Storage:     &storage,
		BackupPath:  backupPath,
		PointInTime: restore.Spec.Source.PointInTime,
	}, nil
}

// buildRestoreCloudEnvVars injects cloud provider credentials into the restore Job,
// mirroring the backup controller's buildCloudEnvVars.
func (r *Neo4jRestoreReconciler) buildRestoreCloudEnvVars(restore *neo4jv1beta1.Neo4jRestore) []corev1.EnvVar {
	cloud := cloudBlockForRestore(restore)
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
		if cloud.EndpointURL != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AWS_ENDPOINT_URL_S3",
				Value: cloud.EndpointURL,
			})
		}
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
		return []corev1.EnvVar{
			{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/var/secrets/gcp/credentials.json"},
		}
	}
	return nil
}

// buildRestoreFromPath constructs a cloud URI (s3://, gs://, azb://) from the
// restore source's storage location and backup path, or returns a local path.
func (r *Neo4jRestoreReconciler) buildRestoreFromPath(restore *neo4jv1beta1.Neo4jRestore) string {
	st := restore.Spec.Source.Storage
	if st == nil {
		return restore.Spec.Source.BackupPath
	}
	basePath := st.Path
	backupFile := restore.Spec.Source.BackupPath
	// Combine storage path and backup filename
	var fullPath string
	if basePath != "" && backupFile != "" {
		fullPath = fmt.Sprintf("%s/%s", basePath, backupFile)
	} else if basePath != "" {
		fullPath = basePath
	} else {
		fullPath = backupFile
	}

	switch st.Type {
	case "s3":
		return fmt.Sprintf("s3://%s/%s", st.Bucket, fullPath)
	case "gcs":
		return fmt.Sprintf("gs://%s/%s", st.Bucket, fullPath)
	case "azure":
		return fmt.Sprintf("azb://%s/%s", st.Bucket, fullPath)
	default: // pvc
		if backupFile != "" {
			return fmt.Sprintf("/backup/%s", backupFile)
		}
		return "/backup"
	}
}

// isLocalPVCRestoreSource reports whether the restore reads from a
// PVC-mounted backup directory (as opposed to a cloud URI). Used in two
// places: (1) shell-side file-path resolution via buildLocalRestoreFilePath,
// and (2) the `mkdir -p /tmp/restore-tmp` prelude that pairs with the
// default `--temp-path=/tmp/restore-tmp`. Centralising the condition makes
// these two stay in sync.
//
// **Nil Storage is NOT treated as PVC** — that shape (Source.Type=storage
// without a Storage block) is broken end-to-end in the operator:
// buildRestoreVolumes only adds a `backup-storage` volume when
// Source.Storage != nil, so a nil-Storage Pod fails to start with
// "volume not found". buildRestoreFromPath also returns a bare relative
// path (no `/backup/` prefix) for nil-Storage. Treating it as PVC here
// would mis-apply the PVC fixups (shell substitution expects a
// `/backup/...` source path, default --temp-path / prelude assume a
// real backup mount exists). The nil-Storage CR shape is essentially a
// no-op for our fixups — the user must specify Storage explicitly to
// engage the PVC path. The underlying pre-existing brokenness is out
// of scope for this PR; the validator should reject nil-Storage as a
// follow-up.
func isLocalPVCRestoreSource(restore *neo4jv1beta1.Neo4jRestore) bool {
	if restore.Spec.Source.Type != "storage" {
		return false
	}
	st := restore.Spec.Source.Storage
	if st == nil {
		return false
	}
	return st.Type == "" || st.Type == "pvc"
}

// buildLocalRestoreFilePath returns a shell command-substitution expression
// that resolves the target database's `<dbname>-*.backup` file path at Pod
// exec time, suitable to pass to `neo4j-admin database restore --from-path=`.
//
// Returns "" for sources that don't need shell-side resolution (cloud URIs).
//
// Why this is necessary: per the Neo4j 5.26 docs, `neo4j-admin database
// restore --from-path=<path>` requires <path> to be a FILE path (or a
// comma-separated list of file paths), not a directory containing the
// .backup file. The operator's backup output is
//
//	/backup/<run-id>/<dbname>-<timestamp>.backup
//
// but the operator doesn't know <timestamp> at reconcile time (it's set by
// neo4j-admin database backup at execution). Instead of staging the file or
// teaching the operator to predict timestamps, the shell resolves the path
// at Pod startup via command substitution: `$(ls .../<dbname>-*.backup
// | head -1)`. This sidesteps both the "directory not file" issue and the
// multi-database directory issue (cluster-target backups co-locate one
// .backup per database in one folder).
//
// Cloud URIs (s3://, gs://, azb://) bypass this — neo4j-admin's native
// cloud readers handle per-file selection from the bucket prefix, and the
// shell `ls` would have no filesystem to enumerate anyway.
//
// Security note: shellQuote() wraps the database name (user-controlled via
// spec.DatabaseName) so shell metacharacters can't escape the glob.
func buildLocalRestoreFilePath(restore *neo4jv1beta1.Neo4jRestore, sourceDir string) string {
	if !isLocalPVCRestoreSource(restore) {
		return ""
	}
	if restore.Spec.DatabaseName == "" {
		return ""
	}
	return resolveLocalPVCFromPath(sourceDir, restore.Spec.DatabaseName)
}

// resolveLocalPVCFromPath is the path-based equivalent of
// buildLocalRestoreFilePath: given a `--from-path` string and a database
// name, returns the shell command-substitution form for local PVC mounts
// (path starts with `/backup`) and the input unchanged for cloud URIs.
//
// This exists for the PITR code path where the source is determined by
// resolving a `BaseBackup` (which could be a Neo4jBackup ref OR explicit
// storage), not by inspecting `Source.Type/Storage` directly. The PVC
// detection is purely string-based here: anything starting with `/backup`
// is a local mount, anything else (s3://, gs://, azb://) is a URI that
// neo4j-admin's native readers handle.
//
// Returns the input unchanged for empty DB names (defensive — neo4j-admin
// will surface a clearer error than a malformed glob).
//
// **Shell-injection guard**: both backupPath and databaseName are passed
// through shellQuote(). backupPath ends with the user-controlled
// `spec.source.backupPath` field (the operator just prepends `/backup/`)
// and an unquoted value like `foo; rm -rf /data #` would otherwise close
// the `ls` invocation early and execute arbitrary commands in the restore
// Pod, which mounts `/data` (server-0's data PVC, read-write) and carries
// `NEO4J_ADMIN_PASSWORD` in its env. Quoting both inputs makes the
// substitution body a single token that `ls` receives as one argument;
// any metacharacter is taken literally.
func resolveLocalPVCFromPath(backupPath, databaseName string) string {
	if databaseName == "" || !strings.HasPrefix(backupPath, "/backup") {
		return backupPath
	}
	// `ls ... | tail -1` picks the LATEST matching file. neo4j-admin embeds
	// an ISO-8601 timestamp in each artifact's filename
	// (`<dbname>-YYYY-MM-DDThh-mm-ss.backup`), and ISO-8601 sorts
	// lexicographically into chronological order — so `ls` (default
	// alphabetical) | `tail -1` reliably returns the most-recent run when
	// multiple runs share the directory (the canonical layout for
	// `--type=DIFF` chaining). Callers that need a specific run pin it via
	// spec.source.backupRunID → the resolver pre-substitutes the captured
	// ArtifactFilename into backupPath, in which case backupPath is already
	// a file path and `resolveLocalPVCFromPath` is not used. If no match
	// exists, ls prints to stderr and tail returns nothing, so --from-path=
	// becomes empty and neo4j-admin errors with a clear "missing argument"
	// message.
	return fmt.Sprintf("$(ls %s/%s-*.backup | tail -1)",
		shellQuote(backupPath), shellQuote(databaseName))
}

// isPVCBackupPath reports whether a resolved restore `--from-path` value
// points at a local PVC mount (vs. a cloud URI). The PVC volume mount in
// buildRestoreVolumeMounts always uses `/backup` as the mount path, so
// the prefix check is sufficient.
func isPVCBackupPath(backupPath string) bool {
	return strings.HasPrefix(backupPath, "/backup")
}

func (r *Neo4jRestoreReconciler) buildRestoreCommand(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	var backupPath string

	// Determine backup path based on source type. source.type=backup is
	// dereferenced upstream by resolveRestoreSource — by the time we get
	// here Source.Type is "storage"/"s3"/"gcs"/"azure"/"pitr". The legacy
	// case "backup" branch that hardcoded `/backup/<backup-name>` over an
	// EmptyDir was always broken; do not reintroduce it (recheck gap 1).
	switch restore.Spec.Source.Type {
	case "storage", SourceTypeS3, SourceTypeGCS, "azure":
		backupPath = r.buildRestoreFromPath(restore)
	case "pitr":
		return r.buildPITRRestoreCommand(ctx, restore, cluster)
	case SourceTypeBackup:
		// Should be unreachable: resolveRestoreSource swaps Type away
		// from "backup" before this function runs. Surface loudly if a
		// future caller bypasses resolution.
		return "", fmt.Errorf("internal: source.type=backup reached buildRestoreCommand without being resolved")
	}

	// PVC sources need shell-side file-path resolution. Reason:
	// `neo4j-admin database restore --from-path=<path>` requires a FILE
	// path (not a directory), and the operator's backup writes
	//   /backup/<run-id>/<dbname>-<timestamp>.backup
	// where <timestamp> is set by neo4j-admin at backup execution and isn't
	// known to the operator at restore-CR reconcile time. The shell resolves
	// it via `$(ls .../<dbname>-*.backup | head -1)` at Pod startup.
	// This also handles the cluster-target backup case where multiple
	// `*.backup` files co-locate in one directory (one per database) — the
	// glob naturally selects only the requested DB's file.
	if resolved := buildLocalRestoreFilePath(restore, backupPath); resolved != "" {
		backupPath = resolved
	} else if !isLocalPVCRestoreSource(restore) && backupPath != "" {
		// Cloud --from-path (s3://, gs://, azb://): quote the whole URI so a
		// crafted spec.source.{bucket,path,backupPath} can't break out of
		// /bin/sh -c. PVC sources take the branch above ($(ls …)), which must
		// stay unquoted to execute the command substitution.
		backupPath = shellQuote(backupPath)
	}

	// Extract Neo4j version from cluster image
	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Build the neo4j-admin restore command with correct Neo4j 5.26+ syntax.
	// PVC sources also get a `rm -rf /tmp/restore-tmp && mkdir -p` prelude so
	// the default --temp-path (added further down) starts empty per Pod
	// attempt — neo4j-admin refuses to write into a non-empty temp dir. The
	// /tmp dir is the Pod's in-memory tmpfs, cheap and self-cleaning.
	preludeCmd := ""
	if isLocalPVCRestoreSource(restore) {
		preludeCmd = "rm -rf /tmp/restore-tmp && mkdir -p /tmp/restore-tmp && "
	}
	cmd := preludeCmd + neo4j.GetRestoreCommand(version, restore.Spec.DatabaseName, backupPath)

	// Add --overwrite-destination flag if force is specified
	if restore.Spec.Force {
		cmd += " --overwrite-destination=true"
	}

	// Add --temp-path when the user has configured staging storage.
	// TempStorage (PVC reference) takes priority, then explicit TempPath,
	// then a sensible default for PVC sources.
	switch {
	case restore.Spec.Options != nil && restore.Spec.Options.TempStorage != nil:
		cmd += " --temp-path=/tmp/neo4j-staging"
	case restore.Spec.Options != nil && restore.Spec.Options.TempPath != "":
		cmd += " --temp-path=" + shellQuote(restore.Spec.Options.TempPath)
	case isLocalPVCRestoreSource(restore):
		// Default for PVC sources. neo4j-admin's restore needs a
		// writable scratch dir to extract the artifact; if not told
		// otherwise it writes alongside the source file. The backup
		// PVC is mounted ReadOnly (safety — we never want restore to
		// mutate user backups), so without an explicit --temp-path
		// neo4j-admin errors with
		//   FileSystemException: .../<dbname>-temp-extracted-artifacts-0: Read-only file system
		// /tmp is the Pod's tmpfs — empty per Pod start, auto-cleaned
		// on exit, plenty fast. The paired prelude (rm -rf + mkdir -p
		// upstream of GetRestoreCommand) ensures the dir starts empty,
		// which neo4j-admin requires.
		cmd += " --temp-path=/tmp/restore-tmp"
	}

	// Add point-in-time restore if specified
	if restore.Spec.Source.PointInTime != nil {
		t := restore.Spec.Source.PointInTime.Time.UTC()
		cmd += fmt.Sprintf(` --restore-until="%s"`, t.Format("2006-01-02 15:04:05"))
	}

	// Add additional arguments if specified. Each arg is shell-quoted (the
	// pod runs the command via /bin/sh -c and holds the admin password), as
	// the backup path already does.
	if restore.Spec.Options != nil && len(restore.Spec.Options.AdditionalArgs) > 0 {
		for _, arg := range restore.Spec.Options.AdditionalArgs {
			cmd += " " + shellQuote(arg)
		}
	}

	return cmd, nil
}

// buildPITRRestoreCommand builds a Point-in-Time Recovery restore command.
// PITR in Neo4j is implemented via the --restore-until flag on neo4j-admin database restore;
// there is no separate log-replay step.
func (r *Neo4jRestoreReconciler) buildPITRRestoreCommand(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	pitrConfig := restore.Spec.Source.PITR
	if pitrConfig == nil {
		return "", fmt.Errorf("PITR configuration is required for PITR restore")
	}

	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Determine backup source path from base backup.
	//
	// type=backup is dereferenced via resolveBackupRef — same path as the
	// main restore flow uses, so a PITR base backup pointing at a
	// Neo4jBackup CR picks up that CR's storage.{type,bucket,path,cloud}
	// and the per-run subfolder from history. The legacy
	// `/backup/<backup-ref>` PVC hardcode (recheck gap 1) is gone.
	var backupPath string
	if pitrConfig.BaseBackup != nil {
		switch pitrConfig.BaseBackup.Type {
		case "backup":
			storage, runPath, err := r.resolveBackupRef(ctx, pitrConfig.BaseBackup.BackupRef, restore.Namespace)
			if err != nil {
				return "", fmt.Errorf("pitr base backup: %w", err)
			}
			// Reuse buildRestoreFromPath by stuffing the resolved storage
			// + per-run subfolder into a synthetic restore view. Keeps URI
			// construction identical to the type=storage path.
			tmp := &neo4jv1beta1.Neo4jRestore{Spec: neo4jv1beta1.Neo4jRestoreSpec{
				Source: neo4jv1beta1.RestoreSource{Storage: &storage, BackupPath: runPath},
			}}
			backupPath = r.buildRestoreFromPath(tmp)
		case "storage":
			// Construct cloud URI if storage has cloud type, otherwise use local path
			if pitrConfig.BaseBackup.Storage != nil {
				st := pitrConfig.BaseBackup.Storage
				basePath := st.Path
				backupFile := pitrConfig.BaseBackup.BackupPath
				fullPath := backupFile
				if basePath != "" && backupFile != "" {
					fullPath = fmt.Sprintf("%s/%s", basePath, backupFile)
				} else if basePath != "" {
					fullPath = basePath
				}
				switch st.Type {
				case "s3":
					backupPath = fmt.Sprintf("s3://%s/%s", st.Bucket, fullPath)
				case "gcs":
					backupPath = fmt.Sprintf("gs://%s/%s", st.Bucket, fullPath)
				case "azure":
					backupPath = fmt.Sprintf("azb://%s/%s", st.Bucket, fullPath)
				default:
					backupPath = fmt.Sprintf("/backup/%s", backupFile)
				}
			} else {
				backupPath = pitrConfig.BaseBackup.BackupPath
			}
		default:
			return "", fmt.Errorf("invalid base backup type: %s", pitrConfig.BaseBackup.Type)
		}
	}

	if backupPath == "" {
		return "", fmt.Errorf("no backup source path could be determined for PITR restore")
	}

	// Apply the same PVC fixups as buildRestoreCommand. PITR base-backup
	// resolution can also produce a `/backup/<run-subfolder>` directory
	// path (either from `BaseBackup.Type=backup` dereferencing through
	// resolveBackupRef, or from `BaseBackup.Type=storage` with a
	// PVC-backed StorageLocation). Without these fixups, PITR restores
	// hit the same three failure modes the main path used to:
	//   1. neo4j-admin rejects `--from-path=<dir>` (requires a FILE).
	//   2. neo4j-admin can't extract into the source dir when the
	//      backup PVC is mounted ReadOnly (its default behavior).
	//   3. The default --temp-path needs an empty directory.
	// Cloud URIs (s3://, gs://, azb://) skip both fixups via
	// isPVCBackupPath. PVC detection happens BEFORE the shell-resolution
	// transformation so the post-transform `$(ls ...)` form doesn't have
	// to be re-detected downstream.
	isPVC := isPVCBackupPath(backupPath)
	if isPVC {
		backupPath = resolveLocalPVCFromPath(backupPath, restore.Spec.DatabaseName)
	} else if backupPath != "" {
		// Cloud --from-path URI: quote so spec.source.{bucket,path,backupPath}
		// can't break out of /bin/sh -c (PVC takes the $(ls …) branch above).
		backupPath = shellQuote(backupPath)
	}
	preludeCmd := ""
	if isPVC {
		preludeCmd = "rm -rf /tmp/restore-tmp && mkdir -p /tmp/restore-tmp && "
	}

	cmd := preludeCmd + neo4j.GetRestoreCommand(version, restore.Spec.DatabaseName, backupPath)

	if restore.Spec.Force {
		cmd += " --overwrite-destination=true"
	}

	// Mirror the main path's --temp-path handling: user-supplied options
	// win; otherwise default to /tmp/restore-tmp for PVC-backed sources
	// (the prelude above guarantees it's empty). Without this, neo4j-admin
	// fails with `FileSystemException: Read-only file system` because the
	// backup PVC is mounted ReadOnly.
	switch {
	case restore.Spec.Options != nil && restore.Spec.Options.TempStorage != nil:
		cmd += " --temp-path=/tmp/neo4j-staging"
	case restore.Spec.Options != nil && restore.Spec.Options.TempPath != "":
		cmd += " --temp-path=" + shellQuote(restore.Spec.Options.TempPath)
	case isPVC:
		cmd += " --temp-path=/tmp/restore-tmp"
	}

	// --restore-until is the Neo4j PITR mechanism
	if restore.Spec.Source.PointInTime != nil {
		t := restore.Spec.Source.PointInTime.Time.UTC()
		cmd += fmt.Sprintf(` --restore-until="%s"`, t.Format("2006-01-02 15:04:05"))
	}

	return cmd, nil
}

func (r *Neo4jRestoreReconciler) buildRestoreVolumeMounts(restore *neo4jv1beta1.Neo4jRestore) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{
			Name:      "backup-storage",
			MountPath: "/backup",
			ReadOnly:  true,
		},
		{
			Name:      "neo4j-data",
			MountPath: "/data",
		},
	}

	// Add transaction log volume mount for PITR
	if restore.Spec.Source.Type == "pitr" && restore.Spec.Source.PITR != nil && restore.Spec.Source.PITR.LogStorage != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "transaction-logs",
			MountPath: "/transaction-logs",
			ReadOnly:  true,
		})
	}

	// GCP credentials mount
	cloud := cloudBlockForRestore(restore)
	if cloud != nil && cloud.Provider == "gcp" && cloud.CredentialsSecretRef != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "gcp-credentials",
			MountPath: "/var/secrets/gcp",
			ReadOnly:  true,
		})
	}

	// Temp staging PVC for cloud operations (created by ensureRestoreTempStagingPVC)
	if restore.Spec.Options != nil && restore.Spec.Options.TempStorage != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "temp-staging",
			MountPath: "/tmp/neo4j-staging",
		})
	}

	return mounts
}

func (r *Neo4jRestoreReconciler) buildRestoreVolumes(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) []corev1.Volume {
	// Always mount the cluster's data PVC for /data — never an EmptyDir.
	// Writing the restored database into an EmptyDir succeeded silently and
	// then evaporated when the Pod exited, so users running stopCluster=false
	// observed "restore complete" with the cluster's actual data unchanged
	// (issue #117). The stopCluster flag now only controls whether the
	// operator coordinates the scale-down; the data volume is always the
	// real PVC. The startRestore preflight blocks running restores against
	// a live cluster when stopCluster=false, so the multi-attach scenario
	// is caught earlier with a clear error.
	//
	// Clusters use "data-{name}-server-0", standalones use "neo4j-data-{name}-0".
	dataPVCName := fmt.Sprintf("data-%s-server-0", restore.Spec.ClusterRef)
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: dataPVCName, Namespace: restore.Namespace}, pvc); err != nil {
		// Cluster PVC not found — try standalone naming
		dataPVCName = fmt.Sprintf("neo4j-data-%s-0", restore.Spec.ClusterRef)
	}
	dataVolume := corev1.Volume{
		Name: "neo4j-data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: dataPVCName,
			},
		},
	}

	volumes := []corev1.Volume{dataVolume}

	// Add storage volume based on source type. source.type=backup is
	// resolved upstream by resolveRestoreSource into the backup's
	// Spec.Storage, so by the time we get here Source.Storage is the
	// concrete StorageLocation (PVC or cloud) and the switch below routes
	// correctly. The legacy EmptyDir-for-backup branch is removed —
	// it dropped the backup's real storage, which broke restore (gap 1).
	if restore.Spec.Source.Storage != nil {
		switch restore.Spec.Source.Storage.Type {
		case "pvc":
			if restore.Spec.Source.Storage.PVC.Name != "" {
				volumes = append(volumes, corev1.Volume{
					Name: "backup-storage",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: restore.Spec.Source.Storage.PVC.Name,
							ReadOnly:  true,
						},
					},
				})
			} else {
				volumes = append(volumes, corev1.Volume{
					Name: "backup-storage",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				})
			}
		default:
			// For cloud storage (s3, gcs, azure), neo4j-admin reads directly from cloud URIs
			// via env vars — no local volume needed. An EmptyDir is still mounted at /backup
			// as a scratch space for neo4j-admin's temp files.
			volumes = append(volumes, corev1.Volume{
				Name: "backup-storage",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})

			// GCP credentials: mount the service-account JSON Secret
			cloud := cloudBlockForRestore(restore)
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
		}
	} else if restore.Spec.Source.Type == "pitr" {
		// Add backup storage volume for PITR base backup
		if restore.Spec.Source.PITR != nil && restore.Spec.Source.PITR.BaseBackup != nil {
			volumes = append(volumes, corev1.Volume{
				Name: "backup-storage",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		}

		// Add transaction log storage volume for PITR
		if restore.Spec.Source.PITR != nil && restore.Spec.Source.PITR.LogStorage != nil {
			switch restore.Spec.Source.PITR.LogStorage.Type {
			case "pvc":
				if restore.Spec.Source.PITR.LogStorage.PVC.Name != "" {
					volumes = append(volumes, corev1.Volume{
						Name: "transaction-logs",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: restore.Spec.Source.PITR.LogStorage.PVC.Name,
								ReadOnly:  true,
							},
						},
					})
				}
			default:
				volumes = append(volumes, corev1.Volume{
					Name: "transaction-logs",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				})
			}
		}
	}

	// Temp staging PVC for cloud operations (created by ensureRestoreTempStagingPVC)
	if restore.Spec.Options != nil && restore.Spec.Options.TempStorage != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "temp-staging",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: restore.Name + "-temp-staging",
				},
			},
		})
	}

	return volumes
}

// resolveStatefulSetName finds the StatefulSet for a cluster or standalone.
// Clusters use "{name}-server", standalones use just "{name}".
func (r *Neo4jRestoreReconciler) resolveStatefulSetName(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	// Try cluster naming convention first: {name}-server
	serverName := cluster.Name + "-server"
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: serverName, Namespace: cluster.Namespace}, sts); err == nil {
		return serverName, nil
	}
	// Fall back to standalone naming: {name} (no suffix)
	if err := r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, sts); err == nil {
		return cluster.Name, nil
	}
	return "", fmt.Errorf("StatefulSet not found for %s (tried %s-server and %s)", cluster.Name, cluster.Name, cluster.Name)
}

// refuseRestoreIfPodsRunning returns an error if the target cluster has any
// server pods. Used when restore.spec.stopCluster=false to prevent silently
// running a restore against a live cluster (which holds the database file
// lock and would either fail neo4j-admin restore or, worse, write the result
// into a non-PVC volume that's discarded when the pod exits — issue #117).
func (r *Neo4jRestoreReconciler) refuseRestoreIfPodsRunning(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(resources.ServerPodSelector(cluster.Name))); err != nil {
		return fmt.Errorf("failed to list server pods for restore preflight: %w", err)
	}
	if len(pods.Items) > 0 {
		return fmt.Errorf("restore %q cannot run against a live cluster: %d server pod(s) of %q are still present. Set spec.stopCluster=true to let the operator coordinate the scale-down, or scale the cluster to 0 manually before applying this restore",
			restore.Name, len(pods.Items), cluster.Name)
	}
	return nil
}

// setRestoreInProgressAnnotation marks the target cluster CR with the
// restore-in-progress annotation so the cluster controller stops re-asserting
// sts.Spec.Replicas (issue #117). If the annotation is already set to a
// different restore, returns an error — two restores against the same cluster
// cannot run concurrently.
func (r *Neo4jRestoreReconciler) setRestoreInProgressAnnotation(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		if existing, ok := latest.Annotations[RestoreInProgressAnnotation]; ok && existing != restore.Name {
			return fmt.Errorf("cluster %q already has a restore in progress by Neo4jRestore %q; cannot start %q", cluster.Name, existing, restore.Name)
		}
		if latest.Annotations == nil {
			latest.Annotations = map[string]string{}
		}
		if latest.Annotations[RestoreInProgressAnnotation] == restore.Name {
			return nil
		}
		latest.Annotations[RestoreInProgressAnnotation] = restore.Name
		return r.Update(ctx, latest)
	})
}

// clearRestoreInProgressAnnotation removes the restore-in-progress annotation
// from the target cluster CR, but only if it was set by THIS restore CR.
// Idempotent — safe to call from cleanup paths even if the annotation was
// never set or was already cleared.
func (r *Neo4jRestoreReconciler) clearRestoreInProgressAnnotation(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, clusterName, clusterNamespace string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: clusterNamespace}, latest); err != nil {
			if errors.IsNotFound(err) {
				return nil // cluster gone — nothing to clean
			}
			return err
		}
		owner, ok := latest.Annotations[RestoreInProgressAnnotation]
		if !ok || owner != restore.Name {
			return nil // not our annotation to clear
		}
		delete(latest.Annotations, RestoreInProgressAnnotation)
		return r.Update(ctx, latest)
	})
}

func (r *Neo4jRestoreReconciler) stopCluster(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Stopping cluster for restore", "cluster", cluster.Name)

	stsName, err := r.resolveStatefulSetName(ctx, cluster)
	if err != nil {
		return err
	}

	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      stsName,
		Namespace: cluster.Namespace,
	}

	if err := r.Get(ctx, stsKey, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Scale down to 0 replicas
	originalReplicas := *sts.Spec.Replicas
	sts.Spec.Replicas = ptr.To(int32(0))

	// Store original replica count in annotation for later restoration
	if sts.Annotations == nil {
		sts.Annotations = make(map[string]string)
	}
	sts.Annotations["neo4j.neo4j.com/original-replicas"] = fmt.Sprintf("%d", originalReplicas)

	if err := r.Update(ctx, sts); err != nil {
		return fmt.Errorf("failed to scale down StatefulSet: %w", err)
	}

	// Wait for all pods to be deleted
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for cluster to stop")
		case <-ticker.C:
			pods := &corev1.PodList{}
			if err := r.List(ctx, pods, client.InNamespace(cluster.Namespace),
				client.MatchingLabels(resources.ServerPodSelector(cluster.Name))); err != nil {
				logger.Error(err, "Failed to list pods")
				continue
			}

			if len(pods.Items) == 0 {
				logger.Info("Cluster stopped successfully")
				return nil
			}

			logger.Info("Waiting for pods to terminate", "remaining", len(pods.Items))
		}
	}
}

func (r *Neo4jRestoreReconciler) startCluster(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting cluster after restore", "cluster", cluster.Name)

	stsName, err := r.resolveStatefulSetName(ctx, cluster)
	if err != nil {
		return err
	}

	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      stsName,
		Namespace: cluster.Namespace,
	}

	if err := r.Get(ctx, stsKey, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Restore original replica count from annotation. The annotation
	// is deleted on first successful startCluster, so a re-entry from a
	// concurrent reconcile (e.g. the post-Job-success flow racing
	// itself while waitForClusterReady blocks the original reconcile)
	// finds it missing. That's not a failure — the cluster is already
	// being scaled back up by the original caller. Treat the missing
	// annotation as idempotent success: if the STS has non-zero
	// replicas (or matches cluster.Spec.Topology.Servers) the scale-up
	// has already happened. Returning an error here used to terminal-
	// fail the restore via the "Restore previously failed" guard, even
	// though everything was actually working.
	originalReplicasStr, exists := sts.Annotations["neo4j.neo4j.com/original-replicas"]
	if !exists {
		current := int32(0)
		if sts.Spec.Replicas != nil {
			current = *sts.Spec.Replicas
		}
		logger.Info(
			"original-replicas annotation absent; assuming startCluster already ran",
			"currentReplicas", current)
		return nil
	}

	originalReplicas, err := strconv.ParseInt(originalReplicasStr, 10, 32)
	if err != nil {
		return fmt.Errorf("failed to parse original replica count: %w", err)
	}

	// Scale back up to original replicas
	sts.Spec.Replicas = ptr.To(int32(originalReplicas))

	// Remove the annotation
	delete(sts.Annotations, "neo4j.neo4j.com/original-replicas")

	if err := r.Update(ctx, sts); err != nil {
		return fmt.Errorf("failed to scale up StatefulSet: %w", err)
	}

	logger.Info("Cluster start initiated", "replicas", originalReplicas)
	return nil
}

func (r *Neo4jRestoreReconciler) waitForClusterReady(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Waiting for cluster to be ready", "cluster", cluster.Name)

	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for cluster to be ready")
		case <-ticker.C:
			// Check if all pods are ready
			pods := &corev1.PodList{}
			if err := r.List(ctx, pods, client.InNamespace(cluster.Namespace),
				client.MatchingLabels(resources.ServerPodSelector(cluster.Name))); err != nil {
				logger.Error(err, "Failed to list pods")
				continue
			}

			expectedReplicas := int(cluster.Spec.Topology.Servers)
			if len(pods.Items) != expectedReplicas {
				logger.Info("Waiting for all pods to be created", "current", len(pods.Items), "expected", expectedReplicas)
				continue
			}

			allReady := true
			for _, pod := range pods.Items {
				if !isPodReady(&pod) {
					allReady = false
					break
				}
			}

			if allReady {
				// Verify Neo4j connectivity
				neo4jClient, err := r.createNeo4jClient(ctx, cluster)
				if err != nil {
					logger.Info("Failed to create Neo4j client, retrying...")
					continue
				}

				// Test connectivity
				if err := neo4jClient.VerifyConnectivity(ctx); err != nil {
					// Close client immediately on error
					if closeErr := neo4jClient.Close(); closeErr != nil {
						logger.Error(closeErr, "failed to close Neo4j client")
					}
					logger.Info("Neo4j not ready yet, retrying...")
					continue
				}

				// Close client on success
				if err := neo4jClient.Close(); err != nil {
					logger.Error(err, "failed to close Neo4j client")
				}

				logger.Info("Cluster is ready")
				return nil
			}

			logger.Info("Waiting for pods to be ready")
		}
	}
}

func (r *Neo4jRestoreReconciler) runRestoreHooks(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, hooks *neo4jv1beta1.RestoreHooks, phase string) error {
	logger := log.FromContext(ctx)
	logger.Info("Running restore hooks", "restore", restore.Name, "phase", phase)

	// Execute Cypher statements if any
	if len(hooks.CypherStatements) > 0 {
		neo4jClient, err := r.createNeo4jClient(ctx, cluster)
		if err != nil {
			return fmt.Errorf("failed to create Neo4j client for hooks: %w", err)
		}
		defer func() {
			if err := neo4jClient.Close(); err != nil {
				logger.Error(err, "failed to close Neo4j client")
			}
		}()

		for _, statement := range hooks.CypherStatements {
			if err := neo4jClient.ExecuteCypher(ctx, restore.Spec.DatabaseName, statement); err != nil {
				return fmt.Errorf("failed to execute Cypher statement in %s hook: %w", phase, err)
			}
		}
	}

	// Execute job hooks if any
	if hooks.Job != nil {
		if err := r.runHookJob(ctx, restore, phase); err != nil {
			return fmt.Errorf("failed to execute job hook in %s: %w", phase, err)
		}
	}

	return nil
}

func (r *Neo4jRestoreReconciler) runHookJob(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, phase string) error {
	logger := log.FromContext(ctx)
	logger.Info("Running hook job", "restore", restore.Name, "phase", phase)

	// Get hook configuration based on phase
	var hookSpec *neo4jv1beta1.RestoreHookJob
	if phase == "pre" && restore.Spec.Options != nil && restore.Spec.Options.PreRestore != nil {
		hookSpec = restore.Spec.Options.PreRestore.Job
	} else if phase == "post" && restore.Spec.Options != nil && restore.Spec.Options.PostRestore != nil {
		hookSpec = restore.Spec.Options.PostRestore.Job
	}

	if hookSpec == nil {
		return nil // No hook job configured
	}

	// Create the job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-hook", restore.Name, phase),
			Namespace: restore.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-restore",
				"app.kubernetes.io/instance":  restore.Name,
				"app.kubernetes.io/component": phase + "-hook",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyNever,
					SecurityContext: hardenedRestorePodSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            "hook",
							Image:           hookSpec.Template.Container.Image,
							Command:         hookSpec.Template.Container.Command,
							Args:            hookSpec.Template.Container.Args,
							Env:             hookSpec.Template.Container.Env,
							SecurityContext: hardenedRestoreContainerSecurityContext(),
						},
					},
				},
			},
			BackoffLimit: hookSpec.Template.BackoffLimit,
		},
	}

	if job.Spec.BackoffLimit == nil {
		job.Spec.BackoffLimit = ptr.To(int32(3))
	}

	if err := r.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create hook job: %w", err)
	}

	// Determine timeout
	timeout := 30 * time.Minute // Default timeout
	if hookSpec.Timeout != "" {
		if duration, err := time.ParseDuration(hookSpec.Timeout); err == nil {
			timeout = duration
		}
	}

	// Wait for job completion
	timeoutChan := time.After(timeout)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("hook job timed out")
		case <-ticker.C:
			if err := r.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
				return fmt.Errorf("failed to get hook job status: %w", err)
			}

			if job.Status.Succeeded > 0 {
				logger.Info("Hook job completed successfully")
				return nil
			}

			if job.Status.Failed > 0 {
				return fmt.Errorf("hook job failed")
			}

			// Still running, continue waiting
		}
	}
}

func (r *Neo4jRestoreReconciler) getClusterRef(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) (*neo4jv1beta1.Neo4jEnterpriseCluster, error) {
	key := types.NamespacedName{Name: restore.Spec.ClusterRef, Namespace: restore.Namespace}

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, key, cluster); err == nil {
		return cluster, nil
	}

	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, key, standalone); err != nil {
		return nil, fmt.Errorf("target %q not found as Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone: %w",
			restore.Spec.ClusterRef, err)
	}
	return standaloneAsCluster(standalone), nil
}

// startClusterCypherRestore is the cluster-native restore path: skip the
// `neo4j-admin database restore` Job entirely (unsafe on clusters per the
// docs) and use Cypher against the live cluster instead.
//
// Decision matrix:
//   - Database EXISTS  → `CALL dbms.recreateDatabase($db, {seedURI: $uri})`
//     (preserves user/role privileges; no DROP needed; per-server atomic
//     swap from the seed chain).
//   - Database ABSENT  → `CREATE DATABASE $db OPTIONS { seedURI: $uri } WAIT`
//     (the modern OPTIONS syntax; CloudSeedProvider scans the directory
//     for the backup chain).
//
// In both forms the URI points at a DIRECTORY (with trailing slash)
// containing the chain — CloudSeedProvider applies the full + diffs in
// order. The `WAIT` clause + Neo4j's blocking semantics mean the call
// returns after the new state is online; we then mark the restore
// Completed.
//
// Sharded databases are NOT supported by this path — they require the
// `Neo4jShardedDatabase.spec.replaceExisting` flow (rule 63). The
// validator rejects sharded restores with an actionable error.
func (r *Neo4jRestoreReconciler) startClusterCypherRestore(
	ctx context.Context,
	restore *neo4jv1beta1.Neo4jRestore,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// If the asynchronous recreate was already issued in a prior reconcile
	// (e.g. the status update to Running failed after the recreate landed and
	// the annotation was stamped), re-running the setup here would re-issue
	// the recreate — wiping the partially-seeded database and restarting from
	// scratch, and re-spawning the proxy / re-emitting chain-parent warnings.
	// Hand straight off to the requeue-driven poll phase instead.
	if _, issued := restore.Annotations[AnnotationCypherRestoreIssued]; issued {
		return r.pollClusterRestoreOnline(ctx, restore, cluster)
	}

	// Resolve backupRef → storage + per-CR shared directory.
	storage, backupPath, err := ResolveBackupRef(ctx, r.Client, restore.Spec.Source.BackupRef, restore.Namespace)
	if err != nil {
		if stderrors.Is(err, ErrBackupNotReady) {
			logger.Info("Cluster Cypher restore waiting for backup to complete", "error", err.Error())
			r.updateRestoreStatus(ctx, restore, StatusPending, fmt.Sprintf("Waiting for backup to complete: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		// `storage` and `backupPath` are only used for type=backup. For
		// type=storage we read directly from spec.source.
		if restore.Spec.Source.Type != SourceTypeBackup {
			storage = neo4jv1beta1.StorageLocation{}
			backupPath = ""
		} else {
			logger.Error(err, "Failed to resolve backupRef")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to resolve backupRef: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}
	if restore.Spec.Source.Type == "storage" {
		if restore.Spec.Source.Storage == nil {
			r.updateRestoreStatus(ctx, restore, StatusFailed, "source.storage is required when type=storage")
			return ctrl.Result{}, fmt.Errorf("source.storage required for type=storage")
		}
		storage = *restore.Spec.Source.Storage
		backupPath = restore.Spec.Source.BackupPath
	}

	// Advisory: restoring via a FULL+DIFF chain PARENT seeds from its latest
	// full snapshot, not the latest chain state held by the differential
	// children (rule 78). Surface that so the user isn't silently surprised.
	if restore.Spec.Source.Type == SourceTypeBackup {
		r.warnIfChainParent(ctx, restore, restore.Spec.Source.BackupRef)
	}

	// Build the seedURI. Cloud-backed backups produce a directory URI
	// (s3://bucket/<base>/<cr-name>/) consumed by Neo4j's CloudSeedProvider.
	// PVC-backed backups produce an http:// URL pointing at the captured
	// `.backup` filename served by an in-cluster proxy (the same approach
	// used by the sharded PVC seedBackupRef path).
	var seedURI string
	switch storage.Type {
	case "s3", "gcs", "azure":
		seedURI, err = buildSeedURIFromBackupStorage(storage, backupPath)
		if err != nil {
			r.updateRestoreStatus(ctx, restore, StatusFailed, err.Error())
			return ctrl.Result{}, err
		}
		// Neo4j's CloudSeedProvider seeds a single database from the exact
		// `.backup` FILE — it does NOT scan a directory. Pointing it at the
		// per-CR directory makes it try to open the directory name as a file
		// ("Can't open seed file: …/<chain-root>").
		if restore.Spec.Source.Type == SourceTypeBackup {
			// type=backup: the operator knows the artifact filename from the
			// backup's status.history — append it (same as the PVC path).
			fname, ferr := r.latestSucceededArtifactFilename(ctx, restore.Spec.Source.BackupRef, restore.Namespace)
			if ferr != nil {
				r.updateRestoreStatus(ctx, restore, StatusFailed, ferr.Error())
				return ctrl.Result{}, ferr
			}
			seedURI = strings.TrimRight(seedURI, "/") + "/" + fname
		} else {
			// type=storage: the operator has no Neo4jBackup history to read,
			// so source.backupPath MUST be the exact `.backup` file path
			// (e.g. "<chain-root>/<dbname>-<timestamp>.backup"). Strip the
			// trailing slash buildSeedURIFromBackupStorage adds so the URI
			// stays a file; reject a bare directory with an actionable error.
			seedURI = strings.TrimRight(seedURI, "/")
			if !strings.HasSuffix(seedURI, ".backup") {
				msg := fmt.Sprintf("cluster restore with source.type=storage requires source.backupPath to be the exact .backup file (e.g. '<chain-root>/<dbname>-<timestamp>.backup'); got a non-file path resolving to %q. CloudSeedProvider cannot seed a single database from a directory.", seedURI)
				r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
				return ctrl.Result{}, fmt.Errorf("%s", msg)
			}
		}
		// Project cloud credentials onto cluster pods (envFrom) so the JVM's
		// SDK default credential chain can authenticate the seed fetch.
		if storage.Cloud != nil && storage.Cloud.CredentialsSecretRef != "" {
			autoInherited, credsErr := EnsureClusterHasSeedCreds(ctx, r.Client, cluster, storage.Cloud.CredentialsSecretRef)
			if credsErr != nil {
				logger.Error(credsErr, "Cluster missing seed credentials projection")
				r.updateRestoreStatus(ctx, restore, StatusFailed, credsErr.Error())
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			if autoInherited {
				logger.Info("Auto-inherited seed credentials onto cluster; waiting for rolling restart",
					"cluster", cluster.Name)
				r.updateRestoreStatus(ctx, restore, StatusPending,
					fmt.Sprintf("Auto-inherited seed credentials Secret %q onto cluster %q; waiting for rolling restart", storage.Cloud.CredentialsSecretRef, cluster.Name))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
		}
	case "pvc":
		// Cluster + PVC restore: spawn the in-cluster HTTP proxy in front
		// of the backup PVC, build a single-file seedURI against it. The
		// cluster's seed_from_uri_providers default (rule 74) includes
		// URLConnectionSeedProvider so http:// URIs are accepted.
		uri, result, perr := r.resolveClusterPVCRestoreURI(ctx, restore, storage, backupPath)
		if perr != nil {
			r.updateRestoreStatus(ctx, restore, StatusFailed, perr.Error())
			return ctrl.Result{}, perr
		}
		if uri == "" {
			// Proxy still rolling out or backup CR not yet ready — caller
			// already wrote the Pending status; just propagate the result.
			return result, nil
		}
		seedURI = uri
	default:
		err := fmt.Errorf("cluster restore does not support storage type %q (expected s3, gcs, azure, or pvc)", storage.Type)
		r.updateRestoreStatus(ctx, restore, StatusFailed, err.Error())
		return ctrl.Result{}, err
	}

	// Open a Bolt connection to the cluster.
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client for cluster Cypher restore")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Failed to connect to cluster: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer func() { _ = neo4jClient.Close() }()

	exists, err := neo4jClient.DatabaseExists(ctx, restore.Spec.DatabaseName)
	if err != nil {
		logger.Error(err, "Failed to check database existence")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("Database existence check failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, vErr := neo4j.GetImageVersion(imageTag)
	if vErr != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	r.updateRestoreStatus(ctx, restore, StatusRunning, fmt.Sprintf("Cluster Cypher restore in progress (seedURI=%s, exists=%v)", seedURI, exists))
	r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreStarted,
		fmt.Sprintf("Cluster Cypher restore: database %q (%s), seedURI=%s",
			restore.Spec.DatabaseName, ternaryString(exists, "recreate", "create"), seedURI))

	if exists {
		// `dbms.recreateDatabase` is ASYNCHRONOUS — it returns as soon as the
		// recreate is scheduled, long before the per-server seed-from-URI
		// finishes. We therefore must NOT block the reconcile worker on a
		// 5-minute online-wait here: MaxConcurrentReconciles is small, and a
		// blocking wait starves every other restore (a single un-seedable
		// restore would hold the worker for the full timeout). Instead, issue
		// the recreate exactly ONCE (guarded by the cypher-restore-issued
		// annotation — re-issuing would wipe the partially-seeded database and
		// restart the seed), then hand off to the requeue-driven poll phase
		// (checkRestoreProgress → pollClusterRestoreOnline).
		applied, recreateErr := neo4jClient.RecreateDatabaseWithSeedURI(ctx, version, restore.Spec.DatabaseName, seedURI)
		if recreateErr != nil {
			logger.Error(recreateErr, "dbms.recreateDatabase with seedURI failed")
			r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("recreateDatabase failed: %v", recreateErr))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, recreateErr
		}
		if !applied {
			// Version doesn't support recreate. CREATE DATABASE OPTIONS{seedURI}
			// only works on absent databases — for an existing one we'd need
			// DROP + CREATE. Surface as actionable failure.
			msg := fmt.Sprintf("Neo4j version %d.%d doesn't support dbms.recreateDatabase; DROP DATABASE %q manually and re-run the restore",
				version.Major, version.Minor, restore.Spec.DatabaseName)
			r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
			return ctrl.Result{}, fmt.Errorf("%s", msg)
		}
		// Mark the recreate issued (annotation = issue timestamp → poll
		// deadline) and move to Running. The next reconcile routes to
		// pollClusterRestoreOnline, which polls SHOW DATABASE per requeue
		// rather than holding the worker.
		if err := r.markCypherRestoreIssued(ctx, restore); err != nil {
			logger.Error(err, "Failed to mark cypher restore issued")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
		r.updateRestoreStatus(ctx, restore, StatusRunning,
			fmt.Sprintf("Database %q recreate issued; waiting for seed to converge online (seedURI=%s)", restore.Spec.DatabaseName, seedURI))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Database ABSENT: `CREATE DATABASE … OPTIONS{seedURI} WAIT` is
	// SYNCHRONOUS — it blocks until the database is online (or fails fast on a
	// bad/unreachable seed). It's a single atomic operation, so we keep it
	// inline and mark Completed directly on success.
	if createErr := neo4jClient.CreateDatabaseWithSeedURIOptions(ctx, restore.Spec.DatabaseName, seedURI, false); createErr != nil {
		logger.Error(createErr, "CREATE DATABASE OPTIONS{seedURI} failed")
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("CREATE DATABASE OPTIONS{seedURI} failed: %v", createErr))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, createErr
	}

	completion := metav1.Now()
	restore.Status.CompletionTime = &completion
	r.updateRestoreStatus(ctx, restore, StatusCompleted,
		fmt.Sprintf("Database %q restored via cluster Cypher path (seedURI=%s)", restore.Spec.DatabaseName, seedURI))
	r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreCompleted,
		fmt.Sprintf("Cluster Cypher restore completed for database %q", restore.Spec.DatabaseName))
	return ctrl.Result{}, nil
}

// markCypherRestoreIssued stamps the cypher-restore-issued annotation with the
// current RFC3339 timestamp via a conflict-retried metadata Update. The
// annotation guards against re-issuing the asynchronous recreate and anchors
// the online-wait deadline used by pollClusterRestoreOnline.
func (r *Neo4jRestoreReconciler) markCypherRestoreIssued(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	stamp := metav1.Now().UTC().Format(time.RFC3339)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jRestore{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(restore), latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = map[string]string{}
		}
		if _, ok := latest.Annotations[AnnotationCypherRestoreIssued]; ok {
			return nil // already stamped — preserve the original timestamp
		}
		latest.Annotations[AnnotationCypherRestoreIssued] = stamp
		return r.Update(ctx, latest)
	})
	if err != nil {
		return err
	}
	// Reflect the annotation onto the in-memory object so the caller's
	// subsequent status update / requeue sees it.
	if restore.Annotations == nil {
		restore.Annotations = map[string]string{}
	}
	if _, ok := restore.Annotations[AnnotationCypherRestoreIssued]; !ok {
		restore.Annotations[AnnotationCypherRestoreIssued] = stamp
	}
	return nil
}

// pollClusterRestoreOnline is the requeue-driven poll phase for the
// cluster-native Cypher restore. The asynchronous `dbms.recreateDatabase` was
// already issued (cypher-restore-issued annotation present); here we open a
// short-lived Bolt connection, run ONE SHOW DATABASE, and either:
//   - mark Completed once every allocation is online,
//   - mark Failed once the online-wait deadline (annotation timestamp +
//     cypherRestoreOnlineTimeout) has passed without convergence,
//   - otherwise requeue.
//
// Crucially this never blocks the worker for the full timeout — each reconcile
// does a single bounded SHOW DATABASE and returns, so other restores keep
// progressing under a small MaxConcurrentReconciles.
func (r *Neo4jRestoreReconciler) pollClusterRestoreOnline(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Derive the deadline from the issue timestamp. A malformed/missing stamp
	// falls back to "now" so a single extra requeue re-stamps via the wait.
	deadline := time.Now().Add(cypherRestoreOnlineTimeout)
	if raw, ok := restore.Annotations[AnnotationCypherRestoreIssued]; ok {
		if issuedAt, perr := time.Parse(time.RFC3339, raw); perr == nil {
			deadline = issuedAt.Add(cypherRestoreOnlineTimeout)
		}
	}
	expired := time.Now().After(deadline)

	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		// The cluster may be mid roll/unreachable transiently. Tolerate until
		// the deadline, then fail.
		if expired {
			r.updateRestoreStatus(ctx, restore, StatusFailed,
				fmt.Sprintf("Restore did not converge to online within %s: connect failed: %v", cypherRestoreOnlineTimeout, err))
			return ctrl.Result{}, nil
		}
		logger.V(1).Info("Poll: cluster not yet reachable, requeueing", "error", err.Error())
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}
	defer func() { _ = neo4jClient.Close() }()

	online, total, diag, stateErr := neo4jClient.DatabaseOnlineState(ctx, restore.Spec.DatabaseName)
	if stateErr == nil && total > 0 && online == total {
		completion := metav1.Now()
		restore.Status.CompletionTime = &completion
		r.updateRestoreStatus(ctx, restore, StatusCompleted,
			fmt.Sprintf("Database %q restored via cluster Cypher path (%d/%d allocations online)", restore.Spec.DatabaseName, online, total))
		r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreCompleted,
			fmt.Sprintf("Cluster Cypher restore completed for database %q", restore.Spec.DatabaseName))
		return ctrl.Result{}, nil
	}

	if expired {
		detail := diag
		if stateErr != nil {
			detail = stateErr.Error()
		}
		r.updateRestoreStatus(ctx, restore, StatusFailed,
			fmt.Sprintf("Restore did not converge to online within %s (%d/%d allocations online); last status: %s",
				cypherRestoreOnlineTimeout, online, total, detail))
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreFailed,
			fmt.Sprintf("Cluster Cypher restore for database %q did not converge online", restore.Spec.DatabaseName))
		return ctrl.Result{}, nil
	}

	logger.V(1).Info("Poll: database not yet online, requeueing",
		"database", restore.Spec.DatabaseName, "online", online, "total", total, "diag", diag)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// resolveClusterPVCRestoreURI spawns the in-cluster HTTP seed proxy for a
// PVC-backed cluster restore and returns the seedURI pointing at the
// captured `.backup` artifact filename.
//
// Returns:
//   - (uri, _, nil)            success — uri is ready to be passed to
//     dbms.recreateDatabase / CREATE DATABASE OPTIONS{seedURI}.
//   - ("", result, nil)        transient — proxy still rolling out OR the
//     backup CR's most-recent Succeeded run has no ArtifactFilename yet.
//     Caller routes to Pending+requeue via the embedded `result`.
//   - ("", _, err)             permanent failure — wrong storage type, no
//     backup ref, missing PVC name. Caller routes to Failed.
//
// Mirrors the sharded PVC seedBackupRef path (rule 71) but for a single
// `.backup` file rather than a per-shard map.
func (r *Neo4jRestoreReconciler) resolveClusterPVCRestoreURI(
	ctx context.Context,
	restore *neo4jv1beta1.Neo4jRestore,
	storage neo4jv1beta1.StorageLocation,
	backupsPath string,
) (string, ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if storage.Type != "pvc" {
		return "", ctrl.Result{}, fmt.Errorf("internal: resolveClusterPVCRestoreURI called with storage.type=%q", storage.Type)
	}
	if storage.PVC == nil || storage.PVC.Name == "" {
		return "", ctrl.Result{}, fmt.Errorf("PVC-backed cluster restore requires storage.pvc.name to be set")
	}

	// We need the captured ArtifactFilename for the most-recent Succeeded
	// run. type=backup paths can use the resolved BackupRun; type=storage
	// users supply the filename via spec.source.backupPath as a complete
	// path (handled below).
	var filename string
	switch restore.Spec.Source.Type {
	case SourceTypeBackup:
		backup := &neo4jv1beta1.Neo4jBackup{}
		if err := r.Get(ctx, types.NamespacedName{Name: restore.Spec.Source.BackupRef, Namespace: restore.Namespace}, backup); err != nil {
			return "", ctrl.Result{}, fmt.Errorf("PVC restore: re-fetch backup %q: %w", restore.Spec.Source.BackupRef, err)
		}
		for i := range backup.Status.History {
			if backup.Status.History[i].Status == "Succeeded" {
				filename = backup.Status.History[i].ArtifactFilename
				break
			}
		}
		if filename == "" {
			msg := fmt.Sprintf("Neo4jBackup %q's most-recent Succeeded run has no captured ArtifactFilename — re-run the backup with a recent operator version (Pod-log capture required for PVC-backed cluster restores). Alternatively, copy the .backup file to S3/GCS/Azure and restore via type=storage.",
				restore.Spec.Source.BackupRef)
			r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
			return "", ctrl.Result{}, fmt.Errorf("%s", msg)
		}
	case "storage":
		// User points us directly at the file via spec.source.backupPath.
		// The proxy serves under /<backupsPath> by convention, so we need
		// to separate dir from filename. If backupPath is a single file
		// path like "inventory-backup/inventory-2026-…backup", split on
		// the last slash.
		fullPath := restore.Spec.Source.BackupPath
		if fullPath == "" {
			return "", ctrl.Result{}, fmt.Errorf("PVC restore with type=storage requires source.backupPath to be set to the .backup file path under the PVC root")
		}
		if idx := strings.LastIndex(fullPath, "/"); idx >= 0 {
			backupsPath = fullPath[:idx]
			filename = fullPath[idx+1:]
		} else {
			filename = fullPath
		}
	default:
		return "", ctrl.Result{}, fmt.Errorf("PVC cluster restore not supported with source.type=%q", restore.Spec.Source.Type)
	}

	// Spawn (idempotent) the HTTP proxy in front of the backup PVC. The
	// Neo4jRestore CR is the owner so the proxy is GC'd when the restore
	// is deleted.
	proxyAvailable, err := ensurePVCSeedProxyResources(ctx, r.Client, r.Scheme, restore, restore.Name, storage.PVC.Name)
	if err != nil {
		return "", ctrl.Result{}, fmt.Errorf("ensure PVC seed proxy: %w", err)
	}
	if !proxyAvailable {
		logger.Info("PVC seed proxy not yet Ready; requeuing",
			"backupPVC", storage.PVC.Name)
		r.updateRestoreStatus(ctx, restore, StatusPending,
			"Waiting for backup-seed-proxy Deployment to become Ready")
		return "", ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	return pvcSeedProxyURL(restore.Name, restore.Namespace, backupsPath, filename), ctrl.Result{}, nil
}

// latestSucceededArtifactFilename returns the captured `.backup` artifact
// filename of a Neo4jBackup's most-recent Succeeded run (history is ordered
// most-recent-first). Both the cloud and PVC cluster-restore paths need this:
// Neo4j seeds a single database from the exact file, not a directory.
func (r *Neo4jRestoreReconciler) latestSucceededArtifactFilename(ctx context.Context, backupRef, namespace string) (string, error) {
	backup := &neo4jv1beta1.Neo4jBackup{}
	if err := r.Get(ctx, types.NamespacedName{Name: backupRef, Namespace: namespace}, backup); err != nil {
		return "", fmt.Errorf("re-fetch backup %q: %w", backupRef, err)
	}
	for i := range backup.Status.History {
		if backup.Status.History[i].Status == "Succeeded" {
			if fn := backup.Status.History[i].ArtifactFilename; fn != "" {
				return fn, nil
			}
		}
	}
	return "", fmt.Errorf("Neo4jBackup %q has no Succeeded run with a captured ArtifactFilename — re-run the backup with a recent operator version (Pod-log capture required for cluster restores), or copy the .backup file to storage and restore via type=storage with source.backupPath pointing at the file", backupRef)
}

// warnIfChainParent emits a Warning event when source.backupRef points at the
// PARENT of a mixed-cadence FULL+DIFF chain (rule 78) — i.e. other Neo4jBackup
// CRs declare spec.chainFromBackup == backupRef and have Succeeded runs.
//
// Restoring via the parent seeds from ITS latest artifact (a full snapshot),
// NOT the latest chain state held by the differential children — Neo4j applies
// a backup chain backward to the seed file, so seeding from the FULL omits the
// newer diffs. That's a legitimate "roll back to the last full" operation, but
// it's silent: a user who expects the latest state must instead reference the
// differential CR (whose latest artifact is the newest DIFF). This advisory
// turns that footgun into a visible signal. Purely informational — it never
// changes the restore behavior or fails the reconcile.
func (r *Neo4jRestoreReconciler) warnIfChainParent(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, backupRef string) {
	if backupRef == "" {
		return
	}
	list := &neo4jv1beta1.Neo4jBackupList{}
	if err := r.List(ctx, list, client.InNamespace(restore.Namespace)); err != nil {
		return // best-effort advisory; never block the restore on this
	}
	var children []string
	for i := range list.Items {
		child := &list.Items[i]
		if child.Spec.ChainFromBackup != backupRef {
			continue
		}
		for _, run := range child.Status.History {
			if run.Status == "Succeeded" {
				children = append(children, child.Name)
				break
			}
		}
	}
	if len(children) == 0 {
		return
	}
	sort.Strings(children)
	r.Recorder.Eventf(restore, corev1.EventTypeWarning, EventReasonRestoreFromChainParent,
		"source.backupRef %q is a FULL+DIFF chain parent; differential backups exist on [%s]. "+
			"This restore seeds from %q's latest full snapshot, NOT the latest chain state. "+
			"To restore the latest state, set source.backupRef to the differential CR instead.",
		backupRef, strings.Join(children, ", "), backupRef)
}

func ternaryString(cond bool, ifTrue, ifFalse string) string {
	if cond {
		return ifTrue
	}
	return ifFalse
}

// isRestoreTargetTrueCluster returns true when spec.clusterRef points at an
// actual Neo4jEnterpriseCluster (not a Neo4jEnterpriseStandalone). The
// cluster restore path uses Cypher (`dbms.recreateDatabase` or
// `CREATE DATABASE OPTIONS{seedURI}`) per the Neo4j cluster restore docs;
// standalone uses the Job + `neo4j-admin restore` path. We can't use
// `getClusterRef` directly because it transparently wraps a Standalone as
// a synthetic Cluster.
func (r *Neo4jRestoreReconciler) isRestoreTargetTrueCluster(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) (bool, *neo4jv1beta1.Neo4jEnterpriseCluster, error) {
	key := types.NamespacedName{Name: restore.Spec.ClusterRef, Namespace: restore.Namespace}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, key, cluster); err == nil {
		return true, cluster, nil
	} else if !errors.IsNotFound(err) {
		return false, nil, err
	}
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, key, standalone); err != nil {
		return false, nil, fmt.Errorf("target %q not found as Cluster or Standalone: %w", restore.Spec.ClusterRef, err)
	}
	return false, nil, nil
}

func (r *Neo4jRestoreReconciler) createNeo4jClient(_ context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	return neo4j.NewClientForEnterprise(cluster, r.Client, cluster.Spec.Auth.AdminSecret)
}

func (r *Neo4jRestoreReconciler) cleanupRestoreJobs(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	// Delete associated jobs
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList, client.InNamespace(restore.Namespace), client.MatchingLabels{
		"app.kubernetes.io/instance":  restore.Name,
		"app.kubernetes.io/component": "restore",
	}); err != nil {
		return err
	}

	for _, job := range jobList.Items {
		if err := r.Delete(ctx, &job); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *Neo4jRestoreReconciler) updateRestoreStatus(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, phase, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jRestore{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(restore), latest); err != nil {
			return err
		}
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		condStatus, condReason := PhaseToConditionStatus(phase)
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, condStatus, condReason, message)
		return r.Status().Update(ctx, latest)
	}
	err := retry.RetryOnConflict(retry.DefaultBackoff, update)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update restore status")
	}
}

func (r *Neo4jRestoreReconciler) updateRestoreStats(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, job *batchv1.Job) {
	// Update completion time
	if job.Status.CompletionTime != nil {
		restore.Status.CompletionTime = job.Status.CompletionTime
	}

	// Update statistics from job
	if len(job.Status.Conditions) > 0 {
		lastCondition := job.Status.Conditions[len(job.Status.Conditions)-1]
		restore.Status.Message = lastCondition.Message
	}

	if err := r.Status().Update(ctx, restore); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update restore stats")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jRestore{}).
		Owns(&batchv1.Job{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}

// validateNeo4jVersion validates that the target cluster uses Neo4j 5.26+ or 2025.01+
func (r *Neo4jRestoreReconciler) validateNeo4jVersion(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	if cluster.Spec.Image.Tag == "" {
		return fmt.Errorf("Neo4j image tag is not specified in cluster %s", cluster.Name)
	}

	return validation.ValidateNeo4jVersion(cluster.Spec.Image.Tag)
}

// Helper function to check if pod is ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}
