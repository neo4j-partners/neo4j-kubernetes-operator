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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
)

// Neo4jGrantReconciler reconciles a Neo4jGrant object
type Neo4jGrantReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
}

const (
	// GrantFinalizer is the finalizer for Neo4j grant resources
	GrantFinalizer = "neo4j.com/grant-finalizer"
)

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jgrants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jgrants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jgrants/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation of Neo4jGrant resources
func (r *Neo4jGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jGrant instance
	grant := &neo4jv1alpha1.Neo4jGrant{}
	if err := r.Get(ctx, req.NamespacedName, grant); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jGrant resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jGrant")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if grant.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, grant)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(grant, GrantFinalizer) {
		controllerutil.AddFinalizer(grant, GrantFinalizer)
		if err := r.Update(ctx, grant); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      grant.Spec.ClusterRef,
		Namespace: grant.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			r.updateGrantStatus(ctx, grant, metav1.ConditionFalse, "ClusterNotFound",
				fmt.Sprintf("Referenced cluster %s not found", grant.Spec.ClusterRef))
			r.Recorder.Eventf(grant, "Warning", "ClusterNotFound",
				"Referenced cluster %s not found", grant.Spec.ClusterRef)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		logger.Error(err, "Failed to get referenced cluster")
		return ctrl.Result{}, err
	}

	// Check if cluster is ready
	if !r.isClusterReady(cluster) {
		r.updateGrantStatus(ctx, grant, metav1.ConditionFalse, "ClusterNotReady",
			"Referenced cluster is not ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client")
		r.updateGrantStatus(ctx, grant, metav1.ConditionFalse, "ConnectionFailed",
			"Failed to connect to Neo4j cluster")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			logger.Error(err, "Failed to close Neo4j client")
		}
	}()

	// Apply grants
	if err := r.applyGrants(ctx, neo4jClient, grant); err != nil {
		logger.Error(err, "Failed to apply grants")
		r.updateGrantStatus(ctx, grant, metav1.ConditionFalse, "GrantApplicationFailed",
			fmt.Sprintf("Failed to apply grants: %v", err))
		r.Recorder.Eventf(grant, "Warning", "GrantApplicationFailed",
			"Failed to apply grants: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to ready
	r.updateGrantStatus(ctx, grant, metav1.ConditionTrue, "GrantReady",
		"Grants applied successfully")
	r.Recorder.Event(grant, "Normal", "GrantReady", "Grants are applied successfully")

	logger.Info("Successfully reconciled Neo4jGrant")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jGrantReconciler) handleDeletion(ctx context.Context, grant *neo4jv1alpha1.Neo4jGrant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(grant, GrantFinalizer) {
		return ctrl.Result{}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      grant.Spec.ClusterRef,
		Namespace: grant.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			// Cluster is gone, remove finalizer
			controllerutil.RemoveFinalizer(grant, GrantFinalizer)
			return ctrl.Result{}, r.Update(ctx, grant)
		}
		logger.Error(err, "Failed to get referenced cluster during deletion")
		return ctrl.Result{}, err
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client during deletion")
		// If we can't connect, assume grants are already gone
		controllerutil.RemoveFinalizer(grant, GrantFinalizer)
		return ctrl.Result{}, r.Update(ctx, grant)
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			logger.Error(err, "Failed to close Neo4j client")
		}
	}()

	// Revoke grants
	if err := r.revokeGrants(ctx, neo4jClient, grant); err != nil {
		logger.Error(err, "Failed to revoke grants")
		r.Recorder.Eventf(grant, "Warning", "GrantRevokeAailed",
			"Failed to revoke grants: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	r.Recorder.Event(grant, "Normal", "GrantRevoked", "Grants revoked successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(grant, GrantFinalizer)
	return ctrl.Result{}, r.Update(ctx, grant)
}

func (r *Neo4jGrantReconciler) applyGrants(ctx context.Context, client *neo4j.Client, grant *neo4jv1alpha1.Neo4jGrant) error {
	// Apply each privilege rule
	for _, rule := range grant.Spec.PrivilegeRules {
		statement := r.buildGrantStatement(rule)
		if err := client.ExecutePrivilegeStatement(ctx, statement); err != nil {
			return fmt.Errorf("failed to execute grant statement: %w", err)
		}
	}

	return nil
}

func (r *Neo4jGrantReconciler) revokeGrants(_ context.Context, client *neo4j.Client, grant *neo4jv1alpha1.Neo4jGrant) error {
	// Revoke each privilege rule
	for _, rule := range grant.Spec.PrivilegeRules {
		statement := r.buildRevokeStatement(rule)
		if err := client.ExecutePrivilegeStatement(context.Background(), statement); err != nil {
			// Log error but continue with other revocations
			fmt.Printf("Failed to revoke privilege: %v", err)
		}
	}

	return nil
}

func (r *Neo4jGrantReconciler) buildGrantStatement(rule neo4jv1alpha1.PrivilegeRule) string {
	var statement string

	action := "GRANT"
	if rule.Action != "" {
		action = rule.Action
	}

	if rule.RoleName != "" {
		statement = fmt.Sprintf("%s %s ON %s TO `%s`", action, rule.Privilege, rule.Resource, rule.RoleName)
	} else if rule.UserName != "" {
		statement = fmt.Sprintf("%s %s ON %s TO `%s`", action, rule.Privilege, rule.Resource, rule.UserName)
	}

	// Add graph specification if provided
	if rule.Graph != "" {
		statement = fmt.Sprintf("%s GRAPH %s", statement, rule.Graph)
	}

	// Add qualifier if provided
	if rule.Qualifier != "" {
		statement = fmt.Sprintf("%s %s", statement, rule.Qualifier)
	}

	return statement
}

func (r *Neo4jGrantReconciler) buildRevokeStatement(rule neo4jv1alpha1.PrivilegeRule) string {
	var statement string

	if rule.RoleName != "" {
		statement = fmt.Sprintf("REVOKE %s ON %s FROM `%s`", rule.Privilege, rule.Resource, rule.RoleName)
	} else if rule.UserName != "" {
		statement = fmt.Sprintf("REVOKE %s ON %s FROM `%s`", rule.Privilege, rule.Resource, rule.UserName)
	}

	// Add graph specification if provided
	if rule.Graph != "" {
		statement = fmt.Sprintf("%s GRAPH %s", statement, rule.Graph)
	}

	// Add qualifier if provided
	if rule.Qualifier != "" {
		statement = fmt.Sprintf("%s %s", statement, rule.Qualifier)
	}

	return statement
}

func (r *Neo4jGrantReconciler) createNeo4jClient(_ context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	return neo4j.NewClientForEnterprise(cluster, r.Client, "admin-secret")
}

func (r *Neo4jGrantReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Neo4jGrantReconciler) updateGrantStatus(ctx context.Context, grant *neo4jv1alpha1.Neo4jGrant,
	status metav1.ConditionStatus, reason, message string) {

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	// Update or add condition
	updated := false
	for i, existingCondition := range grant.Status.Conditions {
		if existingCondition.Type == condition.Type {
			grant.Status.Conditions[i] = condition
			updated = true
			break
		}
	}
	if !updated {
		grant.Status.Conditions = append(grant.Status.Conditions, condition)
	}

	grant.Status.ObservedGeneration = grant.Generation
	if err := r.Status().Update(ctx, grant); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update grant status")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jGrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jGrant{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
