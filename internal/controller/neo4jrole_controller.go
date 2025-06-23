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

// Neo4jRoleReconciler reconciles a Neo4jRole object
type Neo4jRoleReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
}

const (
	// RoleFinalizer is the finalizer for Neo4j role resources
	RoleFinalizer = "neo4j.com/role-finalizer"
)

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jroles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jroles/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation of Neo4jRole resources
func (r *Neo4jRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jRole instance
	role := &neo4jv1alpha1.Neo4jRole{}
	if err := r.Get(ctx, req.NamespacedName, role); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jRole resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jRole")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if role.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, role)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(role, RoleFinalizer) {
		controllerutil.AddFinalizer(role, RoleFinalizer)
		if err := r.Update(ctx, role); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      role.Spec.ClusterRef,
		Namespace: role.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			r.updateRoleStatus(ctx, role, metav1.ConditionFalse, "ClusterNotFound",
				fmt.Sprintf("Referenced cluster %s not found", role.Spec.ClusterRef))
			r.Recorder.Eventf(role, "Warning", "ClusterNotFound",
				"Referenced cluster %s not found", role.Spec.ClusterRef)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		logger.Error(err, "Failed to get referenced cluster")
		return ctrl.Result{}, err
	}

	// Check if cluster is ready
	if !r.isClusterReady(cluster) {
		r.updateRoleStatus(ctx, role, metav1.ConditionFalse, "ClusterNotReady",
			"Referenced cluster is not ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client")
		r.updateRoleStatus(ctx, role, metav1.ConditionFalse, "ConnectionFailed",
			"Failed to connect to Neo4j cluster")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			log.FromContext(ctx).Error(err, "Failed to close Neo4j client")
		}
	}()

	// Ensure role exists
	if err := r.ensureRole(ctx, neo4jClient, role); err != nil {
		logger.Error(err, "Failed to ensure role")
		r.updateRoleStatus(ctx, role, metav1.ConditionFalse, "RoleCreationFailed",
			fmt.Sprintf("Failed to create/update role: %v", err))
		r.Recorder.Eventf(role, "Warning", "RoleCreationFailed",
			"Failed to create/update role: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Configure role privileges
	if err := r.configurePrivileges(ctx, neo4jClient, role); err != nil {
		logger.Error(err, "Failed to configure role privileges")
		r.updateRoleStatus(ctx, role, metav1.ConditionFalse, "PrivilegeConfigurationFailed",
			fmt.Sprintf("Failed to configure privileges: %v", err))
		r.Recorder.Eventf(role, "Warning", "PrivilegeConfigurationFailed",
			"Failed to configure privileges: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to ready
	r.updateRoleStatus(ctx, role, metav1.ConditionTrue, "RoleReady",
		"Role is created and configured successfully")
	r.Recorder.Event(role, "Normal", "RoleReady", "Role is ready and available")

	// Update status
	if err := r.Status().Update(ctx, role); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update role status")
	}

	logger.Info("Successfully reconciled Neo4jRole")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jRoleReconciler) handleDeletion(ctx context.Context, role *neo4jv1alpha1.Neo4jRole) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(role, RoleFinalizer) {
		return ctrl.Result{}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      role.Spec.ClusterRef,
		Namespace: role.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			// Cluster is gone, remove finalizer
			controllerutil.RemoveFinalizer(role, RoleFinalizer)
			return ctrl.Result{}, r.Update(ctx, role)
		}
		logger.Error(err, "Failed to get referenced cluster during deletion")
		return ctrl.Result{}, err
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client during deletion")
		// If we can't connect, assume role is already gone
		controllerutil.RemoveFinalizer(role, RoleFinalizer)
		return ctrl.Result{}, r.Update(ctx, role)
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			log.FromContext(ctx).Error(err, "Failed to close Neo4j client")
		}
	}()

	// Drop role
	if err := neo4jClient.DropRole(ctx, role.Name); err != nil {
		logger.Error(err, "Failed to drop role")
		r.Recorder.Eventf(role, "Warning", "RoleDeletionFailed",
			"Failed to drop role: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	r.Recorder.Event(role, "Normal", "RoleDeleted", "Role dropped successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(role, RoleFinalizer)
	return ctrl.Result{}, r.Update(ctx, role)
}

func (r *Neo4jRoleReconciler) ensureRole(ctx context.Context, client *neo4j.Client, role *neo4jv1alpha1.Neo4jRole) error {
	// Create role
	if err := client.CreateRole(ctx, role.Name); err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}

	return nil
}

func (r *Neo4jRoleReconciler) configurePrivileges(ctx context.Context, client *neo4j.Client, role *neo4jv1alpha1.Neo4jRole) error {
	// Execute privilege statements for the role
	for _, privilege := range role.Spec.Privileges {
		statement := r.buildPrivilegeStatement(privilege, role.Name)
		if err := client.ExecutePrivilegeStatement(ctx, statement); err != nil {
			return fmt.Errorf("failed to execute privilege statement: %w", err)
		}
	}

	return nil
}

func (r *Neo4jRoleReconciler) buildPrivilegeStatement(privilege neo4jv1alpha1.RolePrivilegeRule, roleName string) string {
	var statement string

	switch privilege.Action {
	case "GRANT":
		statement = fmt.Sprintf("GRANT %s ON %s TO `%s`", privilege.Privilege, privilege.Resource, roleName)
	case "DENY":
		statement = fmt.Sprintf("DENY %s ON %s TO `%s`", privilege.Privilege, privilege.Resource, roleName)
	case "REVOKE":
		statement = fmt.Sprintf("REVOKE %s ON %s FROM `%s`", privilege.Privilege, privilege.Resource, roleName)
	default:
		statement = fmt.Sprintf("GRANT %s ON %s TO `%s`", privilege.Privilege, privilege.Resource, roleName)
	}

	// Add graph specification if provided
	if privilege.Graph != "" {
		statement = fmt.Sprintf("%s GRAPH %s", statement, privilege.Graph)
	}

	// Add qualifier if provided
	if privilege.Qualifier != "" {
		statement = fmt.Sprintf("%s %s", statement, privilege.Qualifier)
	}

	return statement
}

func (r *Neo4jRoleReconciler) createNeo4jClient(_ context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	return neo4j.NewClientForEnterprise(cluster, r.Client, "admin-secret")
}

func (r *Neo4jRoleReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Neo4jRoleReconciler) updateRoleStatus(ctx context.Context, role *neo4jv1alpha1.Neo4jRole,
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
	for i, existingCondition := range role.Status.Conditions {
		if existingCondition.Type == condition.Type {
			role.Status.Conditions[i] = condition
			updated = true
			break
		}
	}
	if !updated {
		role.Status.Conditions = append(role.Status.Conditions, condition)
	}

	role.Status.ObservedGeneration = role.Generation
	if err := r.Status().Update(ctx, role); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update role status")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jRole{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
