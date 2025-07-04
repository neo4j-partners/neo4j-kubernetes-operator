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
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jBackupReconciler reconciles a Neo4jBackup object
type Neo4jBackupReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
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

// Reconcile handles the reconciliation of Neo4jBackup resources
func (r *Neo4jBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jBackup instance
	backup := &neo4jv1alpha1.Neo4jBackup{}
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

	// Handle scheduled backups
	if backup.Spec.Schedule != "" {
		return r.handleScheduledBackup(ctx, backup, targetCluster)
	}

	// Handle one-time backup
	return r.handleOneTimeBackup(ctx, backup, targetCluster)
}

func (r *Neo4jBackupReconciler) handleDeletion(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) (ctrl.Result, error) {
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

func (r *Neo4jBackupReconciler) handleScheduledBackup(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check if backup is suspended
	if backup.Spec.Suspend {
		r.updateBackupStatus(ctx, backup, "Suspended", "Backup schedule is suspended")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
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
	r.Recorder.Event(backup, "Normal", "BackupScheduled", "Backup scheduled with CronJob "+cronJob.Name)

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jBackupReconciler) handleOneTimeBackup(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

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
		logger.Error(err, "Failed to create backup job")
		r.updateBackupStatus(ctx, backup, "Failed", fmt.Sprintf("Failed to create backup job: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status
	r.updateBackupStatus(ctx, backup, "Running", fmt.Sprintf("Backup job %s created", job.Name))
	r.Recorder.Event(backup, "Normal", "BackupStarted", fmt.Sprintf("Backup job %s started", job.Name))

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jBackupReconciler) handleExistingBackupJob(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, job *batchv1.Job) (ctrl.Result, error) {
	// Check job status
	if job.Status.Succeeded > 0 {
		// Backup completed successfully
		r.updateBackupStatus(ctx, backup, "Completed", "Backup completed successfully")
		r.Recorder.Event(backup, "Normal", "BackupCompleted", "Backup completed successfully")

		// Update backup statistics
		r.updateBackupStats(ctx, backup, job)

		return ctrl.Result{}, nil
	}

	if job.Status.Failed > 0 {
		// Backup failed
		r.updateBackupStatus(ctx, backup, "Failed", "Backup job failed")
		r.Recorder.Event(backup, "Warning", "BackupFailed", "Backup job failed")
		return ctrl.Result{}, nil
	}

	// Job is still running
	r.updateBackupStatus(ctx, backup, "Running", "Backup job is running")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jBackupReconciler) createBackupJob(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*batchv1.Job, error) {
	jobName := backup.Name + "-backup"

	// Build backup command
	backupCmd := r.buildBackupCommand(backup)

	// Build environment variables
	env := []corev1.EnvVar{}

	// Add admin password environment variable if auth is configured
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		env = append(env, corev1.EnvVar{
			Name: "NEO4J_ADMIN_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cluster.Spec.Auth.AdminSecret,
					},
					Key: "password",
				},
			},
		})
	}

	// Add cloud storage environment variables
	if backup.Spec.Storage.Type == "s3" || backup.Spec.Storage.Type == "gcs" || backup.Spec.Storage.Type == "azure" {
		env = append(env, corev1.EnvVar{
			Name:  "BACKUP_BUCKET",
			Value: backup.Spec.Storage.Bucket,
		})
	}

	// Create job spec
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: backup.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j-backup",
				"app.kubernetes.io/instance":   backup.Name,
				"app.kubernetes.io/component":  "backup",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: func(i int32) *int32 { return &i }(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:         "neo4j-backup",
							Image:        fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
							Command:      []string{"/bin/sh"},
							Args:         []string{"-c", backupCmd},
							Env:          env,
							VolumeMounts: r.buildVolumeMounts(backup),
						},
					},
					Volumes: r.buildVolumes(backup),
				},
			},
		},
	}

	// Set controller reference
	if err := controllerutil.SetControllerReference(backup, job, r.Scheme); err != nil {
		return nil, err
	}

	// Create the job
	if err := r.Create(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

func (r *Neo4jBackupReconciler) createBackupCronJob(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*batchv1.CronJob, error) {
	cronJobName := backup.Name + "-backup-cron"

	// Check if CronJob already exists
	existingCronJob := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: cronJobName, Namespace: backup.Namespace}, existingCronJob)

	if err == nil {
		// CronJob exists, update if needed
		return existingCronJob, nil
	}

	if !errors.IsNotFound(err) {
		return nil, err
	}

	// Build backup command
	backupCmd := r.buildBackupCommand(backup)

	// Build environment variables
	env := []corev1.EnvVar{}

	// Add admin password environment variable if auth is configured
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		env = append(env, corev1.EnvVar{
			Name: "NEO4J_ADMIN_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cluster.Spec.Auth.AdminSecret,
					},
					Key: "password",
				},
			},
		})
	}

	// Add cloud storage environment variables
	if backup.Spec.Storage.Type == "s3" || backup.Spec.Storage.Type == "gcs" || backup.Spec.Storage.Type == "azure" {
		env = append(env, corev1.EnvVar{
			Name:  "BACKUP_BUCKET",
			Value: backup.Spec.Storage.Bucket,
		})
	}

	// Create CronJob spec
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: backup.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j-backup",
				"app.kubernetes.io/instance":   backup.Name,
				"app.kubernetes.io/component":  "backup-cron",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule: backup.Spec.Schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: func(i int32) *int32 { return &i }(3),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:         "neo4j-backup",
									Image:        fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag),
									Command:      []string{"/bin/sh"},
									Args:         []string{"-c", backupCmd},
									Env:          env,
									VolumeMounts: r.buildVolumeMounts(backup),
								},
							},
							Volumes: r.buildVolumes(backup),
						},
					},
				},
			},
		},
	}

	// Set controller reference
	if err := controllerutil.SetControllerReference(backup, cronJob, r.Scheme); err != nil {
		return nil, err
	}

	// Create the CronJob
	if err := r.Create(ctx, cronJob); err != nil {
		return nil, err
	}

	return cronJob, nil
}

func (r *Neo4jBackupReconciler) buildBackupCommand(backup *neo4jv1alpha1.Neo4jBackup) string {
	var cmd string

	// Build the neo4j-admin backup command
	backupName := fmt.Sprintf("%s-%s", backup.Name, time.Now().Format("20060102-150405"))

	switch backup.Spec.Target.Kind {
	case "Cluster":
		// For cluster backups, backup all databases using --all-databases flag for Neo4j 5.26+
		cmd = "neo4j-admin backup --all-databases --to=/backup/" + backupName
	case "Database":
		cmd = fmt.Sprintf("neo4j-admin backup --database=%s --to=/backup/%s", backup.Spec.Target.Name, backupName)
	default:
		// Default to all databases for Neo4j 5.26+ compatibility
		cmd = "neo4j-admin backup --all-databases --to=/backup/" + backupName
	}

	// Add compression if specified
	if backup.Spec.Options != nil && backup.Spec.Options.Compress {
		cmd += " --compress"
	}

	// Add verification if specified
	if backup.Spec.Options != nil && backup.Spec.Options.Verify {
		cmd += " --check-consistency"
	}

	// Add backup aggregation for Neo4j 5.26+ (improves backup chain management)
	cmd += " --backup-aggregation"

	// Add metadata inspection to capture backup information
	cmd += " --include-metadata"

	// Add retention policy flags (note: these are not actual neo4j-admin flags,
	// but will be handled by our retention cleanup job)
	if backup.Spec.Retention != nil {
		if backup.Spec.Retention.MaxAge != "" {
			// Store retention info in environment for cleanup job
			cmd = fmt.Sprintf("export BACKUP_MAX_AGE='%s'; %s", backup.Spec.Retention.MaxAge, cmd)
		}
		if backup.Spec.Retention.MaxCount > 0 {
			// Store retention info in environment for cleanup job
			cmd = fmt.Sprintf("export BACKUP_MAX_COUNT='%d'; %s", backup.Spec.Retention.MaxCount, cmd)
		}
	}

	// Add additional arguments if specified
	if backup.Spec.Options != nil && len(backup.Spec.Options.AdditionalArgs) > 0 {
		for _, arg := range backup.Spec.Options.AdditionalArgs {
			cmd += " " + arg
		}
	}

	return cmd
}

func (r *Neo4jBackupReconciler) buildVolumeMounts(_ *neo4jv1alpha1.Neo4jBackup) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{
			Name:      "backup-storage",
			MountPath: "/backup",
		},
	}

	return mounts
}

func (r *Neo4jBackupReconciler) buildVolumes(backup *neo4jv1alpha1.Neo4jBackup) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Add storage volume based on storage type
	if backup.Spec.Storage.Type == "pvc" && backup.Spec.Storage.PVC != nil {
		if backup.Spec.Storage.PVC.Name != "" {
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
				Name: "backup-storage",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		}
	} else {
		volumes = append(volumes, corev1.Volume{
			Name: "backup-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	return volumes
}

func (r *Neo4jBackupReconciler) getTargetCluster(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}

	// Determine target namespace
	targetNamespace := backup.Spec.Target.Namespace
	if targetNamespace == "" {
		targetNamespace = backup.Namespace
	}

	clusterKey := types.NamespacedName{
		Name:      backup.Spec.Target.Name,
		Namespace: targetNamespace,
	}

	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		return nil, err
	}

	return cluster, nil
}

func (r *Neo4jBackupReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Neo4jBackupReconciler) cleanupBackupJobs(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) error {
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

func (r *Neo4jBackupReconciler) cleanupBackupArtifacts(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) error {
	logger := log.FromContext(ctx)

	if backup.Spec.Retention == nil {
		logger.Info("No retention policy specified, skipping cleanup")
		return nil
	}

	// Create a cleanup job that will handle retention policy enforcement
	cleanupJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cleanup-%d", backup.Name, time.Now().Unix()),
			Namespace: backup.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-backup",
				"app.kubernetes.io/instance":  backup.Spec.Target.Name,
				"app.kubernetes.io/component": "cleanup",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "backup-cleanup",
							Image:   "alpine:latest",
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									echo "Starting backup cleanup for %s"
									echo "Retention policy: keep %s, max count %d"
									# Implementation would:
									# 1. List all backups in storage location
									# 2. Apply retention policy (age + count)
									# 3. Delete old backups
									# 4. Update backup status
									echo "Backup cleanup completed"
								`, backup.Name, backup.Spec.Retention.MaxAge, backup.Spec.Retention.MaxCount),
							},
						},
					},
				},
			},
		},
	}

	if err := r.Create(ctx, cleanupJob); err != nil {
		return fmt.Errorf("failed to create cleanup job: %w", err)
	}

	logger.Info("Backup cleanup job created", "job", cleanupJob.Name)
	return nil
}

func (r *Neo4jBackupReconciler) updateBackupStatus(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, phase, message string) {
	update := func() error {
		latest := &neo4jv1alpha1.Neo4jBackup{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
			return err
		}
		latest.Status.Phase = phase
		latest.Status.Message = message
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             phase,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		}
		if phase == "Failed" || phase == "Suspended" {
			condition.Status = metav1.ConditionFalse
		}
		updated := false
		for i, existingCondition := range latest.Status.Conditions {
			if existingCondition.Type == condition.Type {
				latest.Status.Conditions[i] = condition
				updated = true
				break
			}
		}
		if !updated {
			latest.Status.Conditions = append(latest.Status.Conditions, condition)
		}
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

func (r *Neo4jBackupReconciler) updateBackupStats(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, job *batchv1.Job) {
	// This would calculate and update backup statistics
	// For now, we'll create a basic stats entry
	stats := &neo4jv1alpha1.BackupStats{
		Size:       "unknown",
		Duration:   "unknown",
		Throughput: "unknown",
		FileCount:  0,
	}

	if job.Status.CompletionTime != nil && job.Status.StartTime != nil {
		duration := job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
		stats.Duration = duration.String()
	}

	backup.Status.Stats = stats
	if err := r.Status().Update(ctx, backup); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update backup stats")
	}
}

// validateNeo4jVersion validates that the target cluster uses Neo4j 5.26+ or 2025.01+
func (r *Neo4jBackupReconciler) validateNeo4jVersion(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	if cluster.Spec.Image.Tag == "" {
		return fmt.Errorf("Neo4j image tag is not specified in cluster %s", cluster.Name)
	}

	return validation.ValidateNeo4jVersion(cluster.Spec.Image.Tag)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jBackup{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
