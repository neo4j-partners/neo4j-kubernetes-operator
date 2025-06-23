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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// Neo4jPluginReconciler reconciles a Neo4jPlugin object
type Neo4jPluginReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	RequeueAfter time.Duration
}

// PluginFinalizer is the finalizer for Neo4j plugin resources
const PluginFinalizer = "neo4j.neo4j.com/plugin-finalizer"

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins/finalizers,verbs=update

// Reconcile handles the reconciliation of Neo4jPlugin resources
func (r *Neo4jPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jPlugin instance
	plugin := &neo4jv1alpha1.Neo4jPlugin{}
	if err := r.Get(ctx, req.NamespacedName, plugin); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jPlugin resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jPlugin")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if plugin.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, plugin)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
		controllerutil.AddFinalizer(plugin, PluginFinalizer)
		if err := r.Update(ctx, plugin); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get target cluster
	cluster, err := r.getTargetCluster(ctx, plugin)
	if err != nil {
		logger.Error(err, "Failed to get target cluster")
		r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Target cluster not found: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to "Installing"
	r.updatePluginStatus(ctx, plugin, "Installing", "Installing plugin")

	// Install plugin
	if err := r.installPlugin(ctx, plugin, cluster); err != nil {
		logger.Error(err, "Failed to install plugin")
		r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Plugin installation failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Configure plugin
	if err := r.configurePlugin(ctx, plugin, cluster); err != nil {
		logger.Error(err, "Failed to configure plugin")
		r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Plugin configuration failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to "Ready"
	r.updatePluginStatus(ctx, plugin, "Ready", "Plugin installed and configured successfully")

	logger.Info("Plugin reconciliation completed")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jPluginReconciler) handleDeletion(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Uninstall plugin
	if err := r.uninstallPlugin(ctx, plugin); err != nil {
		logger.Error(err, "Failed to uninstall plugin")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
	if err := r.Update(ctx, plugin); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Plugin deleted successfully")
	return ctrl.Result{}, nil
}

func (r *Neo4jPluginReconciler) getTargetCluster(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      plugin.Spec.ClusterRef,
		Namespace: plugin.Namespace,
	}, cluster); err != nil {
		return nil, fmt.Errorf("failed to get cluster %s: %w", plugin.Spec.ClusterRef, err)
	}

	if cluster.Status.Phase != "Ready" {
		return nil, fmt.Errorf("cluster %s is not ready: %s", cluster.Name, cluster.Status.Phase)
	}

	return cluster, nil
}

func (r *Neo4jPluginReconciler) installPlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// Create Neo4j client
	neo4jClient, err := neo4jclient.NewClientForEnterprise(cluster, r.Client, "neo4j-admin-secret")
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer neo4jClient.Close()

	// Download plugin if needed
	if err := r.downloadPlugin(ctx, plugin); err != nil {
		return fmt.Errorf("failed to download plugin: %w", err)
	}

	// Install dependencies first
	for _, dep := range plugin.Spec.Dependencies {
		if err := r.installDependency(ctx, plugin, cluster, dep); err != nil {
			if !dep.Optional {
				return fmt.Errorf("failed to install required dependency %s: %w", dep.Name, err)
			}
			logger.Error(err, "Failed to install optional dependency", "dependency", dep.Name)
		}
	}

	// Install the plugin
	if err := r.performPluginInstallation(ctx, plugin, cluster, neo4jClient); err != nil {
		return fmt.Errorf("failed to perform plugin installation: %w", err)
	}

	logger.Info("Plugin installed successfully", "plugin", plugin.Spec.Name, "version", plugin.Spec.Version)
	return nil
}

func (r *Neo4jPluginReconciler) downloadPlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {

	if plugin.Spec.Source == nil {
		// Use default source (official Neo4j plugin repository)
		return r.downloadFromOfficialRepository(ctx, plugin)
	}

	switch plugin.Spec.Source.Type {
	case "official":
		return r.downloadFromOfficialRepository(ctx, plugin)
	case "community":
		return r.downloadFromCommunityRepository(ctx, plugin)
	case "custom":
		return r.downloadFromCustomRepository(ctx, plugin)
	case "url":
		return r.downloadFromURL(ctx, plugin)
	default:
		return fmt.Errorf("unsupported source type: %s", plugin.Spec.Source.Type)
	}
}

func (r *Neo4jPluginReconciler) downloadFromOfficialRepository(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)
	logger.Info("Downloading plugin from official repository", "plugin", plugin.Spec.Name)

	// Create download job to fetch plugin from official repository
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-download", plugin.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "download",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "downloader",
							Image:   "curlimages/curl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									# Download from official Neo4j plugin repository
									PLUGIN_URL="https://dist.neo4j.org/plugins/%s/%s/%s-%s.jar"
									curl -L -o /downloads/%s.jar "$PLUGIN_URL"
									if [ $? -eq 0 ]; then
										echo "Plugin downloaded successfully"
									else
										echo "Failed to download plugin"
										exit 1
									fi
								`, plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "downloads",
									MountPath: "/downloads",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "downloads",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			BackoffLimit: ptr.To(int32(3)),
		},
	}

	return r.Create(ctx, job)
}

func (r *Neo4jPluginReconciler) downloadFromCommunityRepository(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)
	logger.Info("Downloading plugin from community repository", "plugin", plugin.Spec.Name)

	// Create download job for community plugin
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-download", plugin.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "download",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "downloader",
							Image:   "curlimages/curl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									# Download from community repository (Maven Central or GitHub)
									PLUGIN_URL="https://repo1.maven.org/maven2/com/neo4j/%s/%s/%s-%s.jar"
									curl -L -o /downloads/%s.jar "$PLUGIN_URL" || {
										# Fallback to GitHub releases
										GITHUB_URL="https://github.com/neo4j-contrib/%s/releases/download/v%s/%s-%s.jar"
										curl -L -o /downloads/%s.jar "$GITHUB_URL"
									}
								`, plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name,
									plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "downloads",
									MountPath: "/downloads",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "downloads",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			BackoffLimit: ptr.To(int32(3)),
		},
	}

	return r.Create(ctx, job)
}

func (r *Neo4jPluginReconciler) downloadFromCustomRepository(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)
	logger.Info("Downloading plugin from custom repository", "plugin", plugin.Spec.Name)

	if plugin.Spec.Source == nil || plugin.Spec.Source.Registry == nil {
		return fmt.Errorf("custom registry configuration not provided")
	}

	// Create download job with custom repository credentials
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-download", plugin.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "download",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "downloader",
							Image:   "curlimages/curl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									# Download from custom repository
									REPO_URL="%s"
									PLUGIN_PATH="%s/%s/%s-%s.jar"
									if [ -n "$REPO_USERNAME" ] && [ -n "$REPO_PASSWORD" ]; then
										curl -L -u "$REPO_USERNAME:$REPO_PASSWORD" -o /downloads/%s.jar "$REPO_URL/$PLUGIN_PATH"
									else
										curl -L -o /downloads/%s.jar "$REPO_URL/$PLUGIN_PATH"
									fi
								`, plugin.Spec.Source.Registry.URL, plugin.Spec.Name, plugin.Spec.Version,
									plugin.Spec.Name, plugin.Spec.Version, plugin.Spec.Name, plugin.Spec.Name),
							},
							Env: r.buildRegistryEnvVars(plugin.Spec.Source.Registry),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "downloads",
									MountPath: "/downloads",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "downloads",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			BackoffLimit: ptr.To(int32(3)),
		},
	}

	return r.Create(ctx, job)
}

func (r *Neo4jPluginReconciler) downloadFromURL(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)
	logger.Info("Downloading plugin from URL", "plugin", plugin.Spec.Name, "url", plugin.Spec.Source.URL)

	// Create download job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-download", plugin.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "download",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "downloader",
							Image:   "curlimages/curl:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									# Download from direct URL
									curl -L -o /downloads/%s.jar "%s"

									# Verify checksum if provided
									if [ -n "%s" ]; then
										echo "%s  /downloads/%s.jar" > /tmp/checksum
										sha256sum -c /tmp/checksum || {
											echo "Checksum verification failed"
											exit 1
										}
									fi
								`, plugin.Spec.Name, plugin.Spec.Source.URL, plugin.Spec.Source.Checksum,
									plugin.Spec.Source.Checksum, plugin.Spec.Name),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "downloads",
									MountPath: "/downloads",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "downloads",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			BackoffLimit: ptr.To(int32(3)),
		},
	}

	if err := r.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create download job: %w", err)
	}

	// Wait for download completion
	return r.waitForJobCompletion(ctx, job)
}

func (r *Neo4jPluginReconciler) installDependency(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, _ *neo4jv1alpha1.Neo4jEnterpriseCluster, dep neo4jv1alpha1.PluginDependency) error {
	logger := log.FromContext(ctx)

	logger.Info("Installing plugin dependency", "dependency", dep.Name, "constraint", dep.VersionConstraint)

	// Create dependency plugin resource
	depPlugin := &neo4jv1alpha1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-dep", plugin.Name, dep.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "dependency",
				"neo4j.plugin/parent":         plugin.Name,
			},
		},
		Spec: neo4jv1alpha1.Neo4jPluginSpec{
			ClusterRef: plugin.Spec.ClusterRef,
			Name:       dep.Name,
			Version:    dep.VersionConstraint,
			Enabled:    true,
		},
	}

	// Create or update dependency plugin
	if err := r.Create(ctx, depPlugin); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create dependency plugin: %w", err)
	}

	// Wait for dependency to be ready
	return r.waitForPluginReady(ctx, depPlugin)
}

func (r *Neo4jPluginReconciler) waitForPluginReady(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// Wait for plugin to be in Ready state with timeout
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for plugin %s to be ready", plugin.Name)
		case <-ticker.C:
			current := &neo4jv1alpha1.Neo4jPlugin{}
			if err := r.Get(ctx, types.NamespacedName{Name: plugin.Name, Namespace: plugin.Namespace}, current); err != nil {
				logger.Error(err, "Failed to get plugin status")
				continue
			}

			if current.Status.Phase == "Ready" {
				logger.Info("Plugin is ready", "plugin", plugin.Name)
				return nil
			}

			if current.Status.Phase == "Failed" {
				return fmt.Errorf("plugin %s failed to install: %s", plugin.Name, current.Status.Message)
			}

			logger.Info("Waiting for plugin to be ready", "plugin", plugin.Name, "phase", current.Status.Phase)
		}
	}
}

func (r *Neo4jPluginReconciler) performPluginInstallation(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, neo4jClient *neo4jclient.Client) error {
	logger := log.FromContext(ctx)

	// Copy plugin to Neo4j plugins directory
	if err := r.copyPluginToCluster(ctx, plugin, cluster); err != nil {
		return fmt.Errorf("failed to copy plugin to cluster: %w", err)
	}

	// Restart Neo4j instances to load plugin
	if err := r.restartNeo4jInstances(ctx, cluster); err != nil {
		return fmt.Errorf("failed to restart Neo4j instances: %w", err)
	}

	// Wait for cluster to be ready after restart
	if err := r.waitForClusterReady(ctx, cluster); err != nil {
		return fmt.Errorf("cluster not ready after plugin installation: %w", err)
	}

	// Verify plugin is loaded
	if err := r.verifyPluginLoaded(ctx, neo4jClient, plugin); err != nil {
		return fmt.Errorf("plugin verification failed: %w", err)
	}

	logger.Info("Plugin installation completed successfully")
	return nil
}

func (r *Neo4jPluginReconciler) copyPluginToCluster(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Copying plugin to cluster", "plugin", plugin.Spec.Name)

	// Create an init container job to copy plugin to the plugins directory
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-install", plugin.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "install",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "installer",
							Image:   "busybox:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									# Copy plugin to Neo4j plugins directory
									mkdir -p /neo4j/plugins
									cp /downloads/%s.jar /neo4j/plugins/

									# Set proper permissions
									chmod 644 /neo4j/plugins/%s.jar

									echo "Plugin installed successfully"
								`, plugin.Spec.Name, plugin.Spec.Name),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "downloads",
									MountPath: "/downloads",
									ReadOnly:  true,
								},
								{
									Name:      "neo4j-plugins",
									MountPath: "/neo4j/plugins",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "downloads",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "neo4j-plugins",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: fmt.Sprintf("%s-plugins", cluster.Name),
								},
							},
						},
					},
				},
			},
			BackoffLimit: ptr.To(int32(3)),
		},
	}

	return r.Create(ctx, job)
}

func (r *Neo4jPluginReconciler) restartNeo4jInstances(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Restarting Neo4j instances to load plugin")

	// Get the StatefulSet for the cluster
	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      cluster.Name,
		Namespace: cluster.Namespace,
	}

	if err := r.Get(ctx, stsKey, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Trigger rolling restart by updating an annotation
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = make(map[string]string)
	}
	sts.Spec.Template.Annotations["neo4j.neo4j.com/plugin-restart"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, sts); err != nil {
		return fmt.Errorf("failed to trigger StatefulSet restart: %w", err)
	}

	logger.Info("Triggered rolling restart of Neo4j instances")
	return nil
}

func (r *Neo4jPluginReconciler) waitForClusterReady(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Waiting for cluster to be ready after plugin installation")

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
				"app.kubernetes.io/name":     "neo4j",
				"app.kubernetes.io/instance": cluster.Name,
			}); err != nil {
				logger.Error(err, "Failed to list pods")
				continue
			}

			expectedReplicas := int(cluster.Spec.Topology.Primaries + cluster.Spec.Topology.Secondaries)
			if len(pods.Items) != expectedReplicas {
				logger.Info("Waiting for all pods to be created", "current", len(pods.Items), "expected", expectedReplicas)
				continue
			}

			allReady := true
			for _, pod := range pods.Items {
				// Check if pod is ready
				podReady := pod.Status.Phase == corev1.PodRunning
				if podReady {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady {
							podReady = condition.Status == corev1.ConditionTrue
							break
						}
					}
				}
				if !podReady {
					allReady = false
					break
				}
			}

			if allReady {
				logger.Info("Cluster is ready")
				return nil
			}

			logger.Info("Waiting for pods to be ready")
		}
	}
}

func (r *Neo4jPluginReconciler) verifyPluginLoaded(ctx context.Context, neo4jClient *neo4jclient.Client, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	// Verify plugin is loaded by calling CALL dbms.components() or similar
	components, err := neo4jClient.GetLoadedComponents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get loaded components: %w", err)
	}

	for _, component := range components {
		if component.Name == plugin.Spec.Name {
			return nil
		}
	}

	return fmt.Errorf("plugin %s not found in loaded components", plugin.Spec.Name)
}

func (r *Neo4jPluginReconciler) configurePlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	if len(plugin.Spec.Config) == 0 {
		logger.Info("No plugin configuration provided")
		return nil
	}

	// Create Neo4j client
	neo4jClient, err := neo4jclient.NewClientForEnterprise(cluster, r.Client, "neo4j-admin-secret")
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer neo4jClient.Close()

	// Configure plugin settings
	for key, value := range plugin.Spec.Config {
		if err := neo4jClient.SetConfiguration(ctx, key, value); err != nil {
			return fmt.Errorf("failed to set configuration %s=%s: %w", key, value, err)
		}
	}

	// Apply security configuration if specified
	if plugin.Spec.Security != nil {
		if err := r.applySecurityConfiguration(ctx, neo4jClient, plugin); err != nil {
			return fmt.Errorf("failed to apply security configuration: %w", err)
		}
	}

	logger.Info("Plugin configuration applied successfully")
	return nil
}

func (r *Neo4jPluginReconciler) applySecurityConfiguration(ctx context.Context, neo4jClient *neo4jclient.Client, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	security := plugin.Spec.Security

	// Configure allowed procedures
	if len(security.AllowedProcedures) > 0 {
		if err := neo4jClient.SetAllowedProcedures(ctx, security.AllowedProcedures); err != nil {
			return fmt.Errorf("failed to set allowed procedures: %w", err)
		}
	}

	// Configure denied procedures
	if len(security.DeniedProcedures) > 0 {
		if err := neo4jClient.SetDeniedProcedures(ctx, security.DeniedProcedures); err != nil {
			return fmt.Errorf("failed to set denied procedures: %w", err)
		}
	}

	// Configure sandbox mode
	if security.Sandbox {
		if err := neo4jClient.EnableSandboxMode(ctx, security.Sandbox); err != nil {
			return fmt.Errorf("failed to enable sandbox mode: %w", err)
		}
	}

	return nil
}

func (r *Neo4jPluginReconciler) uninstallPlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// Get target cluster
	cluster, err := r.getTargetCluster(ctx, plugin)
	if err != nil {
		// If cluster is not found, consider plugin already uninstalled
		if errors.IsNotFound(err) {
			logger.Info("Target cluster not found, considering plugin uninstalled")
			return nil
		}
		return fmt.Errorf("failed to get target cluster: %w", err)
	}

	// Remove plugin from cluster
	if err := r.removePluginFromCluster(ctx, plugin, cluster); err != nil {
		return fmt.Errorf("failed to remove plugin from cluster: %w", err)
	}

	// Clean up plugin dependencies
	if err := r.cleanupDependencies(ctx, plugin); err != nil {
		logger.Error(err, "Failed to cleanup dependencies")
	}

	logger.Info("Plugin uninstalled successfully")
	return nil
}

func (r *Neo4jPluginReconciler) removePluginFromCluster(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Removing plugin from cluster", "plugin", plugin.Spec.Name)

	// Create a Job to remove the plugin from the cluster
	removeJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-remove-plugin-%s", cluster.Name, plugin.Spec.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  cluster.Name,
				"app.kubernetes.io/component": "plugin-removal",
				"neo4j.plugin/name":           plugin.Spec.Name,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "plugin-remover",
							Image:   "busybox:latest",
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									echo "Removing plugin %s from Neo4j cluster %s"
									# Remove plugin jar file
									rm -f /plugins/%s*.jar
									echo "Plugin removal completed"
								`, plugin.Spec.Name, cluster.Name, plugin.Spec.Name),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "plugins",
									MountPath: "/plugins",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "plugins",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: cluster.Name + "-plugins",
								},
							},
						},
					},
				},
			},
		},
	}

	if err := r.Create(ctx, removeJob); err != nil {
		return fmt.Errorf("failed to create plugin removal job: %w", err)
	}

	// Wait for job completion
	return r.waitForJobCompletion(ctx, removeJob)
}

func (r *Neo4jPluginReconciler) cleanupDependencies(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	// Clean up dependency plugins created for this plugin
	dependencyPlugins := &neo4jv1alpha1.Neo4jPluginList{}
	if err := r.List(ctx, dependencyPlugins, client.InNamespace(plugin.Namespace), client.MatchingLabels{
		"neo4j.plugin/parent": plugin.Name,
	}); err != nil {
		return fmt.Errorf("failed to list dependency plugins: %w", err)
	}

	for _, depPlugin := range dependencyPlugins.Items {
		if err := r.Delete(ctx, &depPlugin); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete dependency plugin %s: %w", depPlugin.Name, err)
		}
	}

	return nil
}

func (r *Neo4jPluginReconciler) updatePluginStatus(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, phase, message string) {
	plugin.Status.Phase = phase
	plugin.Status.Message = message
	plugin.Status.ObservedGeneration = plugin.Generation

	// Add or update condition
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             phase,
		Message:            message,
	}

	if phase == "Failed" || phase == "Installing" {
		condition.Status = metav1.ConditionFalse
	}

	// Update or append condition
	for i, cond := range plugin.Status.Conditions {
		if cond.Type == condition.Type {
			plugin.Status.Conditions[i] = condition
			goto updateStatus
		}
	}
	plugin.Status.Conditions = append(plugin.Status.Conditions, condition)

updateStatus:
	if err := r.Status().Update(ctx, plugin); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update plugin status")
	}
}

// SetupWithManager configures the controller with the manager
func (r *Neo4jPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jPlugin{}).
		Complete(r)
}

// Helper functions

func (r *Neo4jPluginReconciler) buildRegistryEnvVars(registry *neo4jv1alpha1.PluginRegistry) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	if registry.AuthSecret != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REPO_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: registry.AuthSecret,
					},
					Key: "username",
				},
			},
		})

		envVars = append(envVars, corev1.EnvVar{
			Name: "REPO_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: registry.AuthSecret,
					},
					Key: "password",
				},
			},
		})
	}

	return envVars
}

func (r *Neo4jPluginReconciler) waitForJobCompletion(ctx context.Context, job *batchv1.Job) error {
	logger := log.FromContext(ctx)
	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("job completion timeout")
		case <-ticker.C:
			if err := r.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			if job.Status.Succeeded > 0 {
				logger.Info("Job completed successfully")
				return nil
			}

			if job.Status.Failed > 0 {
				return fmt.Errorf("job failed")
			}

			// Still running, continue waiting
		}
	}
}
