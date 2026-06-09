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
	"reflect"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
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

func podSecurityContextForStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.PodSecurityContext {
	if standalone.Spec.SecurityContext != nil && standalone.Spec.SecurityContext.PodSecurityContext != nil {
		return standalone.Spec.SecurityContext.PodSecurityContext
	}
	return resources.DefaultNeo4jPodSecurityContext()
}

func containerSecurityContextForStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.SecurityContext {
	if standalone.Spec.SecurityContext != nil && standalone.Spec.SecurityContext.ContainerSecurityContext != nil {
		return standalone.Spec.SecurityContext.ContainerSecurityContext
	}
	return resources.DefaultNeo4jContainerSecurityContext()
}

// standaloneImagePullSecrets converts the standalone's image pull secret names to []corev1.LocalObjectReference.
func standaloneImagePullSecrets(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.LocalObjectReference {
	if len(standalone.Spec.Image.PullSecrets) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(standalone.Spec.Image.PullSecrets))
	for _, name := range standalone.Spec.Image.PullSecrets {
		if name == "" {
			continue
		}
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}

const (
	// StandaloneFinalizer is the finalizer for Neo4j enterprise standalone deployments
	StandaloneFinalizer = "neo4j.neo4j.com/standalone-finalizer"
)

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch

func (r *Neo4jEnterpriseStandaloneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jEnterpriseStandalone instance
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
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
		r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonValidationFailed,
			fmt.Sprintf("Validation failed: %v", validationErrs))

		// Update status to reflect validation failure
		standalone.Status.Phase = EventReasonValidationFailed
		standalone.Status.Message = fmt.Sprintf("Validation failed: %v", validationErrs)
		standalone.Status.Ready = false
		if err := r.Status().Update(ctx, standalone); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Verify the requested StorageClass exists before creating the StatefulSet.
	// A misnamed class (e.g. "standard" on a cluster that doesn't ship one) would
	// otherwise leave the pod Pending indefinitely with no operator-level signal.
	// An empty className is allowed and inherits the cluster default.
	if exists, scErr := storageClassExists(ctx, r.Client, standalone.Spec.Storage.ClassName); scErr != nil {
		logger.Error(scErr, "Failed to look up StorageClass", "storageClass", standalone.Spec.Storage.ClassName)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, scErr
	} else if !exists {
		msg := fmt.Sprintf("StorageClass %q not found; create it or set spec.storage.className to an existing class (or leave it empty to use the cluster default)", standalone.Spec.Storage.ClassName)
		logger.Error(fmt.Errorf("storage class not found"), msg)
		r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonStorageClassNotFound, msg)
		standalone.Status.Phase = "Failed"
		standalone.Status.Message = msg
		standalone.Status.Ready = false
		if statusErr := r.Status().Update(ctx, standalone); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Reconcile the standalone deployment
	result, err := r.reconcileStandalone(ctx, standalone)
	if err != nil {
		logger.Error(err, "Failed to reconcile standalone deployment")
		r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonReconcileFailed,
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
func (r *Neo4jEnterpriseStandaloneReconciler) handleDeletion(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) (ctrl.Result, error) {
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
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStandalone(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) (ctrl.Result, error) {
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

	// Reconcile MCP resources if enabled
	if err := r.reconcileMCP(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile MCP resources: %w", err)
	}

	// Pre-upgrade health check if image tag is changing
	if r.isStandaloneUpgradeRequired(standalone) {
		if blocked := r.preUpgradeHealthCheck(ctx, standalone); blocked {
			logger.Info("Upgrade blocked by pre-upgrade health check, requeueing")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		r.Recorder.Event(standalone, corev1.EventTypeNormal, EventReasonUpgradeStarted,
			fmt.Sprintf("Upgrading Neo4j from %s to %s", standalone.Status.Version, standalone.Spec.Image.Tag))
	}

	// Check if PVC storage expansion is needed before creating/updating the StatefulSet.
	if requeue, err := r.reconcileStandaloneStorageExpansion(ctx, standalone); err != nil {
		logger.Error(err, "Failed to reconcile storage expansion")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	} else if requeue {
		logger.Info("Storage expansion completed, requeueing to recreate StatefulSet")
		return ctrl.Result{Requeue: true}, nil
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

	// Reconcile NetworkPolicy if enabled (issue #128 gap #2 —
	// restricts port 6362 ingress to operator-managed backup pods).
	// Build returns nil when spec.networkPolicy.enabled is unset/false.
	if err := r.reconcileNetworkPolicy(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile NetworkPolicy: %w", err)
	}

	// Reconcile OpenShift Route (if configured via spec.service.route)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Route != nil && standalone.Spec.Service.Route.Enabled {
		route := resources.BuildRouteForStandalone(standalone)
		if route != nil {
			route.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(standalone, neo4jv1beta1.GroupVersion.WithKind("Neo4jEnterpriseStandalone"))})

			if err := r.createOrUpdateUnstructured(ctx, route); err != nil {
				if meta.IsNoMatchError(err) {
					logger.Info("Route API not available; skipping Route creation")
				} else {
					return ctrl.Result{}, fmt.Errorf("failed to reconcile Route: %w", err)
				}
			}
		}
	}

	// Reconcile ServiceMonitor for Prometheus scraping (non-fatal if Prometheus Operator not installed)
	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		if err := r.reconcileServiceMonitor(ctx, standalone); err != nil {
			if meta.IsNoMatchError(err) {
				logger.Info("ServiceMonitor API not available; skipping ServiceMonitor creation")
			} else {
				logger.Info("ServiceMonitor creation failed", "error", err.Error())
			}
		}
	}

	// Update status once at the end
	if err := r.updateStatus(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// Collect live diagnostics when standalone is Ready (non-fatal).
	// Diagnostics are collected by default; only skipped when monitoring is explicitly disabled.
	standaloneMonitoringDisabled := standalone.Spec.Monitoring != nil && !standalone.Spec.Monitoring.Enabled
	if !standaloneMonitoringDisabled && standalone.Status.Phase == "Ready" {
		if diagErr := r.collectStandaloneDiagnostics(ctx, standalone); diagErr != nil {
			logger.Error(diagErr, "Failed to collect standalone diagnostics (non-fatal)")
		}
	}

	// Reconcile Aura Fleet Management registration if enabled (non-fatal if it fails)
	if standalone.Spec.AuraFleetManagement != nil && standalone.Spec.AuraFleetManagement.Enabled {
		if err := r.reconcileAuraFleetManagement(ctx, standalone); err != nil {
			logger.Error(err, "Failed to reconcile Aura Fleet Management registration")
			if r.Recorder != nil {
				r.Recorder.Eventf(standalone, corev1.EventTypeWarning, EventReasonAuraFleetFailed,
					"Aura Fleet Management registration failed: %v", err)
			}
		}
	}

	logger.Info("Successfully reconciled Neo4jEnterpriseStandalone")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// reconcileConfigMap reconciles the ConfigMap for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileConfigMap(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
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

// reconcileService reconciles the client-facing Service and the headless
// Service for the standalone deployment.
//
// The headless Service ({name}-headless) is what gives the standalone pod
// a stable DNS name — {name}-0.{name}-headless.<ns>.svc.cluster.local —
// which the backup Job uses to reach port 6362. Without it the Job's
// neo4j-admin --from=<fqdn> argument resolves to nothing and the backup
// fails. The standalone STS's spec.serviceName references this service.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileService(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Headless Service first — the StatefulSet's spec.serviceName depends on it.
	headless := r.createHeadlessService(standalone)
	if err := controllerutil.SetControllerReference(standalone, headless, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on headless Service: %w", err)
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, headless, func() error { return nil })
		return err
	}); err != nil {
		return fmt.Errorf("failed to create or update headless Service: %w", err)
	}
	logger.Info("Successfully created or updated headless Service", "name", headless.Name)

	// Create client-facing Service using the standalone configuration
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

	// Reconcile OpenShift Route if requested
	if err := r.reconcileRoute(ctx, standalone); err != nil {
		return err
	}

	return nil
}

// createHeadlessService builds the headless Service ({name}-headless) that
// gives the standalone pod a stable DNS identity for backup, peer Bolt
// connections, and any other pod-direct addressing. ClusterIP=None makes
// it headless; the same `app: <name>` selector matches the pod created by
// the StatefulSet. Only the backup port (6362) is exposed since the
// client-facing Service ({name}-service) handles Bolt/HTTP.
func (r *Neo4jEnterpriseStandaloneReconciler) createHeadlessService(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-headless", standalone.Name),
			Namespace: standalone.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "standalone-headless",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			// PublishNotReadyAddresses lets the backup Job resolve the FQDN
			// even during a brief readiness blip. The pod is the same
			// pod whether or not its readiness probe is green at this
			// instant — losing DNS for it during reconciliation noise
			// would otherwise make the backup transiently fail.
			PublishNotReadyAddresses: true,
			Selector: map[string]string{
				"app": standalone.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "backup",
					Port:       6362,
					TargetPort: intstr.FromInt(6362),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// reconcileStatefulSet reconciles the StatefulSet for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStatefulSet(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
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

// reconcileNetworkPolicy reconciles the NetworkPolicy for the standalone
// deployment. Builds nil and returns early when spec.networkPolicy is
// disabled (default) — issue #128 gap #2.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileNetworkPolicy(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	desired := resources.BuildNetworkPolicyForStandalone(standalone)
	if desired == nil {
		return nil
	}
	if err := controllerutil.SetControllerReference(standalone, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on NetworkPolicy: %w", err)
	}

	existing := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating NetworkPolicy", "name", desired.Name)
			return r.Create(ctx, desired)
		}
		return fmt.Errorf("failed to get NetworkPolicy: %w", err)
	}
	// Update only when the spec actually drifted — avoids ResourceVersion
	// churn on every reconcile.
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		logger.Info("Updating NetworkPolicy", "name", desired.Name)
		return r.Update(ctx, existing)
	}
	return nil
}

// reconcileIngress reconciles the Ingress for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileIngress(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
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

// reconcileRoute ensures an OpenShift Route exists when requested
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileRoute(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	route := resources.BuildRouteForStandalone(standalone)
	if route == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(standalone, route, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on route: %w", err)
	}

	desired := route.DeepCopy()

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		route.SetLabels(desired.GetLabels())
		route.SetAnnotations(desired.GetAnnotations())
		route.Object["spec"] = desired.Object["spec"]
		return nil
	})
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Route API not available; skipping Route reconciliation")
			if r.Recorder != nil {
				r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonRouteAPINotFound, "route.openshift.io/v1 not available; skipping Route reconciliation")
			}
			return nil
		}
		return fmt.Errorf("failed to create or update Route: %w", err)
	}

	logger.Info("Successfully reconciled Route", "name", route.GetName())
	return nil
}

func (r *Neo4jEnterpriseStandaloneReconciler) reconcileMCP(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled {
		return nil
	}

	if service := resources.BuildMCPServiceForStandalone(standalone); service != nil {
		if err := r.createOrUpdateMCPResource(ctx, service, standalone); err != nil {
			return fmt.Errorf("failed to reconcile MCP service: %w", err)
		}
	}

	if deployment := resources.BuildMCPDeploymentForStandalone(standalone); deployment != nil {
		if err := r.createOrUpdateMCPResource(ctx, deployment, standalone); err != nil {
			return fmt.Errorf("failed to reconcile MCP deployment: %w", err)
		}
	}

	if ingress := resources.BuildMCPIngressForStandalone(standalone); ingress != nil {
		if err := r.createOrUpdateMCPResource(ctx, ingress, standalone); err != nil {
			return fmt.Errorf("failed to reconcile MCP ingress: %w", err)
		}
	}

	if err := r.reconcileMCPRoute(ctx, standalone); err != nil {
		return err
	}

	r.warnIfMCPMissingAPOC(ctx, standalone)
	return nil
}

func (r *Neo4jEnterpriseStandaloneReconciler) reconcileMCPRoute(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	route := resources.BuildMCPRouteForStandalone(standalone)
	if route == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(standalone, route, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on MCP route: %w", err)
	}

	desired := route.DeepCopy()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
			route.SetLabels(desired.GetLabels())
			route.SetAnnotations(desired.GetAnnotations())
			route.Object["spec"] = desired.Object["spec"]
			return nil
		})
		return err
	})
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Route API not available; skipping MCP Route reconciliation")
			if r.Recorder != nil {
				r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonRouteAPINotFound, "route.openshift.io/v1 not available; skipping MCP Route reconciliation")
			}
			return nil
		}
		return fmt.Errorf("failed to create or update MCP Route: %w", err)
	}

	logger.Info("Successfully reconciled MCP Route", "name", route.GetName())
	return nil
}

func (r *Neo4jEnterpriseStandaloneReconciler) createOrUpdateMCPResource(ctx context.Context, obj client.Object, owner client.Object) error {
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
			return nil
		})
		return err
	})
}

func (r *Neo4jEnterpriseStandaloneReconciler) warnIfMCPMissingAPOC(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled || r.Recorder == nil {
		return
	}

	plugins := &neo4jv1beta1.Neo4jPluginList{}
	if err := r.List(ctx, plugins, client.InNamespace(standalone.Namespace)); err != nil {
		log.FromContext(ctx).V(1).Info("Unable to list plugins for MCP APOC check", "error", err)
		return
	}

	for _, plugin := range plugins.Items {
		if plugin.Spec.ClusterRef != standalone.Name || !plugin.Spec.Enabled {
			continue
		}
		if isAPOCPluginName(plugin.Spec.Name) {
			return
		}
	}

	r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonMCPApocMissing,
		"MCP is enabled but APOC is not configured via Neo4jPlugin; MCP may fail in stdio mode and some tools may be unavailable.")
}

// createConfigMap creates a ConfigMap for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createConfigMap(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.ConfigMap {
	// Build neo4j.conf content
	var configLines []string

	// Add header comment
	configLines = append(configLines, "# Neo4j Standalone Configuration (5.26+ / 2025.x.x)")
	configLines = append(configLines, "")

	// Add basic server configuration
	configLines = append(configLines, "# Basic Server Configuration")
	configLines = append(configLines, "server.default_listen_address=0.0.0.0")
	configLines = append(configLines, "server.http.enabled=true")
	configLines = append(configLines, "server.http.listen_address=:7474")

	// Add TLS configuration if enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		// Bolt with TLS — listen on all interfaces with TLS required
		configLines = append(configLines, "server.bolt.enabled=true")
		configLines = append(configLines, "server.bolt.listen_address=0.0.0.0:7687")
		configLines = append(configLines, "server.bolt.tls_level=REQUIRED")
		configLines = append(configLines, "")
		configLines = append(configLines, "# TLS Configuration")
		configLines = append(configLines, "server.https.enabled=true")
		configLines = append(configLines, "server.https.listen_address=0.0.0.0:7473")
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
	} else {
		// Bolt without TLS
		configLines = append(configLines, "server.bolt.enabled=true")
		configLines = append(configLines, "server.bolt.listen_address=:7687")
		configLines = append(configLines, "")
	}

	// Backup listener — required for Neo4jBackup CRs targeting this
	// standalone to reach port 6362. Mirrors the cluster path's settings
	// in internal/resources/cluster.go (server.backup.{enabled,listen_address}).
	// Without these the standalone pod doesn't bind 6362 and the backup
	// Job's neo4j-admin --from=...:6362 connection is refused.
	configLines = append(configLines, "# Backup listener (port 6362)")
	configLines = append(configLines, "server.backup.enabled=true")
	configLines = append(configLines, "server.backup.listen_address=0.0.0.0:6362")
	configLines = append(configLines, "")

	// Metrics-subsystem hardening (unconditional, mirrors cluster path):
	// JMX off (unauthenticated MBeans surface) + CSV off (pod-ephemeral
	// files). See internal/resources/cluster.go for the full rationale.
	// Users who need either subsystem can re-enable via spec.config.
	configLines = append(configLines,
		"# Metrics subsystem hardening",
		"server.metrics.jmx.enabled=false",
		"server.metrics.csv.enabled=false",
		"",
		"# Seed-from-URI providers — register modern providers so every",
		"# documented URI scheme resolves. ServerSeedProvider is version-gated",
		"# (Neo4j 2026.04+); S3SeedProvider deliberately excluded — CloudSeedProvider",
		"# handles s3:// via SDK default credentials.",
		fmt.Sprintf("dbms.databases.seed_from_uri_providers=%s", resources.SeedFromURIProvidersConfigValue(standalone.Spec.Image.Tag)),
		"",
	)

	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		configLines = append(configLines, strings.Split(resources.BuildMonitoringConfig(standalone.Spec.Monitoring), "\n")...)
		configLines = append(configLines, "")
	}

	// Audit logging — appended AFTER monitoring so audit-driven values
	// override monitoring defaults on shared keys. BuildAuditConfig
	// returns "" when spec.audit is nil, so the call is unconditional.
	if auditCfg := resources.BuildAuditConfig(standalone.Spec.Audit); auditCfg != "" {
		configLines = append(configLines, strings.Split(strings.TrimRight(auditCfg, "\n"), "\n")...)
		configLines = append(configLines, "")
	}

	// Aura Fleet Management configuration
	if standalone.Spec.AuraFleetManagement != nil && standalone.Spec.AuraFleetManagement.Enabled {
		configLines = append(configLines, "# Aura Fleet Management")
		configLines = append(configLines, "dbms.security.procedures.unrestricted=fleetManagement.*")
		configLines = append(configLines, "dbms.security.procedures.allowlist=fleetManagement.*")
		configLines = append(configLines, "")
	}

	// Authentication/Authorization configuration from typed auth fields
	var authGeneratedKeys map[string]bool
	if standalone.Spec.Auth != nil {
		authResult := resources.BuildAuthConfig(standalone.Spec.Auth)
		if authResult.Config != "" {
			configLines = append(configLines, "# Authentication/Authorization Configuration")
			configLines = append(configLines, strings.Split(strings.TrimRight(authResult.Config, "\n"), "\n")...)
			configLines = append(configLines, "")
			authGeneratedKeys = make(map[string]bool, len(authResult.GeneratedKeys))
			for _, key := range authResult.GeneratedKeys {
				authGeneratedKeys[key] = true
			}
		}
	}

	// Add user-provided configuration. SSL policy keys are excluded
	// belt-and-suspenders style — the config validator already rejects
	// dbms.ssl.policy.* / server.bolt.tls_level / server.directories.
	// certificates at apply time, but since
	// server.config.strict_validation.enabled=false elsewhere lets Neo4j
	// silently honour a duplicate-key override, we must not append user
	// values here even if the validator was bypassed somehow.
	// Sort keys for deterministic conf ordering (the cluster path does the same):
	// the ConfigMap is hash-compared, so an unsorted map iteration would reorder
	// lines between reconciles and trigger spurious rolling restarts.
	userConfigKeys := make([]string, 0, len(standalone.Spec.Config))
	for key := range standalone.Spec.Config {
		userConfigKeys = append(userConfigKeys, key)
	}
	sort.Strings(userConfigKeys)
	for _, key := range userConfigKeys {
		if authGeneratedKeys != nil && authGeneratedKeys[key] {
			continue
		}
		if strings.HasPrefix(key, "dbms.ssl.policy.") ||
			key == "server.bolt.tls_level" ||
			key == "server.directories.certificates" {
			continue
		}
		configLines = append(configLines, fmt.Sprintf("%s=%s", key, standalone.Spec.Config[key]))
	}

	// Trusted-CA truststore JVM args (legacy spec.auth.trustStore + new
	// spec.trustedCASecrets). Emitted as `server.jvm.additional=...` so the
	// init container's truststore JKS is picked up by every JVM (incl. OIDC
	// HTTP client, LDAPS, plugin downloads, replication that uses default
	// JVM trust). User-supplied server.jvm.additional values via spec.Config
	// are preserved — Neo4j accepts repeated `server.jvm.additional=...` keys
	// and concatenates them.
	if len(standalone.Spec.TrustedCASecrets) > 0 ||
		(standalone.Spec.Auth != nil && standalone.Spec.Auth.TrustStore != nil) {
		configLines = append(configLines,
			"server.jvm.additional=-Djavax.net.ssl.trustStore=/truststore/truststore.jks",
			"server.jvm.additional=-Djavax.net.ssl.trustStorePassword=changeit",
		)
	}

	// Join all lines, then de-duplicate setting keys (keep last occurrence).
	// Without this, a key emitted by both monitoring and user spec.config
	// (e.g. db.logs.query.threshold) appears twice and CalVer Neo4j refuses to
	// start ("declared multiple times"). See resources.DedupeNeo4jConf.
	neo4jConf := resources.DedupeNeo4jConf(strings.Join(configLines, "\n"))

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", standalone.Name),
			Namespace: standalone.Namespace,
		},
		Data: map[string]string{
			"neo4j.conf": neo4jConf,
			"health.sh":  buildStandaloneHealthScript(),
		},
	}
}

// buildStandaloneHealthScript creates a health check script for standalone deployments
func buildStandaloneHealthScript() string {
	return `#!/bin/bash
# Health check script for Neo4j standalone

# Check if Neo4j process is running
if ! (pgrep -f "EnterpriseEntryPoint" > /dev/null || pgrep -f "Neo4jEnterprise" > /dev/null); then
    echo "Neo4j process not running"
    exit 1
fi

# Check if HTTP port is responding
if (echo > /dev/tcp/localhost/7474) >/dev/null 2>&1; then
    echo "Neo4j HTTP port responding - healthy"
    exit 0
fi

echo "Neo4j process running but HTTP port not responding"
exit 1
`
}

// buildStandaloneReadinessProbe creates a readiness probe for standalone deployments.
// The startup probe gates this — initialDelaySeconds is 0 because by the time
// this probe runs, the startup probe has already confirmed Neo4j is responding.
func buildStandaloneReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", "/conf/health.sh"},
			},
		},
		InitialDelaySeconds: 0,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
}

// buildStandaloneLivenessProbe creates a liveness probe for standalone deployments.
// Gated by the startup probe — only runs after Neo4j is confirmed healthy.
func buildStandaloneLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", "/conf/health.sh"},
			},
		},
		InitialDelaySeconds: 0,
		PeriodSeconds:       30,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
}

// buildStandaloneStartupProbe creates a startup probe for standalone deployments.
// This is the only probe that runs during initial Neo4j startup. Readiness and
// liveness probes are disabled until this succeeds.
func buildStandaloneStartupProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", "/conf/health.sh"},
			},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    30, // Allow up to 5 minutes for startup (30 * 10s)
		SuccessThreshold:    1,
	}
}

// createService creates a Service for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createService(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.Service {
	// Determine service type from spec
	serviceType := corev1.ServiceTypeClusterIP
	if standalone.Spec.Service != nil && standalone.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(standalone.Spec.Service.Type)
	}

	// Get annotations from spec
	annotations := make(map[string]string)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Annotations != nil {
		for k, v := range standalone.Spec.Service.Annotations {
			annotations[k] = v
		}
	}
	resources.ApplyExternalDNSAnnotation(annotations, standalone.Spec.Service)

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

	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "metrics",
			Port:       resources.MetricsPort,
			TargetPort: intstr.FromInt(resources.MetricsPort),
			Protocol:   corev1.ProtocolTCP,
		})
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
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicy(standalone.Spec.Service.ExternalTrafficPolicy)
		}
	}

	return svc
}

// createStatefulSet creates a StatefulSet for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createStatefulSet(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *appsv1.StatefulSet {
	replicas := int32(1)
	annotations := map[string]string{}
	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		annotations["prometheus.io/scrape"] = "true"
		annotations["prometheus.io/port"] = fmt.Sprintf("%d", resources.MetricsPort)
		annotations["prometheus.io/path"] = "/metrics"
	}

	ports := []corev1.ContainerPort{
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
	}

	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name:          "metrics",
			ContainerPort: resources.MetricsPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// Determine StatefulSet update strategy from spec
	updateStrategy := appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
	}
	if standalone.Spec.UpgradeStrategy != nil && standalone.Spec.UpgradeStrategy.Strategy == "Recreate" {
		updateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      standalone.Name,
			Namespace: standalone.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			// ServiceName must reference a headless Service so each pod
			// gets a stable DNS name (<name>-0.<service>.<ns>.svc.cluster.local).
			// The backup Job path uses this FQDN on port 6362 — without a
			// ServiceName + matching headless service, neo4j-admin database
			// backup --from=... can't reach the standalone.
			ServiceName:    fmt.Sprintf("%s-headless", standalone.Name),
			UpdateStrategy: updateStrategy,
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
					Annotations: annotations,
				},
				Spec: func() corev1.PodSpec {
					image := fmt.Sprintf("%s:%s", standalone.Spec.Image.Repo, standalone.Spec.Image.Tag)
					var legacyTrustStore *neo4jv1beta1.SecretKeyRef
					if standalone.Spec.Auth != nil {
						legacyTrustStore = standalone.Spec.Auth.TrustStore
					}
					trustedCAs := resources.CollectTrustedCASecrets(legacyTrustStore, standalone.Spec.TrustedCASecrets)
					var initContainers []corev1.Container
					if len(trustedCAs) > 0 {
						initContainers = append(initContainers, resources.BuildTrustStoreInitContainer(image, trustedCAs))
					}
					return corev1.PodSpec{
						InitContainers: initContainers,
						Containers: []corev1.Container{
							{
								Name:            "neo4j",
								Image:           image,
								SecurityContext: containerSecurityContextForStandalone(standalone),
								Ports:           ports,
								Env:             r.buildEnvVars(standalone),
								// Project user-supplied env-from-Secret / env-from-ConfigMap
								// bundles onto the container. Mirrors Neo4jEnterpriseCluster
								// behaviour: cloud creds for seedURI restores live here.
								EnvFrom:      standalone.Spec.ExtraEnvFrom,
								VolumeMounts: r.buildVolumeMounts(standalone),
								Resources: func() corev1.ResourceRequirements {
									if standalone.Spec.Resources != nil {
										return *standalone.Spec.Resources
									}
									return corev1.ResourceRequirements{}
								}(),
								ReadinessProbe: buildStandaloneReadinessProbe(),
								LivenessProbe:  buildStandaloneLivenessProbe(),
								StartupProbe:   buildStandaloneStartupProbe(),
							},
						},
						SecurityContext:  podSecurityContextForStandalone(standalone),
						Volumes:          r.buildVolumes(standalone),
						NodeSelector:     standalone.Spec.NodeSelector,
						Tolerations:      standalone.Spec.Tolerations,
						Affinity:         standalone.Spec.Affinity,
						ImagePullSecrets: standaloneImagePullSecrets(standalone),
					}
				}(),
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "neo4j-data",
						Labels: resources.GetLabelsForPVC(standalone.Name, "data"),
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						StorageClassName: resources.StorageClassNamePtr(standalone.Spec.Storage.ClassName),
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
func (r *Neo4jEnterpriseStandaloneReconciler) updateStatus(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Get the latest version of the resource to avoid conflicts
	latestStandalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
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
		phase = "Ready"
		message = "Standalone deployment is ready"
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
		latestStandalone.Status.ObservedGeneration == latestStandalone.Generation &&
		latestStandalone.Status.Endpoints != nil {
		logger.V(1).Info("Status unchanged, skipping update")
		return nil
	}

	// Update status on the latest version
	latestStandalone.Status.Phase = phase
	latestStandalone.Status.Message = message
	latestStandalone.Status.Ready = ready
	latestStandalone.Status.Version = standalone.Spec.Image.Tag
	latestStandalone.Status.ObservedGeneration = latestStandalone.Generation

	// Update Ready condition using standard helper
	condStatus, condReason := PhaseToConditionStatus(phase)
	SetReadyCondition(&latestStandalone.Status.Conditions, latestStandalone.Generation, condStatus, condReason, message)

	// Update endpoints — use bolt+s:// when TLS is enabled
	boltScheme := "bolt"
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		boltScheme = "bolt+s"
	}
	hasTLS := standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager"

	// Resolve service type + external IP for the connection-example helper.
	// Defaults to ClusterIP when spec.service is unset; LoadBalancer external
	// IP comes from the live Service status if the cloud assigner has filled
	// it in, otherwise the helper substitutes a `<external-ip>` placeholder.
	serviceType := corev1.ServiceTypeClusterIP
	if standalone.Spec.Service != nil && standalone.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(standalone.Spec.Service.Type)
	}
	externalIP := standaloneServiceExternalIP(ctx, r.Client, standalone)

	latestStandalone.Status.Endpoints = &neo4jv1beta1.EndpointStatus{
		Bolt:               fmt.Sprintf("%s://%s-service.%s.svc.cluster.local:7687", boltScheme, standalone.Name, standalone.Namespace),
		HTTP:               fmt.Sprintf("http://%s-service.%s.svc.cluster.local:7474", standalone.Name, standalone.Namespace),
		HTTPS:              fmt.Sprintf("https://%s-service.%s.svc.cluster.local:7473", standalone.Name, standalone.Namespace),
		ConnectionExamples: GenerateStandaloneConnectionExamples(standalone.Name, standalone.Namespace, serviceType, externalIP, hasTLS),
	}

	// Update the status
	if err := r.Status().Update(ctx, latestStandalone); err != nil {
		logger.Error(err, "Failed to update status")
		return err
	}

	logger.V(1).Info("Status updated successfully", "phase", phase, "ready", ready)
	return nil
}

// collectStandaloneDiagnostics runs SHOW DATABASES against the standalone instance
// and writes results into status.diagnostics. Non-fatal: errors are surfaced in status only.
func (r *Neo4jEnterpriseStandaloneReconciler) collectStandaloneDiagnostics(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Collecting standalone diagnostics", "standalone", standalone.Name)

	diagnostics := &neo4jv1beta1.StandaloneDiagnosticsStatus{}

	neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	if err != nil {
		diagnostics.CollectionError = fmt.Sprintf("cannot connect to Neo4j: %v", err)
		return r.updateStandaloneDiagnostics(ctx, standalone, diagnostics)
	}
	defer neo4jClient.Close()

	databases, dbErr := neo4jClient.GetDatabases(ctx)
	if dbErr != nil {
		logger.Error(dbErr, "Failed to collect SHOW DATABASES")
		diagnostics.CollectionError = fmt.Sprintf("SHOW DATABASES failed: %v", dbErr)
	} else {
		for _, d := range databases {
			diagnostics.Databases = append(diagnostics.Databases, neo4jv1beta1.DatabaseDiagnosticInfo{
				Name:            d.Name,
				Status:          d.Status,
				RequestedStatus: d.RequestedStatus,
				Role:            d.Role,
				Default:         d.Default,
			})
		}
	}

	collectUsersAndRoles(ctx, neo4jClient,
		&diagnostics.Users, &diagnostics.UserCount,
		&diagnostics.Roles, &diagnostics.RoleCount,
		&diagnostics.CollectionError, logger)

	now := metav1.Now()
	diagnostics.LastCollected = &now

	return r.updateStandaloneDiagnostics(ctx, standalone, diagnostics)
}

// updateStandaloneDiagnostics persists diagnostics into standalone status with retry.
func (r *Neo4jEnterpriseStandaloneReconciler) updateStandaloneDiagnostics(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone, diagnostics *neo4jv1beta1.StandaloneDiagnosticsStatus) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(standalone), latest); err != nil {
			return err
		}
		latest.Status.Diagnostics = diagnostics
		return r.Status().Update(ctx, latest)
	})
}

// isStandaloneUpgradeRequired returns true if the Neo4j image tag is changing.
func (r *Neo4jEnterpriseStandaloneReconciler) isStandaloneUpgradeRequired(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) bool {
	return standalone.Status.Version != "" && standalone.Status.Version != standalone.Spec.Image.Tag
}

// preUpgradeHealthCheck runs a health check before allowing an image upgrade.
// Returns true if the upgrade should be blocked (health check failed and autoPause enabled).
func (r *Neo4jEnterpriseStandaloneReconciler) preUpgradeHealthCheck(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) bool {
	logger := log.FromContext(ctx)

	// Skip if no upgrade strategy or health check disabled
	if standalone.Spec.UpgradeStrategy == nil || !standalone.Spec.UpgradeStrategy.PreUpgradeHealthCheck {
		return false
	}

	// Only check if standalone is currently Ready
	if standalone.Status.Phase != "Ready" {
		return false
	}

	neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	if err != nil {
		logger.Error(err, "Pre-upgrade health check: cannot connect to Neo4j")
		if standalone.Spec.UpgradeStrategy.AutoPauseOnFailure {
			r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonUpgradeFailed,
				fmt.Sprintf("Pre-upgrade health check failed (cannot connect): %v", err))
			return true
		}
		return false
	}
	defer neo4jClient.Close()

	// Simple connectivity check
	if err := neo4jClient.VerifyConnectivity(ctx); err != nil {
		logger.Error(err, "Pre-upgrade health check failed")
		if standalone.Spec.UpgradeStrategy.AutoPauseOnFailure {
			r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonUpgradeFailed,
				fmt.Sprintf("Pre-upgrade health check failed: %v", err))
			return true
		}
	}

	logger.Info("Pre-upgrade health check passed")
	return false
}

// cleanupResources cleans up resources during deletion
func (r *Neo4jEnterpriseStandaloneReconciler) cleanupResources(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Cleanup based on retention policy (uses spec.storage.retentionPolicy, matching cluster pattern)
	if standalone.Spec.Storage.RetentionPolicy == "Delete" || standalone.Spec.Storage.RetentionPolicy == "" {
		// Delete PVCs. Selector must match labels emitted by resources.GetLabelsForPVC,
		// which is what the standalone StatefulSet's VolumeClaimTemplates are labeled with.
		// Previously used "app=<name>" which only appears on pods, not PVCs — making this a no-op.
		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := r.List(ctx, pvcList, client.InNamespace(standalone.Namespace),
			client.MatchingLabels(resources.PVCSelectorByInstance(standalone.Name))); err != nil {
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
func (r *Neo4jEnterpriseStandaloneReconciler) buildEnvVars(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.EnvVar {
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
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_UDC_PACKAGING",
		Value: resources.OperatorUDCPackagingValue(),
	})

	// Determine auth secret name (use default if not specified)
	authSecretName := "neo4j-admin-secret" // Default secret name
	if standalone.Spec.Auth != nil && standalone.Spec.Auth.AdminSecret != "" {
		authSecretName = standalone.Spec.Auth.AdminSecret
	}

	// Always add auth credentials (either from specified secret or default)
	// This ensures Neo4j doesn't generate a random password
	envVars = append(envVars,
		corev1.EnvVar{
			Name: "DB_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: authSecretName,
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
						Name: authSecretName,
					},
					Key: "password",
				},
			},
		},
		// Set NEO4J_AUTH in the format Neo4j expects (username/password)
		// This combines the username and password from the secret into the standard Neo4j format
		corev1.EnvVar{
			Name:  "NEO4J_AUTH",
			Value: "$(DB_USERNAME)/$(DB_PASSWORD)",
		},
	)

	// Add LDAP system account credentials from Secret (never in ConfigMap)
	if authEnvVars := resources.BuildAuthEnvVars(standalone.Spec.Auth); len(authEnvVars) > 0 {
		envVars = append(envVars, authEnvVars...)
	}

	// Add user-provided environment variables
	envVars = append(envVars, standalone.Spec.Env...)

	// NOTE: NEO4J_PLUGINS for fleet-management is applied via a live StatefulSet patch
	// in reconcileAuraFleetManagement, not baked here, so it merges with other plugins.

	// Set the config directory (always present now)
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_CONF",
		Value: "/conf",
	})

	return envVars
}

// buildVolumeMounts builds volume mounts for the standalone Neo4j container
func (r *Neo4jEnterpriseStandaloneReconciler) buildVolumeMounts(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.VolumeMount {
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

	// Add truststore volume mount when any trusted CA is configured (legacy
	// spec.auth.trustStore or new spec.trustedCASecrets list).
	if len(standalone.Spec.TrustedCASecrets) > 0 ||
		(standalone.Spec.Auth != nil && standalone.Spec.Auth.TrustStore != nil) {
		volumeMounts = append(volumeMounts, resources.TrustStoreVolumeMount)
	}

	// User-supplied extra volume mounts.
	if len(standalone.Spec.ExtraVolumeMounts) > 0 {
		volumeMounts = append(volumeMounts, standalone.Spec.ExtraVolumeMounts...)
	}

	return volumeMounts
}

// buildVolumes builds volumes for the standalone Neo4j pod
func (r *Neo4jEnterpriseStandaloneReconciler) buildVolumes(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Add ConfigMap volume (always present now, 0755 for executable health.sh)
	volumes = append(volumes, corev1.Volume{
		Name: "neo4j-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: fmt.Sprintf("%s-config", standalone.Name),
				},
				DefaultMode: func() *int32 { mode := int32(0o755); return &mode }(),
			},
		},
	})

	// Add TLS certificate volume if TLS is enabled.
	//
	// DefaultMode 0440 makes the private key at /ssl/tls.key
	// owner+group readable but not world-readable. Neo4j runs as
	// UID/GID 7474 with FSGroup 7474 so both owner and group are
	// the Neo4j process; world-readable key files fail Pod Security
	// "restricted" and CIS Kubernetes baseline checks. Mirrors the
	// cluster path's hardening (internal/resources/cluster.go).
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		volumes = append(volumes, corev1.Volume{
			Name: "neo4j-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%s-tls-secret", standalone.Name),
					DefaultMode: func() *int32 { mode := int32(0o440); return &mode }(),
				},
			},
		})
	}

	// Add truststore volumes (legacy spec.auth.trustStore + new spec.trustedCASecrets).
	var legacyTrustStore *neo4jv1beta1.SecretKeyRef
	if standalone.Spec.Auth != nil {
		legacyTrustStore = standalone.Spec.Auth.TrustStore
	}
	trustedCAs := resources.CollectTrustedCASecrets(legacyTrustStore, standalone.Spec.TrustedCASecrets)
	if len(trustedCAs) > 0 {
		volumes = append(volumes, resources.BuildTrustStoreVolumes(trustedCAs)...)
	}

	// User-supplied extra volumes.
	if len(standalone.Spec.ExtraVolumes) > 0 {
		volumes = append(volumes, standalone.Spec.ExtraVolumes...)
	}

	return volumes
}

// reconcileTLSCertificate reconciles the TLS certificate for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileTLSCertificate(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
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
func (r *Neo4jEnterpriseStandaloneReconciler) createTLSCertificate(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *certmanagerv1.Certificate {
	dnsNames := []string{
		fmt.Sprintf("%s-service", standalone.Name),
		fmt.Sprintf("%s-service.%s", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-service.%s.svc", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-service.%s.svc.cluster.local", standalone.Name, standalone.Namespace),
	}
	// Include the public DNS name (spec.service.dnsName) so TLS connections
	// to the external hostname pass hostname verification — same rationale
	// as the cluster path in internal/resources/cluster.go.
	if standalone.Spec.Service != nil && standalone.Spec.Service.DNSName != "" {
		dnsNames = append(dnsNames, standalone.Spec.Service.DNSName)
	}

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-tls-cert", standalone.Name),
			Namespace: standalone.Namespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: fmt.Sprintf("%s-tls-secret", standalone.Name),
			IssuerRef: cmmeta.IssuerReference{
				Name:  standalone.Spec.TLS.IssuerRef.Name,
				Kind:  standalone.Spec.TLS.IssuerRef.Kind,
				Group: standalone.Spec.TLS.IssuerRef.Group,
			},
			// SecretTemplate propagates ownership labels onto the TLS
			// Secret cert-manager issues. Mirrors the cluster path's
			// labelling — see internal/resources/cluster.go.
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
					"app.kubernetes.io/component":  "tls",
					"neo4j.com/owner-kind":         "Neo4jEnterpriseStandalone",
					"neo4j.com/owner-name":         standalone.Name,
				},
			},
			DNSNames: dnsNames,
		},
	}
}

// createIngress creates an Ingress for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createIngress(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *networkingv1.Ingress {
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

	ingressAnnotations := map[string]string{}
	for k, v := range ingressSpec.Annotations {
		ingressAnnotations[k] = v
	}
	resources.ApplyExternalDNSAnnotation(ingressAnnotations, standalone.Spec.Service)

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ingress", standalone.Name),
			Namespace:   standalone.Namespace,
			Annotations: ingressAnnotations,
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

// reconcileServiceMonitor creates or updates a ServiceMonitor for standalone Prometheus scraping.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileServiceMonitor(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	serviceMonitor := &unstructured.Unstructured{}
	serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	})
	serviceMonitor.SetName(standalone.Name + "-monitoring")
	serviceMonitor.SetNamespace(standalone.Namespace)
	serviceMonitor.SetLabels(map[string]string{
		"app":                        "neo4j",
		"app.kubernetes.io/name":     "neo4j",
		"app.kubernetes.io/instance": standalone.Name,
	})

	serviceMonitor.Object["spec"] = map[string]any{
		"selector": map[string]any{
			"matchLabels": map[string]any{
				"app.kubernetes.io/name":     "neo4j",
				"app.kubernetes.io/instance": standalone.Name,
			},
		},
		"endpoints": []map[string]any{
			{
				"port":     "metrics",
				"interval": "30s",
				"path":     "/metrics",
			},
		},
	}

	if err := ctrl.SetControllerReference(standalone, serviceMonitor, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on ServiceMonitor: %w", err)
	}

	return r.createOrUpdateUnstructured(ctx, serviceMonitor)
}

func (r *Neo4jEnterpriseStandaloneReconciler) createOrUpdateUnstructured(ctx context.Context, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, obj)
		}
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jEnterpriseStandaloneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jEnterpriseStandalone{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{})

	// Only watch Certificate resources if cert-manager is actually installed
	// in the cluster. We check the REST mapper (real CRD presence) rather than
	// the scheme — the cert-manager types are always registered in the scheme
	// (cmd/main.go), so a scheme check (`Recognizes`) is always true and would
	// start a Certificate informer even when the CRD is absent, crashing the
	// manager at cache-sync. This makes cert-manager an OPTIONAL dependency,
	// required only for clusters that use cert-manager TLS. Mirrors the Route
	// guard below.
	certGVK := certmanagerv1.SchemeGroupVersion.WithKind("Certificate")
	if _, err := mgr.GetRESTMapper().RESTMapping(certGVK.GroupKind(), certGVK.Version); err == nil {
		builder = builder.Owns(&certmanagerv1.Certificate{})
	}

	routeGVK := schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}
	if _, err := mgr.GetRESTMapper().RESTMapping(routeGVK.GroupKind(), routeGVK.Version); err == nil {
		routeObj := &unstructured.Unstructured{}
		routeObj.SetGroupVersionKind(routeGVK)
		routeHandler := handler.TypedEnqueueRequestForOwner[*unstructured.Unstructured](mgr.GetScheme(), mgr.GetRESTMapper(), &neo4jv1beta1.Neo4jEnterpriseStandalone{}, handler.OnlyControllerOwner())
		builder = builder.WatchesRawSource(source.Kind(mgr.GetCache(), routeObj, routeHandler))
	}

	return builder.Complete(r)
}

// reconcileAuraFleetManagement handles Aura Fleet Management for standalone deployments.
//
// Phase 1 — Plugin installation: merges "fleet-management" into NEO4J_PLUGINS on the
//
//	live StatefulSet (same pattern as the Neo4jPlugin controller), triggering a pod restart
//	so the Docker entrypoint copies the pre-bundled jar from /var/lib/neo4j/products/.
//
// Phase 2 — Token registration: once the standalone is Ready and the plugin is loaded,
//
//	reads the Aura token from the referenced Secret and calls registerToken.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileAuraFleetManagement(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)
	spec := standalone.Spec.AuraFleetManagement

	// --- Phase 1: ensure the plugin jar is loaded ---
	// The standalone StatefulSet name is the same as the standalone resource name.
	if err := r.mergeFleetManagementPlugin(ctx, standalone.Name, standalone.Namespace); err != nil {
		logger.Error(err, "Failed to patch StatefulSet NEO4J_PLUGINS for fleet-management")
		if r.Recorder != nil {
			r.Recorder.Eventf(standalone, corev1.EventTypeWarning, EventReasonAuraFleetPluginPatchFailed,
				"Failed to add fleet-management to NEO4J_PLUGINS: %v", err)
		}
		return nil
	}

	// --- Phase 2: token registration ---
	if spec.TokenSecretRef == nil {
		logger.Info("Aura Fleet Management enabled but no tokenSecretRef configured; plugin installed, registration deferred")
		return nil
	}

	if standalone.Status.Phase != "Ready" {
		logger.Info("Standalone not yet Ready; fleet-management plugin patch applied, registration deferred",
			"phase", standalone.Status.Phase)
		return nil
	}

	if standalone.Status.AuraFleetManagement != nil && standalone.Status.AuraFleetManagement.Registered {
		logger.V(1).Info("Aura Fleet Management already registered; nothing to do")
		return nil
	}

	secretName := spec.TokenSecretRef.Name
	secretKey := spec.TokenSecretRef.Key
	if secretKey == "" {
		secretKey = "token"
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: standalone.Namespace, Name: secretName}, secret); err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("cannot read token secret %s: %v", secretName, err))
	}

	tokenBytes, ok := secret.Data[secretKey]
	if !ok || len(tokenBytes) == 0 {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("key %q not found in secret %s", secretKey, secretName))
	}
	token := strings.TrimSpace(string(tokenBytes))

	neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	if err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("cannot connect to Neo4j: %v", err))
	}
	defer neo4jClient.Close()

	installed, err := neo4jClient.IsFleetManagementInstalled(ctx)
	if err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("cannot check fleet management plugin: %v", err))
	}
	if !installed {
		logger.Info("Fleet management plugin not yet loaded; will retry on next reconcile")
		return nil
	}

	if err := neo4jClient.RegisterFleetManagementToken(ctx, token); err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("token registration failed: %v", err))
	}

	logger.Info("Aura Fleet Management token registered successfully")
	if r.Recorder != nil {
		r.Recorder.Event(standalone, corev1.EventTypeNormal, EventReasonAuraFleetRegistered,
			"Successfully registered with Aura Fleet Management")
	}
	return r.setFleetManagementStatus(ctx, standalone, true, "Registered with Aura Fleet Management")
}

// mergeFleetManagementPlugin patches the named StatefulSet so "fleet-management" is present
// in NEO4J_PLUGINS. Idempotent: if already present, no patch is issued.
func (r *Neo4jEnterpriseStandaloneReconciler) mergeFleetManagementPlugin(ctx context.Context, stsName, namespace string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, sts); err != nil {
			return fmt.Errorf("get StatefulSet %s: %w", stsName, err)
		}

		containerIdx := -1
		for i, c := range sts.Spec.Template.Spec.Containers {
			if c.Name == "neo4j" {
				containerIdx = i
				break
			}
		}
		if containerIdx < 0 {
			return fmt.Errorf("neo4j container not found in StatefulSet %s", stsName)
		}

		envs := sts.Spec.Template.Spec.Containers[containerIdx].Env
		pluginsIdx := -1
		for i, e := range envs {
			if e.Name == "NEO4J_PLUGINS" {
				pluginsIdx = i
				break
			}
		}

		var updated string
		var err error
		if pluginsIdx < 0 {
			updated = `["fleet-management"]`
		} else {
			current := envs[pluginsIdx].Value
			updated, err = MergeNeo4jPluginList(current, "fleet-management")
			if err != nil {
				return fmt.Errorf("merge plugin list: %w", err)
			}
			if updated == current {
				return nil // already present — nothing to patch
			}
		}

		stsCopy := sts.DeepCopy()
		if pluginsIdx < 0 {
			stsCopy.Spec.Template.Spec.Containers[containerIdx].Env = append(
				stsCopy.Spec.Template.Spec.Containers[containerIdx].Env,
				corev1.EnvVar{Name: "NEO4J_PLUGINS", Value: updated},
			)
		} else {
			stsCopy.Spec.Template.Spec.Containers[containerIdx].Env[pluginsIdx].Value = updated
		}
		return r.Update(ctx, stsCopy)
	})
}

func (r *Neo4jEnterpriseStandaloneReconciler) setFleetManagementStatus(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone, registered bool, message string) error {
	now := metav1.Now()
	status := &neo4jv1beta1.AuraFleetManagementStatus{
		Registered: registered,
		Message:    message,
	}
	if registered {
		status.LastRegistrationTime = &now
	}

	latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: standalone.Namespace, Name: standalone.Name}, latest); err != nil {
		return err
	}
	latest.Status.AuraFleetManagement = status
	return r.Status().Update(ctx, latest)
}
