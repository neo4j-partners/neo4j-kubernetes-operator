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
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
)

// Neo4jDatabaseReconciler reconciles a Neo4jDatabase object
type Neo4jDatabaseReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
}

const (
	// DatabaseFinalizer is the finalizer for Neo4j database resources
	DatabaseFinalizer = "neo4j.com/database-finalizer"
)

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jdatabases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jdatabases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jdatabases/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation of Neo4jDatabase resources
func (r *Neo4jDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jDatabase instance
	database := &neo4jv1alpha1.Neo4jDatabase{}
	if err := r.Get(ctx, req.NamespacedName, database); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jDatabase resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jDatabase")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if database.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, database)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(database, DatabaseFinalizer) {
		controllerutil.AddFinalizer(database, DatabaseFinalizer)
		if err := r.Update(ctx, database); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      database.Spec.ClusterRef,
		Namespace: database.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, "ClusterNotFound",
				fmt.Sprintf("Referenced cluster %s not found", database.Spec.ClusterRef))
			r.Recorder.Eventf(database, "Warning", "ClusterNotFound",
				"Referenced cluster %s not found", database.Spec.ClusterRef)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		logger.Error(err, "Failed to get referenced cluster")
		return ctrl.Result{}, err
	}

	// Check if cluster is ready
	if !r.isClusterReady(cluster) {
		r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, "ClusterNotReady",
			"Referenced cluster is not ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client")
		r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, "ConnectionFailed",
			"Failed to connect to Neo4j cluster")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			logger.Error(err, "Failed to close Neo4j client")
		}
	}()

	// Ensure database exists
	if err := r.ensureDatabase(ctx, neo4jClient, database); err != nil {
		logger.Error(err, "Failed to ensure database")
		r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, "CreationFailed",
			fmt.Sprintf("Failed to create database: %v", err))
		r.Recorder.Eventf(database, "Warning", "CreationFailed",
			"Failed to create database: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Import initial data if specified
	if database.Spec.InitialData != nil && database.Status.DataImported == nil {
		if err := r.importInitialData(ctx, neo4jClient, database); err != nil {
			logger.Error(err, "Failed to import initial data")
			r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, "DataImportFailed",
				fmt.Sprintf("Failed to import initial data: %v", err))
			r.Recorder.Eventf(database, "Warning", "DataImportFailed",
				"Failed to import initial data: %v", err)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}

		// Mark data as imported
		imported := true
		database.Status.DataImported = &imported
		if err := r.Status().Update(ctx, database); err != nil {
			logger.Error(err, "Failed to update data import status")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(database, "Normal", "DataImported", "Initial data imported successfully")
	}

	// Update status to ready
	r.updateDatabaseStatus(ctx, database, metav1.ConditionTrue, "DatabaseReady",
		"Database is ready and available")
	r.Recorder.Event(database, "Normal", "DatabaseReady", "Database is ready and available")

	logger.Info("Successfully reconciled Neo4jDatabase")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jDatabaseReconciler) handleDeletion(ctx context.Context, database *neo4jv1alpha1.Neo4jDatabase) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(database, DatabaseFinalizer) {
		logger.Info("Finalizer not present, nothing to do", "finalizers", database.Finalizers, "deletionTimestamp", database.DeletionTimestamp)
		return ctrl.Result{}, nil
	}

	logger.Info("Starting deletion handler", "finalizers", database.Finalizers, "deletionTimestamp", database.DeletionTimestamp)

	// Get referenced cluster
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      database.Spec.ClusterRef,
		Namespace: database.Namespace,
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Referenced cluster not found, removing finalizer", "clusterKey", clusterKey)
			controllerutil.RemoveFinalizer(database, DatabaseFinalizer)
			err := r.Update(ctx, database)
			if err != nil {
				logger.Error(err, "Failed to update database after removing finalizer")
			}
			return ctrl.Result{}, err
		}
		logger.Error(err, "Failed to get referenced cluster during deletion")
		return ctrl.Result{}, err
	}

	// Create Neo4j client
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client during deletion")
		// If we can't connect, assume database is already gone
		controllerutil.RemoveFinalizer(database, DatabaseFinalizer)
		err := r.Update(ctx, database)
		if err != nil {
			logger.Error(err, "Failed to update database after removing finalizer")
		}
		return ctrl.Result{}, err
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			logger.Error(err, "Failed to close Neo4j client")
		}
	}()

	// Drop database
	if err := neo4jClient.DropDatabase(ctx, database.Spec.Name); err != nil {
		logger.Error(err, "Failed to drop database")
		r.Recorder.Eventf(database, "Warning", "DeletionFailed",
			"Failed to drop database: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	r.Recorder.Event(database, "Normal", "DatabaseDeleted", "Database dropped successfully")

	logger.Info("Removing finalizer from database", "finalizers", database.Finalizers, "deletionTimestamp", database.DeletionTimestamp)
	controllerutil.RemoveFinalizer(database, DatabaseFinalizer)
	err = r.Update(ctx, database)
	if err != nil {
		logger.Error(err, "Failed to update database after removing finalizer", "finalizers", database.Finalizers, "deletionTimestamp", database.DeletionTimestamp)
		return ctrl.Result{}, err
	}
	logger.Info("Successfully removed finalizer and updated database", "finalizers", database.Finalizers, "deletionTimestamp", database.DeletionTimestamp)
	return ctrl.Result{}, nil
}

func (r *Neo4jDatabaseReconciler) ensureDatabase(ctx context.Context, client *neo4j.Client, database *neo4jv1alpha1.Neo4jDatabase) error {
	// Check if database exists
	exists, err := client.DatabaseExists(ctx, database.Spec.Name)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if !exists {
		// Create database with options
		if err := client.CreateDatabase(ctx, database.Spec.Name, database.Spec.Options); err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
	}

	return nil
}

func (r *Neo4jDatabaseReconciler) importInitialData(ctx context.Context, client *neo4j.Client, database *neo4jv1alpha1.Neo4jDatabase) error {
	for _, statement := range database.Spec.InitialData.CypherStatements {
		if err := client.ExecuteCypher(ctx, database.Spec.Name, statement); err != nil {
			return fmt.Errorf("failed to execute cypher statement: %w", err)
		}
	}
	return nil
}

func (r *Neo4jDatabaseReconciler) createNeo4jClient(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	// Use the enterprise client method
	return neo4j.NewClientForEnterprise(cluster, r.Client, "neo4j-admin-secret")
}

func (r *Neo4jDatabaseReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Neo4jDatabaseReconciler) updateDatabaseStatus(ctx context.Context, database *neo4jv1alpha1.Neo4jDatabase, status metav1.ConditionStatus, reason, message string) {
	update := func() error {
		latest := &neo4jv1alpha1.Neo4jDatabase{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(database), latest); err != nil {
			return err
		}
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             status,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
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
		latest.Status.ObservedGeneration = latest.Generation
		return r.Status().Update(ctx, latest)
	}
	err := retry.RetryOnConflict(retry.DefaultBackoff, update)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update database status")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jDatabase{}).
		Owns(&neo4jv1alpha1.Neo4jEnterpriseCluster{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		}).
		Complete(r)
}
