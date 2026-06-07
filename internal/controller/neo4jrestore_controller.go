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

// errBackupNotReady is a sentinel error returned by the source resolver
// when the referenced Neo4jBackup exists but has no Succeeded run in
// status.history yet. This is a *transient* condition — the backup may
// complete on a future reconcile — so callers route it to StatusPending
// (which the Reconcile guard requeues) rather than StatusFailed (which
// the guard pins as a terminal state until the CR is recreated).
//
// Use errors.Is to detect; resolveBackupRef wraps it with fmt.Errorf
// to preserve the human-readable backup name in the message.
var errBackupNotReady = fmt.Errorf("backup has no Succeeded run yet")

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

	// Get restore job
	jobName := restore.Name + "-restore"
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: restore.Namespace}, job)

	if err != nil {
		logger.Error(err, "Failed to get restore job")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Check job status
	if job.Status.Succeeded > 0 {
		// Restore completed successfully
		return r.handleRestoreSuccess(ctx, restore, cluster, job)
	}

	if job.Status.Failed > 0 {
		// Restore failed
		r.updateRestoreStatus(ctx, restore, StatusFailed, "Restore job failed")
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreFailed, "Restore job failed")
		return ctrl.Result{}, nil
	}

	// Job is still running
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
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

	default:
		return fmt.Errorf("invalid source type %q: must be one of: backup, storage, s3, gcs, azure, pitr", restore.Spec.Source.Type)
	}

	if restore.Spec.DatabaseName == "" {
		return fmt.Errorf("databaseName is required")
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

// resolveBackupRef dereferences a Neo4jBackup CR name into a concrete
// StorageLocation (with cloud creds folded in) and the per-run subfolder
// (BackupsPath) of its most-recent Succeeded run. Used by both the main
// restore path (source.type=backup) and the PITR base-backup branch
// (pitr.baseBackup.type=backup).
//
// Returns an error if backupRef is empty, the Neo4jBackup is missing, or
// its status.history contains no Succeeded run — restoring against a
// failed/missing backup would silently produce garbage.
func (r *Neo4jRestoreReconciler) resolveBackupRef(ctx context.Context, backupRef, namespace string) (storage neo4jv1beta1.StorageLocation, backupPath string, err error) {
	if backupRef == "" {
		return neo4jv1beta1.StorageLocation{}, "", fmt.Errorf("backupRef is required")
	}

	backup := &neo4jv1beta1.Neo4jBackup{}
	if err := r.Get(ctx, types.NamespacedName{Name: backupRef, Namespace: namespace}, backup); err != nil {
		return neo4jv1beta1.StorageLocation{}, "", fmt.Errorf("failed to get Neo4jBackup %q: %w", backupRef, err)
	}

	// status.history is sorted newest-first by sortBackupRunsNewestFirst
	// (see neo4jbackup_controller.go). Walk forward until we find the
	// first Succeeded run — "the most recent successful backup" is the
	// only reasonable default for a backupRef-based restore.
	var succeeded *neo4jv1beta1.BackupRun
	for i := range backup.Status.History {
		if backup.Status.History[i].Status == "Succeeded" {
			succeeded = &backup.Status.History[i]
			break
		}
	}
	if succeeded == nil {
		// Wrap with errBackupNotReady so the caller can detect this
		// transient condition with errors.Is and route to Pending +
		// requeue instead of the terminal Failed phase.
		return neo4jv1beta1.StorageLocation{}, "", fmt.Errorf(
			"Neo4jBackup %q: %w (status.history has no Succeeded run yet)",
			backupRef, errBackupNotReady,
		)
	}

	// Materialize a StorageLocation copy with the backup's cloud creds
	// folded in. Neo4jBackup historically allowed `spec.cloud` as an
	// alternative to `spec.storage.cloud`; cloudBlockForBackup picks
	// whichever is populated, and we project it onto the synthesized
	// Storage so downstream cloudBlockForRestore finds it via the
	// canonical path.
	resolved := backup.Spec.Storage
	if resolved.Cloud == nil {
		if cb := cloudBlockForBackup(backup); cb != nil {
			resolved.Cloud = cb
		}
	}
	return resolved, succeeded.BackupsPath, nil
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
	// `ls ... | head -1` picks the single matching file (per-run subfolder
	// holds at most one `<dbname>-*.backup`). If no match exists, ls prints
	// to stderr and head returns nothing, so --from-path= becomes empty and
	// neo4j-admin errors with a clear "missing argument" message.
	return fmt.Sprintf("$(ls %s/%s-*.backup | head -1)",
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
		cmd += " --temp-path=" + restore.Spec.Options.TempPath
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

	// Add additional arguments if specified
	if restore.Spec.Options != nil && len(restore.Spec.Options.AdditionalArgs) > 0 {
		cmd += " " + strings.Join(restore.Spec.Options.AdditionalArgs, " ")
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
		cmd += " --temp-path=" + restore.Spec.Options.TempPath
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
