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
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
	corev1 "k8s.io/api/core/v1"
)

// Neo4jDatabaseReconciler reconciles a Neo4jDatabase object
type Neo4jDatabaseReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
	DatabaseValidator       *validation.DatabaseValidator
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

	// Track reconciliation start time for monitoring
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		if duration > 30*time.Second {
			logger.Info("Long reconciliation detected", "duration", duration, "database", req.NamespacedName)
		}
	}()

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

	// Validate database configuration
	if r.DatabaseValidator != nil {
		validationResult := r.DatabaseValidator.Validate(ctx, database)

		// Log and record warnings
		for _, warning := range validationResult.Warnings {
			logger.Info("Database validation warning", "warning", warning)
			r.Recorder.Eventf(database, corev1.EventTypeWarning, EventReasonValidationWarning, warning)
		}

		// Handle validation errors
		if len(validationResult.Errors) > 0 {
			errMessages := make([]string, len(validationResult.Errors))
			for i, err := range validationResult.Errors {
				errMessages[i] = err.Error()
			}
			message := fmt.Sprintf("Database validation failed: %v", errMessages)
			logger.Error(nil, message)
			r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, EventReasonValidationFailed, message)
			r.Recorder.Eventf(database, corev1.EventTypeWarning, EventReasonValidationFailed, message)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
	}

	// Get referenced cluster or standalone
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{
		Name:      database.Spec.ClusterRef,
		Namespace: database.Namespace,
	}

	clusterErr := r.Get(ctx, clusterKey, cluster)
	var standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone
	var isStandalone bool

	// If cluster not found, try to get standalone
	if errors.IsNotFound(clusterErr) {
		standalone = &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
		standaloneKey := types.NamespacedName{
			Name:      database.Spec.ClusterRef,
			Namespace: database.Namespace,
		}

		if err := r.Get(ctx, standaloneKey, standalone); err != nil {
			if errors.IsNotFound(err) {
				r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, EventReasonClusterNotFound,
					fmt.Sprintf("Referenced cluster %s not found", database.Spec.ClusterRef))
				r.Recorder.Eventf(database, corev1.EventTypeWarning, EventReasonClusterNotFound,
					"Referenced cluster %s not found", database.Spec.ClusterRef)
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			logger.Error(err, "Failed to get referenced standalone")
			return ctrl.Result{}, err
		}
		isStandalone = true
	} else if clusterErr != nil {
		logger.Error(clusterErr, "Failed to get referenced cluster")
		return ctrl.Result{}, clusterErr
	}

	// Check if cluster/standalone is ready
	var clusterReady bool
	if isStandalone {
		clusterReady = r.isStandaloneReady(standalone)
	} else {
		clusterReady = r.isClusterReady(cluster)
	}

	if !clusterReady {
		r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, EventReasonClusterNotReady,
			"Referenced cluster is not ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client with retry for transient connection issues
	var neo4jClient *neo4j.Client
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		// Retry on connection errors
		return strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "timeout")
	}, func() error {
		var clientErr error
		if isStandalone {
			neo4jClient, clientErr = r.createNeo4jClientForStandalone(ctx, standalone)
		} else {
			neo4jClient, clientErr = r.createNeo4jClient(ctx, cluster)
		}
		return clientErr
	})

	if err != nil {
		logger.Error(err, "Failed to create Neo4j client after retries")
		r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, EventReasonConnectionFailed,
			"Failed to connect to Neo4j cluster")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			logger.Error(err, "Failed to close Neo4j client")
		}
	}()

	// Ensure database exists (with seed URI support)
	logger.Info("Starting database creation/verification", "database", database.Spec.Name, "wait", database.Spec.Wait, "topology", database.Spec.Topology)
	dbCreateStart := time.Now()
	if err := r.ensureDatabase(ctx, neo4jClient, database); err != nil {
		duration := time.Since(dbCreateStart)
		logger.Error(err, "Failed to ensure database", "database", database.Spec.Name, "duration", duration)
		r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, EventReasonCreationFailed,
			fmt.Sprintf("Failed to create database: %v", err))
		r.Recorder.Eventf(database, corev1.EventTypeWarning, EventReasonCreationFailed,
			"Failed to create database: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	duration := time.Since(dbCreateStart)
	logger.Info("Database creation/verification completed successfully", "database", database.Spec.Name, "duration", duration)

	// Import initial data if specified (skip if using seed URI since data comes from the seed)
	if database.Spec.InitialData != nil && database.Spec.SeedURI == "" && database.Status.DataImported == nil {
		if err := r.importInitialData(ctx, neo4jClient, database); err != nil {
			logger.Error(err, "Failed to import initial data")
			r.updateDatabaseStatus(ctx, database, metav1.ConditionFalse, EventReasonDataImportFailed,
				fmt.Sprintf("Failed to import initial data: %v", err))
			r.Recorder.Eventf(database, corev1.EventTypeWarning, EventReasonDataImportFailed,
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
		r.Recorder.Event(database, corev1.EventTypeNormal, EventReasonDataImported, "Initial data imported successfully")
	} else if database.Spec.SeedURI != "" && database.Status.DataImported == nil {
		// Mark data as imported for seed URI databases (data comes from the seed)
		imported := true
		database.Status.DataImported = &imported
		if err := r.Status().Update(ctx, database); err != nil {
			logger.Error(err, "Failed to update data import status for seeded database")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(database, corev1.EventTypeNormal, EventReasonDataSeeded, "Database seeded from URI successfully")
	}

	// Update status to ready
	r.updateDatabaseStatus(ctx, database, metav1.ConditionTrue, EventReasonDatabaseReady,
		"Database is ready and available")
	r.Recorder.Event(database, corev1.EventTypeNormal, EventReasonDatabaseReady, "Database is ready and available")

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
		r.Recorder.Eventf(database, corev1.EventTypeWarning, EventReasonDeletionFailed,
			"Failed to drop database: %v", err)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	r.Recorder.Event(database, corev1.EventTypeNormal, EventReasonDatabaseDeleted, "Database dropped successfully")

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
	logger := log.FromContext(ctx)

	// Check if database exists
	exists, err := client.DatabaseExists(ctx, database.Spec.Name)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if !exists {
		// Prepare cloud credentials if using seed URI with explicit credentials
		if database.Spec.SeedURI != "" && database.Spec.SeedCredentials != nil {
			if err := client.PrepareCloudCredentials(ctx, r.Client, database); err != nil {
				return fmt.Errorf("failed to prepare cloud credentials: %w", err)
			}
		}

		// Determine which creation method to use based on seed URI
		if database.Spec.SeedURI != "" {
			// Create database from seed URI
			if database.Spec.Topology != nil {
				logger.Info("Creating database from seed URI with topology",
					"database", database.Spec.Name,
					"seedURI", database.Spec.SeedURI,
					"primaries", database.Spec.Topology.Primaries,
					"secondaries", database.Spec.Topology.Secondaries)

				err = client.CreateDatabaseFromSeedURIWithTopology(
					ctx,
					database.Spec.Name,
					database.Spec.SeedURI,
					database.Spec.Topology.Primaries,
					database.Spec.Topology.Secondaries,
					database.Spec.SeedConfig,
					database.Spec.Options,
					database.Spec.Wait,
					database.Spec.IfNotExists,
					database.Spec.DefaultCypherLanguage,
				)
			} else {
				logger.Info("Creating database from seed URI",
					"database", database.Spec.Name,
					"seedURI", database.Spec.SeedURI)

				err = client.CreateDatabaseFromSeedURI(
					ctx,
					database.Spec.Name,
					database.Spec.SeedURI,
					database.Spec.SeedConfig,
					database.Spec.Options,
					database.Spec.Wait,
					database.Spec.IfNotExists,
					database.Spec.DefaultCypherLanguage,
				)
			}
		} else {
			// Standard database creation without seed URI
			if database.Spec.Topology != nil {
				logger.Info("Creating database with topology",
					"database", database.Spec.Name,
					"primaries", database.Spec.Topology.Primaries,
					"secondaries", database.Spec.Topology.Secondaries)

				logger.Info("Creating database with topology",
					"database", database.Spec.Name,
					"primaries", database.Spec.Topology.Primaries,
					"secondaries", database.Spec.Topology.Secondaries,
					"wait", database.Spec.Wait,
					"timeout", "300s")

				err = client.CreateDatabaseWithTopology(
					ctx,
					database.Spec.Name,
					database.Spec.Topology.Primaries,
					database.Spec.Topology.Secondaries,
					database.Spec.Options,
					database.Spec.Wait,
					database.Spec.IfNotExists,
					database.Spec.DefaultCypherLanguage,
				)
			} else {
				// Create database without topology
				logger.Info("Creating database",
					"database", database.Spec.Name,
					"wait", database.Spec.Wait,
					"ifNotExists", database.Spec.IfNotExists)

				err = client.CreateDatabase(
					ctx,
					database.Spec.Name,
					database.Spec.Options,
					database.Spec.Wait,
					database.Spec.IfNotExists,
				)
			}
		}

		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}

		// Record appropriate success event based on creation method
		if database.Spec.SeedURI != "" {
			r.Recorder.Eventf(database, corev1.EventTypeNormal, EventReasonDatabaseCreatedSeed,
				"Database %s created successfully from seed URI", database.Spec.Name)
			logger.Info("Database created successfully from seed URI",
				"database", database.Spec.Name, "seedURI", database.Spec.SeedURI)
		} else {
			logger.Info("Database created successfully", "database", database.Spec.Name)
		}
	} else {
		logger.Info("Database already exists", "database", database.Spec.Name)
	}

	// Always update database state and servers after creation or verification
	// Check and update database state in status
	state, err := client.GetDatabaseState(ctx, database.Spec.Name)
	if err != nil {
		logger.Error(err, "Failed to get database state")
	} else {
		database.Status.State = state
	}

	// Get servers hosting the database
	servers, err := client.GetDatabaseServers(ctx, database.Spec.Name)
	if err != nil {
		logger.Error(err, "Failed to get database servers")
	} else {
		database.Status.Servers = servers
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
	return neo4j.NewClientForEnterprise(cluster, r.Client, cluster.Spec.Auth.AdminSecret)
}

func (r *Neo4jDatabaseReconciler) createNeo4jClientForStandalone(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) (*neo4j.Client, error) {
	// Use the enterprise client method for standalone
	return neo4j.NewClientForEnterpriseStandalone(standalone, r.Client, standalone.Spec.Auth.AdminSecret)
}

func (r *Neo4jDatabaseReconciler) isClusterReady(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Neo4jDatabaseReconciler) isStandaloneReady(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) bool {
	// Standalone uses a simple ready boolean field, not conditions array
	return standalone.Status.Ready
}

func (r *Neo4jDatabaseReconciler) updateDatabaseStatus(ctx context.Context, database *neo4jv1alpha1.Neo4jDatabase, status metav1.ConditionStatus, reason, message string) {
	update := func() error {
		latest := &neo4jv1alpha1.Neo4jDatabase{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(database), latest); err != nil {
			return err
		}
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, status, reason, message)

		// Set Phase field based on condition status for API consistency
		switch status {
		case metav1.ConditionTrue:
			latest.Status.Phase = "Ready"
			// Set creation time if this is the first time the database becomes ready
			if latest.Status.CreationTime == nil && reason == EventReasonDatabaseReady {
				now := metav1.Now()
				latest.Status.CreationTime = &now
			}
		case metav1.ConditionFalse:
			// Set phase based on the reason for failure
			switch reason {
			case EventReasonValidationFailed:
				latest.Status.Phase = EventReasonValidationFailed
			case EventReasonClusterNotFound, EventReasonClusterNotReady:
				latest.Status.Phase = "Pending"
			case EventReasonConnectionFailed, EventReasonCreationFailed, EventReasonDataImportFailed:
				latest.Status.Phase = "Failed"
			default:
				latest.Status.Phase = "Unknown"
			}
		case metav1.ConditionUnknown:
			latest.Status.Phase = "Unknown"
		}

		// Also update the message field for quick access
		latest.Status.Message = message
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
