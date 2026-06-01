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

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/validation"
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

	// Build restore command
	restoreCmd, err := r.buildRestoreCommand(ctx, restore, cluster)
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
								if cloudEnvs := r.buildRestoreCloudEnvVars(restore); cloudEnvs != nil {
									envs = append(envs, cloudEnvs...)
								}
								return envs
							}(),
							VolumeMounts: r.buildRestoreVolumeMounts(restore),
						},
					},
					Volumes: r.buildRestoreVolumes(ctx, restore),
				},
			},
		},
	}

	// Set controller reference
	if err := controllerutil.SetControllerReference(restore, job, r.Scheme); err != nil {
		return nil, err
	}

	// Create the job
	if err := r.Create(ctx, job); err != nil {
		return nil, err
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

func (r *Neo4jRestoreReconciler) buildRestoreCommand(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	var backupPath string

	// Determine backup path based on source type
	switch restore.Spec.Source.Type {
	case "backup":
		// Get backup resource to determine path
		backup := &neo4jv1beta1.Neo4jBackup{}
		backupKey := types.NamespacedName{Name: restore.Spec.Source.BackupRef, Namespace: restore.Namespace}
		if err := r.Get(ctx, backupKey, backup); err != nil {
			return "", fmt.Errorf("failed to get backup %s: %w", restore.Spec.Source.BackupRef, err)
		}
		backupPath = fmt.Sprintf("/backup/%s", restore.Spec.Source.BackupRef)
	case "storage", SourceTypeS3, SourceTypeGCS, "azure":
		backupPath = r.buildRestoreFromPath(restore)
	case "pitr":
		return r.buildPITRRestoreCommand(ctx, restore, cluster)
	}

	// Extract Neo4j version from cluster image
	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Build the neo4j-admin restore command with correct Neo4j 5.26+ syntax
	cmd := neo4j.GetRestoreCommand(version, restore.Spec.DatabaseName, backupPath)

	// Add --overwrite-destination flag if force is specified
	if restore.Spec.Force {
		cmd += " --overwrite-destination=true"
	}

	// Add --temp-path when the user has configured staging storage.
	// TempStorage (PVC reference) takes priority, then explicit TempPath.
	if restore.Spec.Options != nil && restore.Spec.Options.TempStorage != nil {
		cmd += " --temp-path=/tmp/neo4j-staging"
	} else if restore.Spec.Options != nil && restore.Spec.Options.TempPath != "" {
		cmd += " --temp-path=" + restore.Spec.Options.TempPath
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
func (r *Neo4jRestoreReconciler) buildPITRRestoreCommand(_ context.Context, restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	pitrConfig := restore.Spec.Source.PITR
	if pitrConfig == nil {
		return "", fmt.Errorf("PITR configuration is required for PITR restore")
	}

	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Determine backup source path from base backup
	var backupPath string
	if pitrConfig.BaseBackup != nil {
		switch pitrConfig.BaseBackup.Type {
		case "backup":
			backupPath = fmt.Sprintf("/backup/%s", pitrConfig.BaseBackup.BackupRef)
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

	cmd := neo4j.GetRestoreCommand(version, restore.Spec.DatabaseName, backupPath)

	if restore.Spec.Force {
		cmd += " --overwrite-destination=true"
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

	// Add storage volume based on source type
	if restore.Spec.Source.Type == "backup" {
		// For backup references, we'd need to determine the storage from the backup
		volumes = append(volumes, corev1.Volume{
			Name: "backup-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if restore.Spec.Source.Storage != nil {
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

	// Restore original replica count from annotation
	originalReplicasStr, exists := sts.Annotations["neo4j.neo4j.com/original-replicas"]
	if !exists {
		return fmt.Errorf("original replica count not found in annotations")
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
