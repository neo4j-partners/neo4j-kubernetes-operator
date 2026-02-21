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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
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

// Neo4jRestoreReconciler reconciles a Neo4jRestore object
type Neo4jRestoreReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
}

func hardenedRestorePodSecurityContext() *corev1.PodSecurityContext {
	uid := int64(7474)
	return &corev1.PodSecurityContext{
		RunAsUser:    ptr.To(uid),
		RunAsGroup:   ptr.To(uid),
		FSGroup:      ptr.To(uid),
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func hardenedRestoreContainerSecurityContext() *corev1.SecurityContext {
	uid := int64(7474)
	return &corev1.SecurityContext{
		RunAsUser:                ptr.To(uid),
		RunAsGroup:               ptr.To(uid),
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
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
	restore := &neo4jv1alpha1.Neo4jRestore{}
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
	targetCluster, err := r.getTargetCluster(ctx, restore)
	if err != nil {
		logger.Error(err, "Failed to get target cluster")
		r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Failed to get target cluster: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Validate Neo4j version compatibility (5.26+ or 2025.01+)
	if err := r.validateNeo4jVersion(targetCluster); err != nil {
		logger.Error(err, "Neo4j version validation failed")
		r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Neo4j version not supported: %v", err))
		return ctrl.Result{}, err
	}

	// Check if restore is already completed
	if restore.Status.Phase == StatusCompleted && restore.Status.ObservedGeneration == restore.Generation {
		logger.Info("Restore already completed")
		return ctrl.Result{}, nil
	}

	// Check if restore is running
	if restore.Status.Phase == "Running" {
		return r.checkRestoreProgress(ctx, restore, targetCluster)
	}

	// Start restore process
	return r.startRestore(ctx, restore, targetCluster)
}

func (r *Neo4jRestoreReconciler) handleDeletion(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(restore, RestoreFinalizer) {
		return ctrl.Result{}, nil
	}

	// Clean up restore jobs
	if err := r.cleanupRestoreJobs(ctx, restore); err != nil {
		logger.Error(err, "Failed to cleanup restore jobs")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(restore, RestoreFinalizer)
	return ctrl.Result{}, r.Update(ctx, restore)
}

func (r *Neo4jRestoreReconciler) startRestore(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Set start time
	now := metav1.Now()
	restore.Status.StartTime = &now
	restore.Status.ObservedGeneration = restore.Generation

	// Validate restore request
	if err := r.validateRestore(ctx, restore); err != nil {
		logger.Error(err, "Restore validation failed")
		r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Validation failed: %v", err))
		return ctrl.Result{}, err
	}

	// Check if database exists and handle accordingly
	if !restore.Spec.Force {
		if err := r.checkDatabaseExists(ctx, restore, cluster); err != nil {
			logger.Error(err, "Database existence check failed")
			r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Database check failed: %v", err))
			return ctrl.Result{}, err
		}
	}

	// Stop cluster if required
	if restore.Spec.StopCluster {
		if err := r.stopCluster(ctx, cluster); err != nil {
			logger.Error(err, "Failed to stop cluster")
			r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Failed to stop cluster: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Run pre-restore hooks
	if restore.Spec.Options != nil && restore.Spec.Options.PreRestore != nil {
		if err := r.runRestoreHooks(ctx, restore, cluster, restore.Spec.Options.PreRestore, "pre-restore"); err != nil {
			logger.Error(err, "Pre-restore hooks failed")
			r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Pre-restore hooks failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Create restore job
	job, err := r.createRestoreJob(ctx, restore, cluster)
	if err != nil {
		logger.Error(err, "Failed to create restore job")
		r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Failed to create restore job: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status
	r.updateRestoreStatus(ctx, restore, "Running", fmt.Sprintf("Restore job %s created", job.Name))
	r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreStarted, fmt.Sprintf("Restore job %s started", job.Name))

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jRestoreReconciler) checkRestoreProgress(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
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
		r.updateRestoreStatus(ctx, restore, "Failed", "Restore job failed")
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreFailed, "Restore job failed")
		return ctrl.Result{}, nil
	}

	// Job is still running
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jRestoreReconciler) handleRestoreSuccess(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, job *batchv1.Job) (ctrl.Result, error) {
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
			r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Failed to start cluster after restore: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}

		// Wait for cluster to be ready
		if err := r.waitForClusterReady(ctx, cluster); err != nil {
			logger.Error(err, "Cluster not ready after restore")
			r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Cluster not ready after restore: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Run post-restore hooks
	if restore.Spec.Options != nil && restore.Spec.Options.PostRestore != nil {
		if err := r.runRestoreHooks(ctx, restore, cluster, restore.Spec.Options.PostRestore, "post-restore"); err != nil {
			logger.Error(err, "Post-restore hooks failed")
			r.updateRestoreStatus(ctx, restore, "Failed", fmt.Sprintf("Post-restore hooks failed: %v", err))
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
	r.updateRestoreStatus(ctx, restore, "Completed", "Restore completed successfully")
	r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreCompleted, "Restore completed successfully")

	return ctrl.Result{}, nil
}

// createOrStartDatabase registers the restored database with Neo4j.
// If the database already exists (overwrite restore) it starts it; otherwise it creates it.
func (r *Neo4jRestoreReconciler) createOrStartDatabase(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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

func (r *Neo4jRestoreReconciler) validateRestore(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) error {
	// Validate source
	switch restore.Spec.Source.Type {
	case SourceTypeBackup:
		if restore.Spec.Source.BackupRef == "" {
			return fmt.Errorf("backupRef is required when source type is 'backup'")
		}
		backup := &neo4jv1alpha1.Neo4jBackup{}
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

func (r *Neo4jRestoreReconciler) checkDatabaseExists(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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

func (r *Neo4jRestoreReconciler) createRestoreJob(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*batchv1.Job, error) {
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
			BackoffLimit: func(i int32) *int32 { return &i }(1), // Restore should not retry
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyNever,
					SecurityContext: hardenedRestorePodSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            "neo4j-restore",
							Image:           fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
							SecurityContext: hardenedRestoreContainerSecurityContext(),
							Command:         []string{"/bin/sh"},
							Args:            []string{"-c", restoreCmd},
							Env: []corev1.EnvVar{
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
							},
							VolumeMounts: r.buildRestoreVolumeMounts(restore),
						},
					},
					Volumes: r.buildRestoreVolumes(restore),
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

func (r *Neo4jRestoreReconciler) buildRestoreCommand(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (string, error) {
	var backupPath string

	// Determine backup path based on source type
	switch restore.Spec.Source.Type {
	case "backup":
		// Get backup resource to determine path
		backup := &neo4jv1alpha1.Neo4jBackup{}
		backupKey := types.NamespacedName{Name: restore.Spec.Source.BackupRef, Namespace: restore.Namespace}
		if err := r.Get(ctx, backupKey, backup); err != nil {
			return "", fmt.Errorf("failed to get backup %s: %w", restore.Spec.Source.BackupRef, err)
		}
		backupPath = fmt.Sprintf("/backup/%s", restore.Spec.Source.BackupRef)
	case "storage", SourceTypeS3, SourceTypeGCS, "azure":
		backupPath = restore.Spec.Source.BackupPath
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

	// Add point-in-time restore if specified
	if restore.Spec.Source.PointInTime != nil {
		t := restore.Spec.Source.PointInTime.Time.UTC()
		cmd += fmt.Sprintf(` --restore-until="%s"`, t.Format("2006-01-02 15:04:05"))
	}

	// Add additional arguments if specified
	if restore.Spec.Options != nil && len(restore.Spec.Options.AdditionalArgs) > 0 {
		for _, arg := range restore.Spec.Options.AdditionalArgs {
			cmd += " " + arg
		}
	}

	return cmd, nil
}

// buildPITRRestoreCommand builds a Point-in-Time Recovery restore command.
// PITR in Neo4j is implemented via the --restore-until flag on neo4j-admin database restore;
// there is no separate log-replay step.
func (r *Neo4jRestoreReconciler) buildPITRRestoreCommand(_ context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (string, error) {
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
			backupPath = pitrConfig.BaseBackup.BackupPath
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

func (r *Neo4jRestoreReconciler) buildRestoreVolumeMounts(restore *neo4jv1alpha1.Neo4jRestore) []corev1.VolumeMount {
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

	return mounts
}

func (r *Neo4jRestoreReconciler) buildRestoreVolumes(restore *neo4jv1alpha1.Neo4jRestore) []corev1.Volume {
	var dataVolume corev1.Volume
	if restore.Spec.StopCluster {
		// For offline restore, write directly to the first server pod's data PVC so the
		// restored store is available when the StatefulSet is scaled back up.
		dataPVCName := fmt.Sprintf("data-%s-server-0", restore.Spec.TargetCluster)
		dataVolume = corev1.Volume{
			Name: "neo4j-data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: dataPVCName,
				},
			},
		}
	} else {
		dataVolume = corev1.Volume{
			Name: "neo4j-data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
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
			volumes = append(volumes, corev1.Volume{
				Name: "backup-storage",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
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

	return volumes
}

func (r *Neo4jRestoreReconciler) stopCluster(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Stopping cluster for restore", "cluster", cluster.Name)

	// Get the StatefulSet for the cluster (named {cluster-name}-server in server-based architecture)
	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      cluster.Name + "-server",
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
			if err := r.List(ctx, pods, client.InNamespace(cluster.Namespace), client.MatchingLabels{
				"app.kubernetes.io/instance": cluster.Name + "-server",
			}); err != nil {
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

func (r *Neo4jRestoreReconciler) startCluster(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting cluster after restore", "cluster", cluster.Name)

	// Get the StatefulSet for the cluster (named {cluster-name}-server in server-based architecture)
	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      cluster.Name + "-server",
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

func (r *Neo4jRestoreReconciler) waitForClusterReady(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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
			if err := r.List(ctx, pods, client.InNamespace(cluster.Namespace), client.MatchingLabels{
				"app.kubernetes.io/instance": cluster.Name + "-server",
			}); err != nil {
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

func (r *Neo4jRestoreReconciler) runRestoreHooks(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, hooks *neo4jv1alpha1.RestoreHooks, phase string) error {
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

func (r *Neo4jRestoreReconciler) runHookJob(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, phase string) error {
	logger := log.FromContext(ctx)
	logger.Info("Running hook job", "restore", restore.Name, "phase", phase)

	// Get hook configuration based on phase
	var hookSpec *neo4jv1alpha1.RestoreHookJob
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
							Env:             convertEnvVars(hookSpec.Template.Container.Env),
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

// convertEnvVars converts custom EnvVar to corev1.EnvVar
func convertEnvVars(envVars []neo4jv1alpha1.EnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, len(envVars))
	for i, env := range envVars {
		result[i] = corev1.EnvVar{
			Name:  env.Name,
			Value: env.Value,
		}
		if env.ValueFrom != nil {
			result[i].ValueFrom = &corev1.EnvVarSource{}
			if env.ValueFrom.SecretKeyRef != nil {
				result[i].ValueFrom.SecretKeyRef = &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: env.ValueFrom.SecretKeyRef.Name,
					},
					Key: env.ValueFrom.SecretKeyRef.Key,
				}
			}
			if env.ValueFrom.ConfigMapKeyRef != nil {
				result[i].ValueFrom.ConfigMapKeyRef = &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: env.ValueFrom.ConfigMapKeyRef.Name,
					},
					Key: env.ValueFrom.ConfigMapKeyRef.Key,
				}
			}
		}
	}
	return result
}

func (r *Neo4jRestoreReconciler) getTargetCluster(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
	key := types.NamespacedName{Name: restore.Spec.TargetCluster, Namespace: restore.Namespace}

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, key, cluster); err == nil {
		return cluster, nil
	}

	standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, key, standalone); err != nil {
		return nil, fmt.Errorf("target %q not found as Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone: %w",
			restore.Spec.TargetCluster, err)
	}
	return standaloneAsCluster(standalone), nil
}

func (r *Neo4jRestoreReconciler) createNeo4jClient(_ context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	return neo4j.NewClientForEnterprise(cluster, r.Client, cluster.Spec.Auth.AdminSecret)
}

func (r *Neo4jRestoreReconciler) cleanupRestoreJobs(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore) error {
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

func (r *Neo4jRestoreReconciler) updateRestoreStatus(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, phase, message string) {
	update := func() error {
		latest := &neo4jv1alpha1.Neo4jRestore{}
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

func (r *Neo4jRestoreReconciler) updateRestoreStats(ctx context.Context, restore *neo4jv1alpha1.Neo4jRestore, job *batchv1.Job) {
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
		For(&neo4jv1alpha1.Neo4jRestore{}).
		Owns(&batchv1.Job{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}

// validateNeo4jVersion validates that the target cluster uses Neo4j 5.26+ or 2025.01+
func (r *Neo4jRestoreReconciler) validateNeo4jVersion(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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
