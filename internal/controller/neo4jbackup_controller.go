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
	"strconv"
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

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
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
//+kubebuilder:rbac:groups="",resources=pods/exec,verbs=create;get
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get

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

	// Ensure backup ServiceAccount exists (and carries workload-identity annotations).
	if err := r.ensureBackupServiceAccount(ctx, backup); err != nil {
		logger.Error(err, "Failed to ensure backup ServiceAccount")
		return ctrl.Result{}, err
	}

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
	logger := log.FromContext(ctx)

	backupCmd, err := r.buildBackupCommand(backup, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build backup command: %w", err)
	}
	logger.Info("Running backup command", "cmd", backupCmd)

	image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	backoffLimit := int32(3)

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
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: backupServiceAccountName,
					Containers: []corev1.Container{
						{
							Name:         "backup",
							Image:        image,
							Command:      []string{"/bin/sh"},
							Args:         []string{"-c", backupCmd},
							Env:          r.buildCloudEnvVars(backup),
							VolumeMounts: r.buildVolumeMounts(backup),
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

func (r *Neo4jBackupReconciler) createBackupCronJob(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*batchv1.CronJob, error) {
	cronJobName := backup.Name + "-backup-cron"

	backupCmd, err := r.buildBackupCommand(backup, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build backup command: %w", err)
	}

	image := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	backoffLimit := int32(3)

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: backup.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cronJob, func() error {
		cronJob.Labels = map[string]string{
			"app.kubernetes.io/name":       "neo4j-backup",
			"app.kubernetes.io/instance":   backup.Name,
			"app.kubernetes.io/component":  "backup-cron",
			"app.kubernetes.io/managed-by": "neo4j-operator",
		}
		cronJob.Spec.Schedule = backup.Spec.Schedule
		cronJob.Spec.JobTemplate = batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{
				BackoffLimit: &backoffLimit,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy:      corev1.RestartPolicyNever,
						ServiceAccountName: backupServiceAccountName,
						Containers: []corev1.Container{
							{
								Name:         "backup",
								Image:        image,
								Command:      []string{"/bin/sh"},
								Args:         []string{"-c", backupCmd},
								Env:          r.buildCloudEnvVars(backup),
								VolumeMounts: r.buildVolumeMounts(backup),
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

func (r *Neo4jBackupReconciler) buildBackupCommand(backup *neo4jv1alpha1.Neo4jBackup, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (string, error) {
	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Validate version-gated flags individually.
	if backup.Spec.Options != nil {
		if backup.Spec.Options.ParallelDownload && !version.SupportsParallelDownload() {
			return "", fmt.Errorf("--parallel-download requires CalVer 2025.11+ (image: %s)", cluster.Spec.Image.Tag)
		}
		if backup.Spec.Options.RemoteAddressResolution && !version.SupportsRemoteAddressResolution() {
			return "", fmt.Errorf("--remote-address-resolution requires CalVer 2025.09+ (image: %s)", cluster.Spec.Image.Tag)
		}
		if backup.Spec.Options.SkipRecovery && !version.SupportsSkipRecovery() {
			return "", fmt.Errorf("--skip-recovery requires CalVer 2025.11+ (image: %s)", cluster.Spec.Image.Tag)
		}
		if backup.Spec.Options.PreferDiffAsParent && !version.SupportsPreferDiffAsParent() {
			return "", fmt.Errorf("--prefer-diff-as-parent requires CalVer 2025.04+ (image: %s)", cluster.Spec.Image.Tag)
		}
	}

	toPath := r.buildToPath(backup)
	fromAddresses := resources.BuildBackupFromAddresses(cluster)
	allDatabases := backup.Spec.Target.Kind == "Cluster"
	dbName := ""
	if !allDatabases {
		dbName = backup.Spec.Target.Name
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
		}
		if backup.Spec.Options.PreferDiffAsParent {
			cmd += " --prefer-diff-as-parent"
		}
		if backup.Spec.Options.RemoteAddressResolution {
			cmd += " --remote-address-resolution=true"
		}
		if backup.Spec.Options.ParallelDownload {
			cmd += " --parallel-download=true"
		}
		if backup.Spec.Options.SkipRecovery {
			cmd += " --skip-recovery=true"
		}
		for _, arg := range backup.Spec.Options.AdditionalArgs {
			cmd += " " + arg
		}
	}

	if backup.Spec.Storage.Type == "pvc" {
		cmd = fmt.Sprintf("mkdir -p %s && %s", toPath, cmd)
	}

	return cmd, nil
}

// buildToPath returns the --to-path value: a cloud URI for cloud storage or a
// timestamped local directory for PVC storage.
func (r *Neo4jBackupReconciler) buildToPath(backup *neo4jv1alpha1.Neo4jBackup) string {
	st := backup.Spec.Storage
	p := st.Path
	if p == "" {
		p = "backups"
	}
	switch st.Type {
	case "s3":
		return fmt.Sprintf("s3://%s/%s/", st.Bucket, p)
	case "gcs":
		return fmt.Sprintf("gs://%s/%s/", st.Bucket, p)
	case "azure":
		return fmt.Sprintf("azb://%s/%s/", st.Bucket, p)
	default: // pvc
		backupName := fmt.Sprintf("%s-%s", backup.Name, time.Now().Format("20060102-150405"))
		return fmt.Sprintf("/backup/%s", backupName)
	}
}

// cloudBlockForBackup returns the CloudBlock from whichever spec field is populated.
func cloudBlockForBackup(backup *neo4jv1alpha1.Neo4jBackup) *neo4jv1alpha1.CloudBlock {
	if backup.Spec.Storage.Cloud != nil {
		return backup.Spec.Storage.Cloud
	}
	return backup.Spec.Cloud
}

// buildCloudEnvVars injects cloud provider credentials from a Kubernetes Secret
// into the backup job container as environment variables.
// When CredentialsSecretRef is empty the function returns nil, which means the
// Job relies on ambient cloud identity (IRSA, GKE Workload Identity, etc.).
func (r *Neo4jBackupReconciler) buildCloudEnvVars(backup *neo4jv1alpha1.Neo4jBackup) []corev1.EnvVar {
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
			{Name: "AWS_DEFAULT_REGION", ValueFrom: fromSecret("AWS_DEFAULT_REGION")},
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

func (r *Neo4jBackupReconciler) buildVolumeMounts(backup *neo4jv1alpha1.Neo4jBackup) []corev1.VolumeMount {
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

	return mounts
}

func (r *Neo4jBackupReconciler) buildVolumes(backup *neo4jv1alpha1.Neo4jBackup) []corev1.Volume {
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

	return volumes
}

func (r *Neo4jBackupReconciler) getTargetCluster(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
	targetNamespace := backup.Spec.Target.Namespace
	if targetNamespace == "" {
		targetNamespace = backup.Namespace
	}

	// For Kind=Database the Name is the database name; use ClusterRef for the cluster.
	clusterName := backup.Spec.Target.Name
	if backup.Spec.Target.Kind == "Database" {
		if backup.Spec.Target.ClusterRef == "" {
			return nil, fmt.Errorf("clusterRef must be set when backup target Kind is Database")
		}
		clusterName = backup.Spec.Target.ClusterRef
	}

	// Try Neo4jEnterpriseCluster first.
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: targetNamespace}, cluster); err == nil {
		return cluster, nil
	}

	// Fall back to Neo4jEnterpriseStandalone.
	standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: targetNamespace}, standalone); err != nil {
		return nil, fmt.Errorf("target %q not found as Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone in namespace %q", clusterName, targetNamespace)
	}
	return standaloneAsCluster(standalone), nil
}

func (r *Neo4jBackupReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	return cluster.Status.Phase == "Ready"
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

	cleanupJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cleanupJobName,
			Namespace: backup.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j-backup",
				"app.kubernetes.io/instance":   backup.Name,
				"app.kubernetes.io/component":  "cleanup",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "backup-cleanup",
							Image:   "alpine:latest",
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
func buildRetentionScript(policy *neo4jv1alpha1.RetentionPolicy) string {
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
    find "$BACKUP_DIR" -maxdepth 1 -mindepth 1 -type d | \
        sort | \
        head -n "$TO_DELETE" | \
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
	logger := log.FromContext(ctx)

	stats := &neo4jv1alpha1.BackupStats{}
	if job.Status.StartTime != nil && job.Status.CompletionTime != nil {
		duration := job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
		stats.Duration = duration.Round(time.Second).String()
	}
	// Size, Throughput, FileCount are intentionally omitted:
	// they require parsing neo4j-admin stdout from Job pod logs (future enhancement).

	update := func() error {
		latest := &neo4jv1alpha1.Neo4jBackup{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
			return err
		}
		latest.Status.Stats = stats

		run := neo4jv1alpha1.BackupRun{
			Status: "Succeeded",
			Stats:  stats,
		}
		if job.Status.StartTime != nil {
			run.StartTime = *job.Status.StartTime
		}
		run.CompletionTime = job.Status.CompletionTime

		latest.Status.History = append([]neo4jv1alpha1.BackupRun{run}, latest.Status.History...)
		if len(latest.Status.History) > 10 {
			latest.Status.History = latest.Status.History[:10]
		}

		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		logger.Error(err, "Failed to update backup stats")
	}
}

// validateNeo4jVersion validates that the target cluster uses Neo4j 5.26+ or 2025.01+
func (r *Neo4jBackupReconciler) validateNeo4jVersion(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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
func (r *Neo4jBackupReconciler) ensureBackupServiceAccount(ctx context.Context, backup *neo4jv1alpha1.Neo4jBackup) error {
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
		For(&neo4jv1alpha1.Neo4jBackup{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
