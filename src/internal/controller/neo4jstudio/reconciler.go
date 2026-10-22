/*
Copyright Neo4j.

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

// Package neo4jstudio reconciles a Neo4jStudio into an Enterprise Studio Deployment and companions
// (ADR-018): validate → resolve served deployments → render/apply → observe Deployment availability.
package neo4jstudio

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	domainstudio "github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/studio"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	renderstudio "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/studio"
)

// StudioReconciler drives a Neo4jStudio to a running Studio Deployment.
type StudioReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// NewReconciler builds a StudioReconciler bound to the manager.
func NewReconciler(mgr ctrl.Manager) *StudioReconciler {
	return &StudioReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("neo4jstudio-controller"),
	}
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jstudios,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jstudios/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *StudioReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithName("neo4jstudio")

	var studio neo4jv1.Neo4jStudio
	if err := r.Get(ctx, req.NamespacedName, &studio); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	resolved, rerr := domainstudio.ResolveDeployments(ctx, r.Client, &studio)
	if rerr != nil {
		switch rerr.Kind {
		case domainstudio.ResolveEditionUnsupported:
			return r.fail(ctx, &studio, oracle.ReasonStudioEditionUnsupported, rerr.Detail)
		default:
			return r.retryable(ctx, &studio, oracle.ReasonStudioTargetNotFound, rerr.Detail)
		}
	}

	if err := domainstudio.Apply(ctx, r.Client, r.Scheme, &studio, resolved); err != nil {
		return ctrl.Result{}, err
	}

	// Mirror the owned Deployment's availability into status.
	var dep appsv1.Deployment
	depKey := types.NamespacedName{Name: renderstudio.Name(&studio), Namespace: studio.Namespace}
	if err := r.Get(ctx, depKey, &dep); err != nil {
		if apierrors.IsNotFound(err) {
			return r.progressing(ctx, &studio, resolved, "Studio Deployment not observed yet")
		}
		return ctrl.Result{}, err
	}
	if deploymentAvailable(&dep) {
		return r.available(ctx, &studio, resolved)
	}
	log.V(1).Info("studio deployment not available yet", "deployment", dep.Name)
	return r.progressing(ctx, &studio, resolved, "waiting for the Studio Deployment to become Available")
}

func (r *StudioReconciler) available(ctx context.Context, s *neo4jv1.Neo4jStudio, resolved []renderstudio.ResolvedDeployment) (ctrl.Result, error) {
	s.Status.Phase = neo4jv1.StudioPhaseRunning
	s.Status.Reason = ""
	s.Status.Message = ""
	s.Status.URL = studioURL(s)
	s.Status.Deployments = deploymentStatuses(resolved)
	setCondition(s, oracle.ConditionStudioReady, metav1.ConditionTrue, oracle.ReasonStudioAvailable, "Studio is Available and serving")
	return ctrl.Result{}, r.writeStatus(ctx, s)
}

func (r *StudioReconciler) progressing(ctx context.Context, s *neo4jv1.Neo4jStudio, resolved []renderstudio.ResolvedDeployment, msg string) (ctrl.Result, error) {
	s.Status.Phase = neo4jv1.StudioPhasePending
	s.Status.Reason = oracle.ReasonStudioProgressing.String()
	s.Status.Message = msg
	s.Status.URL = studioURL(s)
	s.Status.Deployments = deploymentStatuses(resolved)
	setCondition(s, oracle.ConditionStudioReady, metav1.ConditionFalse, oracle.ReasonStudioProgressing, msg)
	if err := r.writeStatus(ctx, s); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *StudioReconciler) retryable(ctx context.Context, s *neo4jv1.Neo4jStudio, reason oracle.Reason, msg string) (ctrl.Result, error) {
	s.Status.Phase = neo4jv1.StudioPhasePending
	s.Status.Reason = reason.String()
	s.Status.Message = msg
	setCondition(s, oracle.ConditionStudioReady, metav1.ConditionFalse, reason, msg)
	if err := r.writeStatus(ctx, s); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *StudioReconciler) fail(ctx context.Context, s *neo4jv1.Neo4jStudio, reason oracle.Reason, msg string) (ctrl.Result, error) {
	s.Status.Phase = neo4jv1.StudioPhaseFailed
	s.Status.Reason = reason.String()
	s.Status.Message = msg
	setCondition(s, oracle.ConditionStudioReady, metav1.ConditionFalse, reason, msg)
	if r.Recorder != nil {
		r.Recorder.Event(s, corev1.EventTypeWarning, reason.String(), msg)
	}
	return ctrl.Result{}, r.writeStatus(ctx, s)
}

func (r *StudioReconciler) writeStatus(ctx context.Context, s *neo4jv1.Neo4jStudio) error {
	s.Status.ObservedGeneration = s.Generation
	if err := r.Status().Update(ctx, s); err != nil {
		if apierrors.IsConflict(err) {
			return nil // next reconcile recomputes from fresh state
		}
		return err
	}
	return nil
}

func setCondition(s *neo4jv1.Neo4jStudio, ctype oracle.Condition, status metav1.ConditionStatus, reason oracle.Reason, message string) {
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ctype.String(),
		Status:             status,
		Reason:             reason.String(),
		Message:            message,
		ObservedGeneration: s.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func deploymentStatuses(resolved []renderstudio.ResolvedDeployment) []neo4jv1.StudioDeploymentStatus {
	out := make([]neo4jv1.StudioDeploymentStatus, 0, len(resolved))
	for _, d := range resolved {
		// ponytail: Reachable is not probed yet — the asset-DB/deployment Bolt probe (ADR-018 §4-a)
		// is a follow-up; resolvedUri is reported so users can see what Studio was pointed at.
		out = append(out, neo4jv1.StudioDeploymentStatus{Name: d.Name, ResolvedURI: d.URI})
	}
	return out
}

// deploymentAvailable reports whether the Deployment's Available condition is True.
func deploymentAvailable(dep *appsv1.Deployment) bool {
	for _, c := range dep.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// studioURL resolves a user-facing URL from the Ingress (preferred) or the in-cluster Service DNS.
func studioURL(s *neo4jv1.Neo4jStudio) string {
	if s.Spec.Connectivity != nil && s.Spec.Connectivity.Ingress != nil && len(s.Spec.Connectivity.Ingress.Rules) > 0 {
		host := s.Spec.Connectivity.Ingress.Rules[0].Host
		scheme := "http"
		if len(s.Spec.Connectivity.Ingress.TLS) > 0 {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	return fmt.Sprintf("http://%s.%s.svc:%d", renderstudio.Name(s), s.Namespace, renderstudio.Port(s))
}

// SetupWithManager wires the reconciler and the objects it owns.
func (r *StudioReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1.Neo4jStudio{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
