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

	corev1 "k8s.io/api/core/v1"
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

// Neo4jUserReconciler reconciles a Neo4jUser object
type Neo4jUserReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
}

const (
	// UserFinalizer is the finalizer for Neo4j user resources
	UserFinalizer = "neo4j.com/user-finalizer"
)

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jusers/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation of Neo4jUser resources
func (r *Neo4jUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jUser instance
	user := &neo4jv1alpha1.Neo4jUser{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jUser resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jUser")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if user.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, user)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(user, UserFinalizer) {
		controllerutil.AddFinalizer(user, UserFinalizer)
		if err := r.Update(ctx, user); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      user.Spec.ClusterRef,
		Namespace: user.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			r.updateUserStatus(ctx, user, metav1.ConditionFalse, "ClusterNotFound",
				fmt.Sprintf("Referenced cluster %s not found", user.Spec.ClusterRef))
			r.Recorder.Eventf(user, "Warning", "ClusterNotFound",
				"Referenced cluster %s not found", user.Spec.ClusterRef)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		logger.Error(err, "Failed to get referenced cluster")
		return ctrl.Result{}, err
	}

	// Check if cluster is ready
	if !r.isClusterReady(cluster) {
		r.updateUserStatus(ctx, user, metav1.ConditionFalse, "ClusterNotReady",
			"Referenced cluster is not ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Get password from secret
	password, err := r.getPasswordFromSecret(ctx, user)
	if err != nil {
		logger.Error(err, "Failed to get password from secret")
		r.updateUserStatus(ctx, user, metav1.ConditionFalse, "PasswordSecretError",
			fmt.Sprintf("Failed to get password: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client")
		r.updateUserStatus(ctx, user, metav1.ConditionFalse, "ConnectionFailed",
			"Failed to connect to Neo4j cluster")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer neo4jClient.Close()

	// Ensure user exists with correct properties
	if err := r.ensureUser(ctx, neo4jClient, user, password); err != nil {
		logger.Error(err, "Failed to ensure user")
		r.updateUserStatus(ctx, user, metav1.ConditionFalse, "UserCreationFailed",
			fmt.Sprintf("Failed to create/update user: %v", err))
		r.Recorder.Eventf(user, "Warning", "UserCreationFailed",
			"Failed to create/update user: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Assign roles to user
	if err := r.assignRolesToUser(ctx, neo4jClient, user); err != nil {
		logger.Error(err, "Failed to assign roles to user")
		r.updateUserStatus(ctx, user, metav1.ConditionFalse, "RoleAssignmentFailed",
			fmt.Sprintf("Failed to assign roles: %v", err))
		r.Recorder.Eventf(user, "Warning", "RoleAssignmentFailed",
			"Failed to assign roles: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to ready
	r.updateUserStatus(ctx, user, metav1.ConditionTrue, "UserReady",
		"User is created and configured successfully")
	r.Recorder.Event(user, "Normal", "UserReady", "User is ready and available")

	logger.Info("Successfully reconciled Neo4jUser")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jUserReconciler) handleDeletion(ctx context.Context, user *neo4jv1alpha1.Neo4jUser) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(user, UserFinalizer) {
		return ctrl.Result{}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      user.Spec.ClusterRef,
		Namespace: user.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			// Cluster is gone, remove finalizer
			controllerutil.RemoveFinalizer(user, UserFinalizer)
			return ctrl.Result{}, r.Update(ctx, user)
		}
		logger.Error(err, "Failed to get referenced cluster during deletion")
		return ctrl.Result{}, err
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client during deletion")
		// If we can't connect, assume user is already gone
		controllerutil.RemoveFinalizer(user, UserFinalizer)
		return ctrl.Result{}, r.Update(ctx, user)
	}
	defer neo4jClient.Close()

	// Drop user
	if err := neo4jClient.DropUser(ctx, user.Spec.Username); err != nil {
		logger.Error(err, "Failed to drop user")
		r.Recorder.Eventf(user, "Warning", "UserDeletionFailed",
			"Failed to drop user: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	r.Recorder.Event(user, "Normal", "UserDeleted", "User dropped successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(user, UserFinalizer)
	return ctrl.Result{}, r.Update(ctx, user)
}

func (r *Neo4jUserReconciler) ensureUser(ctx context.Context, client *neo4j.Client, user *neo4jv1alpha1.Neo4jUser, password string) error {
	// Create or update user
	if err := client.CreateUser(ctx, user.Spec.Username, password, user.Spec.MustChangePassword); err != nil {
		return fmt.Errorf("failed to create/update user: %w", err)
	}

	// Update user properties if specified
	if len(user.Spec.Properties) > 0 {
		for key, value := range user.Spec.Properties {
			if err := client.SetUserProperty(ctx, user.Spec.Username, key, value); err != nil {
				return fmt.Errorf("failed to set user property %s: %w", key, err)
			}
		}
	}

	// Handle suspension
	if user.Spec.Suspended {
		if err := client.SuspendUser(ctx, user.Spec.Username); err != nil {
			return fmt.Errorf("failed to suspend user: %w", err)
		}
	} else {
		if err := client.ActivateUser(ctx, user.Spec.Username); err != nil {
			return fmt.Errorf("failed to activate user: %w", err)
		}
	}

	return nil
}

func (r *Neo4jUserReconciler) assignRolesToUser(ctx context.Context, client *neo4j.Client, user *neo4jv1alpha1.Neo4jUser) error {
	// Get current roles for the user
	currentRoles, err := client.GetUserRoles(ctx, user.Spec.Username)
	if err != nil {
		return fmt.Errorf("failed to get current user roles: %w", err)
	}

	// Convert to maps for easier comparison
	currentRoleMap := make(map[string]bool)
	for _, role := range currentRoles {
		currentRoleMap[role] = true
	}

	desiredRoleMap := make(map[string]bool)
	for _, role := range user.Spec.Roles {
		desiredRoleMap[role] = true
	}

	// Grant new roles
	for role := range desiredRoleMap {
		if !currentRoleMap[role] {
			if err := client.GrantRoleToUser(ctx, role, user.Spec.Username); err != nil {
				return fmt.Errorf("failed to grant role %s to user: %w", role, err)
			}
		}
	}

	// Revoke removed roles
	for role := range currentRoleMap {
		if !desiredRoleMap[role] && role != "PUBLIC" { // Don't revoke PUBLIC role
			if err := client.RevokeRoleFromUser(ctx, role, user.Spec.Username); err != nil {
				return fmt.Errorf("failed to revoke role %s from user: %w", role, err)
			}
		}
	}

	return nil
}

func (r *Neo4jUserReconciler) getPasswordFromSecret(ctx context.Context, user *neo4jv1alpha1.Neo4jUser) (string, error) {
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      user.Spec.PasswordSecret.Name,
		Namespace: user.Namespace,
	}

	if err := r.Get(ctx, secretKey, secret); err != nil {
		return "", fmt.Errorf("failed to get password secret: %w", err)
	}

	key := user.Spec.PasswordSecret.Key
	if key == "" {
		key = "password"
	}

	passwordBytes, exists := secret.Data[key]
	if !exists {
		return "", fmt.Errorf("password key %s not found in secret", key)
	}

	return string(passwordBytes), nil
}

func (r *Neo4jUserReconciler) createNeo4jClient(_ context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	return neo4j.NewClientForEnterprise(cluster, r.Client, "admin-secret")
}

func (r *Neo4jUserReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Neo4jUserReconciler) updateUserStatus(ctx context.Context, user *neo4jv1alpha1.Neo4jUser,
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
	for i, existingCondition := range user.Status.Conditions {
		if existingCondition.Type == condition.Type {
			user.Status.Conditions[i] = condition
			updated = true
			break
		}
	}
	if !updated {
		user.Status.Conditions = append(user.Status.Conditions, condition)
	}

	user.Status.ObservedGeneration = user.Generation
	r.Status().Update(ctx, user)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jUser{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
