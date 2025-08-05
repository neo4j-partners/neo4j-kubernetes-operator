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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jEnterpriseStandaloneReconciler reconciles a Neo4jEnterpriseStandalone object
type Neo4jEnterpriseStandaloneReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	RequeueAfter     time.Duration
	Validator        *validation.StandaloneValidator
	ConfigMapManager *ConfigMapManager
}

const (
	// StandaloneFinalizer is the finalizer for Neo4j enterprise standalone deployments
	StandaloneFinalizer = "neo4j.neo4j.com/standalone-finalizer"
)

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones/finalizers,verbs=update
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch

func (r *Neo4jEnterpriseStandaloneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jEnterpriseStandalone instance
	standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, req.NamespacedName, standalone); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jEnterpriseStandalone resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jEnterpriseStandalone")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if standalone.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, standalone)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(standalone, StandaloneFinalizer) {
		controllerutil.AddFinalizer(standalone, StandaloneFinalizer)
		if err := r.Update(ctx, standalone); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate the standalone configuration
	if validationErrs := r.Validator.ValidateCreate(standalone); len(validationErrs) > 0 {
		logger.Error(fmt.Errorf("validation failed: %v", validationErrs), "Validation failed")
		r.Recorder.Event(standalone, corev1.EventTypeWarning, "ValidationFailed",
			fmt.Sprintf("Validation failed: %v", validationErrs))

		// Update status to reflect validation failure
		standalone.Status.Phase = "ValidationFailed"
		standalone.Status.Message = fmt.Sprintf("Validation failed: %v", validationErrs)
		standalone.Status.Ready = false
		if err := r.Status().Update(ctx, standalone); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Reconcile the standalone deployment
	result, err := r.reconcileStandalone(ctx, standalone)
	if err != nil {
		logger.Error(err, "Failed to reconcile standalone deployment")
		r.Recorder.Event(standalone, corev1.EventTypeWarning, "ReconcileFailed",
			fmt.Sprintf("Failed to reconcile: %v", err))

		// Update status to reflect failure
		standalone.Status.Phase = "Failed"
		standalone.Status.Message = fmt.Sprintf("Reconciliation failed: %v", err)
		standalone.Status.Ready = false
		if statusErr := r.Status().Update(ctx, standalone); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	return result, nil
}

// handleDeletion handles the deletion of a standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) handleDeletion(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Cleanup resources
	if err := r.cleanupResources(ctx, standalone); err != nil {
		logger.Error(err, "Failed to cleanup resources")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(standalone, StandaloneFinalizer)
	if err := r.Update(ctx, standalone); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully deleted Neo4jEnterpriseStandalone")
	return ctrl.Result{}, nil
}

// reconcileStandalone reconciles the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStandalone(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Reconcile TLS Certificate (if TLS is enabled)
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		if err := r.reconcileTLSCertificate(ctx, standalone); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile TLS Certificate: %w", err)
		}
	}

	// Reconcile ConfigMap (always needed for config)
	if err := r.reconcileConfigMap(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile ConfigMap: %w", err)
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile Service: %w", err)
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile StatefulSet: %w", err)
	}

	// Reconcile Ingress (if configured)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Ingress != nil && standalone.Spec.Service.Ingress.Enabled {
		if err := r.reconcileIngress(ctx, standalone); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile Ingress: %w", err)
		}
	}

	// Update status once at the end
	if err := r.updateStatus(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	logger.Info("Successfully reconciled Neo4jEnterpriseStandalone")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// reconcileConfigMap reconciles the ConfigMap for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileConfigMap(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Create ConfigMap using the standalone configuration
	configMap := r.createConfigMap(standalone)

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, configMap, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update ConfigMap with retry logic to handle resource version conflicts
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
			// ConfigMap updates for standalone deployments
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}
	logger.Info("Successfully created or updated ConfigMap", "name", configMap.Name)

	return nil
}

// reconcileService reconciles the Service for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileService(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Create Service using the standalone configuration
	service := r.createService(standalone)

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, service, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update Service with retry logic to handle resource version conflicts
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
			// Service updates for standalone deployments
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update Service: %w", err)
	}
	logger.Info("Successfully created or updated Service", "name", service.Name)

	return nil
}

// reconcileStatefulSet reconciles the StatefulSet for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStatefulSet(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Create StatefulSet using the standalone configuration
	statefulSet := r.createStatefulSet(standalone)

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, statefulSet, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update StatefulSet with retry logic to handle resource version conflicts
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, statefulSet, func() error {
			// StatefulSet template updates for standalone deployments
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update StatefulSet: %w", err)
	}
	logger.Info("Successfully created or updated StatefulSet", "name", statefulSet.Name)

	return nil
}

// reconcileIngress reconciles the Ingress for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileIngress(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	ingress := r.createIngress(standalone)
	if ingress == nil {
		return nil
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, ingress, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update Ingress
	existing := &networkingv1.Ingress{}
	if err := r.Get(ctx, types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating Ingress", "name", ingress.Name)
			if err := r.Create(ctx, ingress); err != nil {
				return fmt.Errorf("failed to create Ingress: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get Ingress: %w", err)
		}
	} else {
		// Update existing Ingress
		existing.Spec = ingress.Spec
		existing.Annotations = ingress.Annotations
		logger.Info("Updating Ingress", "name", ingress.Name)
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update Ingress: %w", err)
		}
	}

	return nil
}

// createConfigMap creates a ConfigMap for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createConfigMap(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *corev1.ConfigMap {
	// Build neo4j.conf content
	var configLines []string

	// Add header comment
	configLines = append(configLines, "# Neo4j Standalone Configuration (5.26+ / 2025.x.x)")
	configLines = append(configLines, "")

	// Add basic server configuration
	configLines = append(configLines, "# Basic Server Configuration")
	configLines = append(configLines, "server.default_listen_address=0.0.0.0")
	configLines = append(configLines, "server.bolt.enabled=true")
	configLines = append(configLines, "server.bolt.listen_address=:7687")
	configLines = append(configLines, "server.http.enabled=true")
	configLines = append(configLines, "server.http.listen_address=:7474")
	configLines = append(configLines, "")

	// Add TLS configuration if enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		configLines = append(configLines, "# TLS Configuration")
		configLines = append(configLines, "server.https.enabled=true")
		configLines = append(configLines, "server.https.listen_address=0.0.0.0:7473")
		configLines = append(configLines, "server.bolt.enabled=true")
		configLines = append(configLines, "server.bolt.listen_address=0.0.0.0:7687")
		configLines = append(configLines, "server.bolt.tls_level=REQUIRED")
		configLines = append(configLines, "")
		configLines = append(configLines, "# SSL Policy for HTTPS")
		configLines = append(configLines, "dbms.ssl.policy.https.enabled=true")
		configLines = append(configLines, "dbms.ssl.policy.https.base_directory=/ssl")
		configLines = append(configLines, "dbms.ssl.policy.https.private_key=tls.key")
		configLines = append(configLines, "dbms.ssl.policy.https.public_certificate=tls.crt")
		configLines = append(configLines, "dbms.ssl.policy.https.client_auth=NONE")
		configLines = append(configLines, "dbms.ssl.policy.https.tls_versions=TLSv1.3,TLSv1.2")
		configLines = append(configLines, "")
		configLines = append(configLines, "# SSL Policy for Bolt")
		configLines = append(configLines, "dbms.ssl.policy.bolt.enabled=true")
		configLines = append(configLines, "dbms.ssl.policy.bolt.base_directory=/ssl")
		configLines = append(configLines, "dbms.ssl.policy.bolt.private_key=tls.key")
		configLines = append(configLines, "dbms.ssl.policy.bolt.public_certificate=tls.crt")
		configLines = append(configLines, "dbms.ssl.policy.bolt.client_auth=NONE")
		configLines = append(configLines, "dbms.ssl.policy.bolt.tls_versions=TLSv1.3,TLSv1.2")
		configLines = append(configLines, "")
	}

	// Add user-provided configuration
	for key, value := range standalone.Spec.Config {
		configLines = append(configLines, fmt.Sprintf("%s=%s", key, value))
	}

	// Join all lines
	neo4jConf := strings.Join(configLines, "\n")

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", standalone.Name),
			Namespace: standalone.Namespace,
		},
		Data: map[string]string{
			"neo4j.conf": neo4jConf,
		},
	}
}

// createService creates a Service for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createService(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *corev1.Service {
	// Determine service type from spec
	serviceType := corev1.ServiceTypeClusterIP
	if standalone.Spec.Service != nil && standalone.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(standalone.Spec.Service.Type)
	}

	// Get annotations from spec
	annotations := make(map[string]string)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Annotations != nil {
		annotations = standalone.Spec.Service.Annotations
	}

	// Build service ports
	ports := []corev1.ServicePort{
		{
			Name:       "http",
			Port:       7474,
			TargetPort: intstr.FromInt(7474),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "bolt",
			Port:       7687,
			TargetPort: intstr.FromInt(7687),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	// Add HTTPS port if TLS is enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		ports = append(ports, corev1.ServicePort{
			Name:       "https",
			Port:       7473,
			TargetPort: intstr.FromInt(7473),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-service", standalone.Name),
			Namespace:   standalone.Namespace,
			Annotations: annotations,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "standalone",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				"app": standalone.Name,
			},
			Ports: ports,
		},
	}

	// Add enhanced features if specified
	if standalone.Spec.Service != nil {
		// LoadBalancer specific configurations
		if standalone.Spec.Service.LoadBalancerIP != "" {
			svc.Spec.LoadBalancerIP = standalone.Spec.Service.LoadBalancerIP
		}
		if len(standalone.Spec.Service.LoadBalancerSourceRanges) > 0 {
			svc.Spec.LoadBalancerSourceRanges = standalone.Spec.Service.LoadBalancerSourceRanges
		}

		// External traffic policy
		if standalone.Spec.Service.ExternalTrafficPolicy != "" {
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyType(standalone.Spec.Service.ExternalTrafficPolicy)
		}
	}

	return svc
}

// createStatefulSet creates a StatefulSet for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createStatefulSet(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *appsv1.StatefulSet {
	replicas := int32(1)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      standalone.Name,
			Namespace: standalone.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": standalone.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": standalone.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "neo4j",
							Image: fmt.Sprintf("%s:%s", standalone.Spec.Image.Repo, standalone.Spec.Image.Tag),
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 7474,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "https",
									ContainerPort: 7473,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "bolt",
									ContainerPort: 7687,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "backup",
									ContainerPort: 6362,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env:          r.buildEnvVars(standalone),
							VolumeMounts: r.buildVolumeMounts(standalone),
							Resources: func() corev1.ResourceRequirements {
								if standalone.Spec.Resources != nil {
									return *standalone.Spec.Resources
								}
								return corev1.ResourceRequirements{}
							}(),
						},
						r.buildBackupSidecarContainer(standalone),
					},
					Volumes:      r.buildVolumes(standalone),
					NodeSelector: standalone.Spec.NodeSelector,
					Tolerations:  standalone.Spec.Tolerations,
					Affinity:     standalone.Spec.Affinity,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "neo4j-data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						StorageClassName: &standalone.Spec.Storage.ClassName,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(standalone.Spec.Storage.Size),
							},
						},
					},
				},
			},
		},
	}
}

// updateStatus updates the status of the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) updateStatus(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Get the latest version of the resource to avoid conflicts
	latestStandalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{Name: standalone.Name, Namespace: standalone.Namespace}, latestStandalone); err != nil {
		return fmt.Errorf("failed to get latest standalone resource: %w", err)
	}

	// Get the StatefulSet to check its status
	statefulSet := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: standalone.Name, Namespace: standalone.Namespace}, statefulSet); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Calculate the desired status
	var phase, message string
	var ready bool

	if statefulSet.Status.ReadyReplicas == 1 {
		phase = "Running"
		message = "Standalone deployment is running"
		ready = true
	} else {
		phase = "Pending"
		message = "Waiting for standalone deployment to be ready"
		ready = false
	}

	// Check if status actually needs to be updated
	if latestStandalone.Status.Phase == phase &&
		latestStandalone.Status.Message == message &&
		latestStandalone.Status.Ready == ready &&
		latestStandalone.Status.Version == standalone.Spec.Image.Tag &&
		latestStandalone.Status.Endpoints != nil {
		logger.V(1).Info("Status unchanged, skipping update")
		return nil
	}

	// Update status on the latest version
	latestStandalone.Status.Phase = phase
	latestStandalone.Status.Message = message
	latestStandalone.Status.Ready = ready
	latestStandalone.Status.Version = standalone.Spec.Image.Tag

	// Update endpoints
	latestStandalone.Status.Endpoints = &neo4jv1alpha1.EndpointStatus{
		Bolt:  fmt.Sprintf("bolt://%s-service.%s.svc.cluster.local:7687", standalone.Name, standalone.Namespace),
		HTTP:  fmt.Sprintf("http://%s-service.%s.svc.cluster.local:7474", standalone.Name, standalone.Namespace),
		HTTPS: fmt.Sprintf("https://%s-service.%s.svc.cluster.local:7473", standalone.Name, standalone.Namespace),
	}

	// Update the status
	if err := r.Status().Update(ctx, latestStandalone); err != nil {
		logger.Error(err, "Failed to update status")
		return err
	}

	logger.V(1).Info("Status updated successfully", "phase", phase, "ready", ready)
	return nil
}

// cleanupResources cleans up resources during deletion
func (r *Neo4jEnterpriseStandaloneReconciler) cleanupResources(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Cleanup based on retention policy
	if standalone.Spec.Persistence != nil && standalone.Spec.Persistence.RetentionPolicy == "Delete" {
		// Delete PVCs
		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := r.List(ctx, pvcList, client.InNamespace(standalone.Namespace), client.MatchingLabels{
			"app": standalone.Name,
		}); err != nil {
			logger.Error(err, "Failed to list PVCs")
			return err
		}

		for _, pvc := range pvcList.Items {
			if err := r.Delete(ctx, &pvc); err != nil {
				logger.Error(err, "Failed to delete PVC", "pvc", pvc.Name)
				return err
			}
		}
	}

	return nil
}

// buildEnvVars builds environment variables for the standalone Neo4j container
func (r *Neo4jEnterpriseStandaloneReconciler) buildEnvVars(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}

	// Add essential Neo4j environment variables
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_EDITION",
		Value: "enterprise",
	})
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
		Value: "yes",
	})

	// Add auth credentials from secret if specified
	if standalone.Spec.Auth != nil && standalone.Spec.Auth.AdminSecret != "" {
		envVars = append(envVars,
			corev1.EnvVar{
				Name: "DB_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: standalone.Spec.Auth.AdminSecret,
						},
						Key: "username",
					},
				},
			},
			corev1.EnvVar{
				Name: "DB_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: standalone.Spec.Auth.AdminSecret,
						},
						Key: "password",
					},
				},
			},
		)
	}

	// Add user-provided environment variables
	envVars = append(envVars, standalone.Spec.Env...)

	// Set the config directory (always present now)
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_CONF",
		Value: "/conf",
	})

	return envVars
}

// buildVolumeMounts builds volume mounts for the standalone Neo4j container
func (r *Neo4jEnterpriseStandaloneReconciler) buildVolumeMounts(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "neo4j-data",
			MountPath: "/data",
		},
	}

	// Add ConfigMap mount (always present now)
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "neo4j-config",
		MountPath: "/conf",
		ReadOnly:  true,
	})

	// Add TLS certificate mount if TLS is enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "neo4j-certs",
			MountPath: "/ssl",
			ReadOnly:  true,
		})
	}

	return volumeMounts
}

// buildVolumes builds volumes for the standalone Neo4j pod
func (r *Neo4jEnterpriseStandaloneReconciler) buildVolumes(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Add ConfigMap volume (always present now)
	volumes = append(volumes, corev1.Volume{
		Name: "neo4j-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: fmt.Sprintf("%s-config", standalone.Name),
				},
			},
		},
	})

	// Add TLS certificate volume if TLS is enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		volumes = append(volumes, corev1.Volume{
			Name: "neo4j-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: fmt.Sprintf("%s-tls-secret", standalone.Name),
				},
			},
		})
	}

	// Add backup requests volume for backup sidecar
	volumes = append(volumes, corev1.Volume{
		Name: "backup-requests",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return volumes
}

// reconcileTLSCertificate reconciles the TLS certificate for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileTLSCertificate(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Create Certificate using cert-manager
	certificate := r.createTLSCertificate(standalone)

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, certificate, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update Certificate
	existing := &certmanagerv1.Certificate{}
	if err := r.Get(ctx, types.NamespacedName{Name: certificate.Name, Namespace: certificate.Namespace}, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating TLS Certificate", "name", certificate.Name)
			if err := r.Create(ctx, certificate); err != nil {
				return fmt.Errorf("failed to create TLS Certificate: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get TLS Certificate: %w", err)
		}
	} else {
		// Update existing Certificate
		existing.Spec = certificate.Spec
		logger.Info("Updating TLS Certificate", "name", certificate.Name)
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update TLS Certificate: %w", err)
		}
	}

	return nil
}

// createTLSCertificate creates a TLS certificate for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createTLSCertificate(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *certmanagerv1.Certificate {
	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-tls-cert", standalone.Name),
			Namespace: standalone.Namespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: fmt.Sprintf("%s-tls-secret", standalone.Name),
			IssuerRef: cmmeta.ObjectReference{
				Name: standalone.Spec.TLS.IssuerRef.Name,
				Kind: standalone.Spec.TLS.IssuerRef.Kind,
			},
			DNSNames: []string{
				fmt.Sprintf("%s-service", standalone.Name),
				fmt.Sprintf("%s-service.%s", standalone.Name, standalone.Namespace),
				fmt.Sprintf("%s-service.%s.svc", standalone.Name, standalone.Namespace),
				fmt.Sprintf("%s-service.%s.svc.cluster.local", standalone.Name, standalone.Namespace),
			},
		},
	}
}

// buildBackupSidecarContainer creates the backup sidecar container for standalone deployments
func (r *Neo4jEnterpriseStandaloneReconciler) buildBackupSidecarContainer(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) corev1.Container {
	return corev1.Container{
		Name:            "backup-sidecar",
		Image:           fmt.Sprintf("%s:%s", standalone.Spec.Image.Repo, standalone.Spec.Image.Tag),
		ImagePullPolicy: corev1.PullPolicy(standalone.Spec.Image.PullPolicy),
		Command: []string{
			"/bin/bash",
			"-c",
			`# Install jq if not available
which jq >/dev/null 2>&1 || apt-get update && apt-get install -y jq

# Function to clean old backups
cleanup_old_backups() {
	local backup_dir="/data/backups"
	local max_age_days="${BACKUP_RETENTION_DAYS:-7}"
	local max_count="${BACKUP_RETENTION_COUNT:-10}"

	if [ -d "$backup_dir" ]; then
		echo "Cleaning backups older than $max_age_days days..."
		find "$backup_dir" -maxdepth 1 -type d -mtime +$max_age_days -exec rm -rf {} \; 2>/dev/null || true

		# Keep only the most recent backups if count exceeds max
		backup_count=$(find "$backup_dir" -maxdepth 1 -type d | wc -l)
		if [ $backup_count -gt $max_count ]; then
			echo "Keeping only $max_count most recent backups..."
			find "$backup_dir" -maxdepth 1 -type d -printf '%T@ %p\n' | \
				sort -n | head -n -$max_count | cut -d' ' -f2- | \
				xargs -r rm -rf
		fi

		# Check disk usage
		df -h /data | tail -1
	fi
}

while true; do
	if [ -f /backup-requests/backup.request ]; then
		echo "Backup request found, starting backup..."
		REQUEST=$(cat /backup-requests/backup.request)
		BACKUP_PATH=$(echo $REQUEST | jq -r .path)
		BACKUP_TYPE=$(echo $REQUEST | jq -r '.type // "FULL"')
		DATABASE=$(echo $REQUEST | jq -r '.database // empty')

		# Clean up old backups before starting new one
		cleanup_old_backups

		# Create backup directory - Neo4j 5.26+ requires the full path to exist
		mkdir -p $BACKUP_PATH

		# Execute backup
		# Note: neo4j-admin in 5.x uses configuration from NEO4J_CONF directory
		export NEO4J_CONF=/var/lib/neo4j/conf

		if [ -z "$DATABASE" ]; then
			echo "Starting full standalone backup to $BACKUP_PATH with type $BACKUP_TYPE"
			neo4j-admin database backup --include-metadata=all --to-path=$BACKUP_PATH --type=$BACKUP_TYPE --verbose
		else
			echo "Starting database backup for $DATABASE to $BACKUP_PATH with type $BACKUP_TYPE"
			neo4j-admin database backup $DATABASE --to-path=$BACKUP_PATH --type=$BACKUP_TYPE --verbose
		fi

		# Save exit status
		BACKUP_STATUS=$?
		echo $BACKUP_STATUS > /backup-requests/backup.status

		if [ $BACKUP_STATUS -eq 0 ]; then
			echo "Backup completed successfully"
			# Clean up again after successful backup
			cleanup_old_backups
		else
			echo "Backup failed with status $BACKUP_STATUS"
		fi

		# Clean up request file
		rm -f /backup-requests/backup.request
	fi
	sleep 5
done`,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "neo4j-data",
				MountPath: "/data",
			},
			{
				Name:      "backup-requests",
				MountPath: "/backup-requests",
			},
			{
				Name:      "neo4j-config",
				MountPath: "/var/lib/neo4j/conf",
			},
		},
		Env: append([]corev1.EnvVar{
			{
				Name:  "BACKUP_RETENTION_DAYS",
				Value: "7", // Default: keep backups for 7 days
			},
			{
				Name:  "BACKUP_RETENTION_COUNT",
				Value: "10", // Default: keep maximum 10 backups
			},
			{
				Name:  "NEO4J_CONF",
				Value: "/var/lib/neo4j/conf",
			},
			{
				Name:  "NEO4J_HOME",
				Value: "/var/lib/neo4j",
			},
			{
				Name:  "NEO4J_EDITION",
				Value: "enterprise",
			},
			{
				Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
				Value: "yes",
			},
		}, r.buildEnvVars(standalone)...), // Append the main container environment variables
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}
}

// createIngress creates an Ingress for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createIngress(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) *networkingv1.Ingress {
	if standalone.Spec.Service == nil || standalone.Spec.Service.Ingress == nil || !standalone.Spec.Service.Ingress.Enabled {
		return nil
	}

	ingressSpec := standalone.Spec.Service.Ingress

	// Build TLS configuration
	var tls []networkingv1.IngressTLS
	if ingressSpec.TLSSecretName != "" {
		tls = []networkingv1.IngressTLS{
			{
				Hosts:      []string{ingressSpec.Host},
				SecretName: ingressSpec.TLSSecretName,
			},
		}
	}

	// Build HTTP paths
	pathType := networkingv1.PathTypePrefix
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     "/",
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: fmt.Sprintf("%s-service", standalone.Name),
					Port: networkingv1.ServiceBackendPort{
						Number: 7474,
					},
				},
			},
		},
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ingress", standalone.Name),
			Namespace:   standalone.Namespace,
			Annotations: ingressSpec.Annotations,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "ingress",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressSpec.ClassName,
			TLS:              tls,
			Rules: []networkingv1.IngressRule{
				{
					Host: ingressSpec.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jEnterpriseStandaloneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jEnterpriseStandalone{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{})

	// Only watch Certificate resources if cert-manager is available
	// This allows tests to run without cert-manager CRDs
	if mgr.GetScheme().Recognizes(certmanagerv1.SchemeGroupVersion.WithKind("Certificate")) {
		builder = builder.Owns(&certmanagerv1.Certificate{})
	}

	return builder.Complete(r)
}
