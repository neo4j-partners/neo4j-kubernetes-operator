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

// Package metrics provides Prometheus metrics for the Neo4j Kubernetes Operator
package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	// Metric subsystem
	subsystem = "neo4j_operator"

	// MetricResultSuccess represents a successful operation
	MetricResultSuccess = "success"
	// MetricResultFailure represents a failed operation
	MetricResultFailure = "failure"
	// MetricValueTrue represents a true boolean value as string
	MetricValueTrue = "true"

	// LabelClusterName is the label key for cluster name
	LabelClusterName = "cluster_name"
	// LabelNamespace is the label key for namespace
	LabelNamespace = "namespace"
	// LabelOperation is the label key for operation type
	LabelOperation = "operation"
	// LabelResult is the label key for operation result
	LabelResult = "result"
	// LabelPhase is the label key for operation phase
	LabelPhase = "phase"
	// LabelRole is the label key for node role
	LabelRole = "role"

	// LabelCluster is the label key for cluster name
	LabelCluster = "cluster"
	// LabelNodeType is the label key for node type (primary/secondary)
	LabelNodeType = "node_type"
	// LabelStatus is the label key for operation status
	LabelStatus = "status"
)

var (
	// Tracer for OTEL tracing
	tracer = otel.Tracer("neo4j-operator")

	// Prometheus metrics

	// Cluster metrics
	clusterReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "cluster_replicas_total",
			Help:      "Total number of Neo4j cluster replicas by role",
		},
		[]string{LabelClusterName, LabelNamespace, LabelRole},
	)

	clusterHealthy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "cluster_healthy",
			Help:      "Whether the Neo4j cluster is healthy (1 = healthy, 0 = unhealthy)",
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	clusterPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "cluster_phase",
			Help:      "Current phase of the Neo4j cluster (1 = active phase, 0 = not in this phase)",
		},
		[]string{LabelClusterName, LabelNamespace, LabelPhase},
	)

	splitBrainDetectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "split_brain_detected_total",
			Help:      "Total number of split-brain scenarios detected",
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	// Reconciliation metrics
	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "reconcile_total",
			Help:      "Total number of reconciliation attempts",
		},
		[]string{LabelClusterName, LabelNamespace, LabelOperation, LabelResult},
	)

	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystem,
			Name:      "reconcile_duration_seconds",
			Help:      "Time spent on reconciliation operations",
			Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0},
		},
		[]string{LabelClusterName, LabelNamespace, LabelOperation},
	)

	// Upgrade metrics
	upgradeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "upgrade_total",
			Help:      "Total number of upgrade attempts",
		},
		[]string{LabelClusterName, LabelNamespace, LabelResult},
	)

	upgradeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystem,
			Name:      "upgrade_duration_seconds",
			Help:      "Time spent on upgrade operations",
			Buckets:   []float64{60.0, 300.0, 600.0, 1200.0, 1800.0, 3600.0},
		},
		[]string{LabelClusterName, LabelNamespace, LabelPhase},
	)

	// Backup metrics
	backupTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "backup_total",
			Help:      "Total number of backup attempts",
		},
		[]string{LabelClusterName, LabelNamespace, LabelResult},
	)

	backupDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystem,
			Name:      "backup_duration_seconds",
			Help:      "Time spent on backup operations",
			Buckets:   []float64{60.0, 300.0, 600.0, 1800.0, 3600.0, 7200.0},
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	backupSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "backup_size_bytes",
			Help:      "Size of the latest backup in bytes",
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	// Cypher execution metrics
	cypherTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "cypher_executions_total",
			Help:      "Total number of Cypher statement executions",
		},
		[]string{LabelClusterName, LabelNamespace, LabelOperation, LabelResult},
	)

	cypherDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystem,
			Name:      "cypher_execution_duration_seconds",
			Help:      "Time spent executing Cypher statements",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
		[]string{LabelClusterName, LabelNamespace, LabelOperation},
	)

	// Security metrics
	securityOperationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "security_operations_total",
			Help:      "Total number of security operations (user, role, grant)",
		},
		[]string{LabelClusterName, LabelNamespace, LabelOperation, LabelResult},
	)

	// Resource version conflict metrics
	resourceVersionConflicts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "resource_version_conflicts_total",
			Help:      "Total number of resource version conflicts encountered",
		},
		[]string{"resource_type", LabelNamespace},
	)

	conflictRetryAttempts = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystem,
			Name:      "conflict_retry_attempts",
			Help:      "Number of retry attempts needed to resolve resource version conflicts",
			Buckets:   []float64{1, 2, 3, 4, 5, 10},
		},
		[]string{"resource_type", LabelNamespace},
	)

	conflictRetryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystem,
			Name:      "conflict_retry_duration_seconds",
			Help:      "Time spent retrying due to resource version conflicts",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"resource_type", LabelNamespace},
	)
)

func init() {
	// Register Prometheus metrics
	metrics.Registry.MustRegister(
		clusterReplicas,
		clusterHealthy,
		clusterPhase,
		splitBrainDetectedTotal,
		reconcileTotal,
		reconcileDuration,
		upgradeTotal,
		upgradeDuration,
		backupTotal,
		backupDuration,
		backupSize,
		cypherTotal,
		cypherDuration,
		securityOperationTotal,
		// Resource version conflict metrics
		resourceVersionConflicts,
		conflictRetryAttempts,
		conflictRetryDuration,
		// New feature metrics
		disasterRecoveryStatus,
		failoverTotal,
		replicationLag,
		manualScalerEnabled,
		scaleEventsTotal,
		primaryCount,
		secondaryCount,
		scalingValidationTotal,
		serverHealth,
	)
}

// ReconcileMetrics provides methods for recording reconciliation metrics
type ReconcileMetrics struct {
	clusterName string
	namespace   string
}

// NewReconcileMetrics creates a new ReconcileMetrics instance
func NewReconcileMetrics(clusterName, namespace string) *ReconcileMetrics {
	return &ReconcileMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordReconcile records a reconciliation operation with tracing
func (m *ReconcileMetrics) RecordReconcile(ctx context.Context, operation string, duration time.Duration, success bool) {
	result := MetricResultSuccess
	if !success {
		result = MetricResultFailure
	}

	// Record Prometheus metrics
	reconcileTotal.WithLabelValues(m.clusterName, m.namespace, operation, result).Inc()
	reconcileDuration.WithLabelValues(m.clusterName, m.namespace, operation).Observe(duration.Seconds())

	// Add span attributes if tracing is enabled
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("operation", operation),
			attribute.String("result", result),
			attribute.Float64("duration.seconds", duration.Seconds()),
		)
		span.End()
	}
}

// StartReconcileSpan starts a new tracing span for reconciliation
// The caller is responsible for calling span.End()
func (m *ReconcileMetrics) StartReconcileSpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "reconcile."+operation,
		trace.WithAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("operation", operation),
		))
	return ctx, span
}

// ClusterMetrics provides methods for recording cluster-related metrics
type ClusterMetrics struct {
	clusterName string
	namespace   string
}

// NewClusterMetrics creates a new ClusterMetrics instance
func NewClusterMetrics(clusterName, namespace string) *ClusterMetrics {
	return &ClusterMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordClusterReplicas records the number of replicas by role
func (m *ClusterMetrics) RecordClusterReplicas(primaries, secondaries int32) {
	clusterReplicas.WithLabelValues(m.clusterName, m.namespace, "primary").Set(float64(primaries))
	clusterReplicas.WithLabelValues(m.clusterName, m.namespace, "secondary").Set(float64(secondaries))
}

// RecordClusterHealth records cluster health status
func (m *ClusterMetrics) RecordClusterHealth(healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	clusterHealthy.WithLabelValues(m.clusterName, m.namespace).Set(value)
}

// RecordClusterPhase records the current cluster phase as a labelled gauge.
// It sets 1.0 for the active phase label and 0.0 for all others.
func (m *ClusterMetrics) RecordClusterPhase(phase string) {
	for _, p := range []string{"Pending", "Forming", "Ready", "Failed", "Degraded", "Upgrading"} {
		v := 0.0
		if p == phase {
			v = 1.0
		}
		clusterPhase.WithLabelValues(m.clusterName, m.namespace, p).Set(v)
	}
}

// RecordSplitBrainDetected increments the split-brain detection counter for the given cluster.
func RecordSplitBrainDetected(clusterName, namespace string) {
	splitBrainDetectedTotal.WithLabelValues(clusterName, namespace).Inc()
}

// UpgradeMetrics provides methods for recording upgrade-related metrics
type UpgradeMetrics struct {
	clusterName string
	namespace   string
}

// NewUpgradeMetrics creates a new UpgradeMetrics instance
func NewUpgradeMetrics(clusterName, namespace string) *UpgradeMetrics {
	return &UpgradeMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordUpgrade records an upgrade operation
func (m *UpgradeMetrics) RecordUpgrade(ctx context.Context, success bool, totalDuration time.Duration) {
	result := MetricResultSuccess
	if !success {
		result = MetricResultFailure
	}

	upgradeTotal.WithLabelValues(m.clusterName, m.namespace, result).Inc()

	// Add span attributes
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("result", result),
			attribute.Float64("total.duration.seconds", totalDuration.Seconds()),
		)
		span.End()
	}
}

// RecordUpgradePhase records duration for a specific upgrade phase
func (m *UpgradeMetrics) RecordUpgradePhase(phase string, duration time.Duration) {
	upgradeDuration.WithLabelValues(m.clusterName, m.namespace, phase).Observe(duration.Seconds())
}

// StartUpgradeSpan starts a new tracing span for upgrade operations
// The caller is responsible for calling span.End()
func (m *UpgradeMetrics) StartUpgradeSpan(ctx context.Context, phase string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "upgrade."+phase,
		trace.WithAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("phase", phase),
		))
	return ctx, span
}

// BackupMetrics provides methods for recording backup-related metrics
type BackupMetrics struct {
	clusterName string
	namespace   string
}

// NewBackupMetrics creates a new BackupMetrics instance
func NewBackupMetrics(clusterName, namespace string) *BackupMetrics {
	return &BackupMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordBackup records a backup operation
func (m *BackupMetrics) RecordBackup(ctx context.Context, success bool, duration time.Duration, sizeBytes int64) {
	result := MetricResultSuccess
	if !success {
		result = MetricResultFailure
	}

	backupTotal.WithLabelValues(m.clusterName, m.namespace, result).Inc()
	backupDuration.WithLabelValues(m.clusterName, m.namespace).Observe(duration.Seconds())

	if success && sizeBytes > 0 {
		backupSize.WithLabelValues(m.clusterName, m.namespace).Set(float64(sizeBytes))
	}

	// Add span attributes
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("result", result),
			attribute.Float64("duration.seconds", duration.Seconds()),
			attribute.Int64("size.bytes", sizeBytes),
		)
		span.End()
	}
}

// StartBackupSpan starts a new tracing span for backup operations
func (m *BackupMetrics) StartBackupSpan(ctx context.Context) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "backup",
		trace.WithAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
		))
	return ctx, span
}

// CypherMetrics provides methods for recording Cypher execution metrics
type CypherMetrics struct {
	clusterName string
	namespace   string
}

// NewCypherMetrics creates a new CypherMetrics instance
func NewCypherMetrics(clusterName, namespace string) *CypherMetrics {
	return &CypherMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordCypherExecution records a Cypher statement execution
func (m *CypherMetrics) RecordCypherExecution(ctx context.Context, operation string, duration time.Duration, success bool) {
	result := MetricResultSuccess
	if !success {
		result = MetricResultFailure
	}

	cypherTotal.WithLabelValues(m.clusterName, m.namespace, operation, result).Inc()
	cypherDuration.WithLabelValues(m.clusterName, m.namespace, operation).Observe(duration.Seconds())

	// Add span attributes
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("operation", operation),
			attribute.String("result", result),
			attribute.Float64("duration.seconds", duration.Seconds()),
		)
		span.End()
	}
}

// StartCypherSpan starts a new tracing span for Cypher execution
func (m *CypherMetrics) StartCypherSpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "cypher."+operation,
		trace.WithAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("operation", operation),
		))
	return ctx, span
}

// SecurityMetrics provides methods for recording security operation metrics
type SecurityMetrics struct {
	clusterName string
	namespace   string
}

// NewSecurityMetrics creates a new SecurityMetrics instance
func NewSecurityMetrics(clusterName, namespace string) *SecurityMetrics {
	return &SecurityMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordSecurityOperation records a security operation (user, role, grant)
func (m *SecurityMetrics) RecordSecurityOperation(ctx context.Context, operation string, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}

	securityOperationTotal.WithLabelValues(m.clusterName, m.namespace, operation, result).Inc()

	// Add span attributes
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("operation", operation),
			attribute.String("result", result),
		)
		span.End()
	}
}

// StartSecuritySpan starts a new tracing span for security operations
func (m *SecurityMetrics) StartSecuritySpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "security."+operation,
		trace.WithAttributes(
			attribute.String("cluster.name", m.clusterName),
			attribute.String("namespace", m.namespace),
			attribute.String("operation", operation),
		))
	return ctx, span
}

// Enhanced metrics for new features

var (
	// Disaster Recovery metrics
	disasterRecoveryStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "disaster_recovery_status",
			Help:      "Status of disaster recovery setup (1=ready, 0=not ready)",
		},
		[]string{LabelClusterName, LabelNamespace, "primary_region", "secondary_region"},
	)

	failoverTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "failover_total",
			Help:      "Total number of failovers performed",
		},
		[]string{LabelClusterName, LabelNamespace, LabelResult},
	)

	replicationLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "replication_lag_seconds",
			Help:      "Replication lag in seconds",
		},
		[]string{LabelClusterName, LabelNamespace, "primary_region", "secondary_region"},
	)

	// Manual scaling metrics
	manualScalerEnabled = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "manual_scaler_enabled",
			Help:      "Status of manual scaling (1=enabled, 0=disabled)",
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	scaleEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "scale_events_total",
			Help:      "Total number of manual scale events",
		},
		[]string{LabelClusterName, LabelNamespace, "node_type", "direction"},
	)

	primaryCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "primary_count",
			Help:      "Current number of primary nodes",
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	secondaryCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "secondary_count",
			Help:      "Current number of secondary nodes",
		},
		[]string{LabelClusterName, LabelNamespace},
	)

	scalingValidationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "scaling_validation_total",
			Help:      "Total number of scaling validation attempts",
		},
		[]string{LabelClusterName, LabelNamespace, "validation_type", LabelResult},
	)

	serverHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystem,
			Name:      "server_health",
			Help:      "Health of individual Neo4j servers: 1=Enabled+Available, 0=degraded",
		},
		[]string{LabelClusterName, LabelNamespace, "server_name", "server_address"},
	)
)

// DisasterRecoveryMetrics provides methods for recording disaster recovery metrics
type DisasterRecoveryMetrics struct {
	clusterName string
	namespace   string
}

// NewDisasterRecoveryMetrics creates a new DisasterRecoveryMetrics instance
func NewDisasterRecoveryMetrics(clusterName, namespace string) *DisasterRecoveryMetrics {
	return &DisasterRecoveryMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordFailover records a failover event
func (m *DisasterRecoveryMetrics) RecordFailover(_ context.Context, success bool) {
	result := "failure"
	if success {
		result = "success"
	}
	failoverTotal.WithLabelValues(m.clusterName, m.namespace, result).Inc()
}

// ManualScalingMetrics provides methods for recording manual scaling metrics
type ManualScalingMetrics struct {
	clusterName string
	namespace   string
}

// NewManualScalingMetrics creates a new ManualScalingMetrics instance
func NewManualScalingMetrics(clusterName, namespace string) *ManualScalingMetrics {
	return &ManualScalingMetrics{
		clusterName: clusterName,
		namespace:   namespace,
	}
}

// RecordPrimaryScaling records a primary node scaling event
func (m *ManualScalingMetrics) RecordPrimaryScaling(_ context.Context, currentReplicas, desiredReplicas int32) {
	primaryCount.WithLabelValues(m.clusterName, m.namespace).Set(float64(desiredReplicas))

	if desiredReplicas > currentReplicas {
		scaleEventsTotal.WithLabelValues(m.clusterName, m.namespace, "primary", "up").Inc()
	} else if desiredReplicas < currentReplicas {
		scaleEventsTotal.WithLabelValues(m.clusterName, m.namespace, "primary", "down").Inc()
	}
}

// RecordSecondaryScaling records a secondary node scaling event
func (m *ManualScalingMetrics) RecordSecondaryScaling(_ context.Context, currentReplicas, desiredReplicas int32) {
	secondaryCount.WithLabelValues(m.clusterName, m.namespace).Set(float64(desiredReplicas))

	if desiredReplicas > currentReplicas {
		scaleEventsTotal.WithLabelValues(m.clusterName, m.namespace, "secondary", "up").Inc()
	} else if desiredReplicas < currentReplicas {
		scaleEventsTotal.WithLabelValues(m.clusterName, m.namespace, "secondary", "down").Inc()
	}
}

// RecordValidation records a scaling validation attempt
func (m *ManualScalingMetrics) RecordValidation(ctx context.Context, validationType string, success bool) {
	result := "failure"
	if success {
		result = "success"
	}
	scalingValidationTotal.WithLabelValues(m.clusterName, m.namespace, validationType, result).Inc()
}

// SetManualScalingEnabled sets the manual scaling enabled status
func (m *ManualScalingMetrics) SetManualScalingEnabled(enabled bool) {
	value := float64(0)
	if enabled {
		value = 1
	}
	manualScalerEnabled.WithLabelValues(m.clusterName, m.namespace).Set(value)
}

// ConflictMetrics provides methods for recording resource version conflict metrics
type ConflictMetrics struct{}

// NewConflictMetrics creates a new ConflictMetrics instance
func NewConflictMetrics() *ConflictMetrics {
	return &ConflictMetrics{}
}

// RecordConflict records a resource version conflict
func (m *ConflictMetrics) RecordConflict(resourceType, namespace string) {
	resourceVersionConflicts.WithLabelValues(resourceType, namespace).Inc()
}

// RecordConflictRetry records retry attempts and duration for conflict resolution
func (m *ConflictMetrics) RecordConflictRetry(resourceType, namespace string, attempts int, duration time.Duration) {
	conflictRetryAttempts.WithLabelValues(resourceType, namespace).Observe(float64(attempts))
	conflictRetryDuration.WithLabelValues(resourceType, namespace).Observe(duration.Seconds())
}

// ServerHealth is a lightweight struct carrying per-server health info for metric recording.
type ServerHealth struct {
	Name      string
	Address   string
	Enabled   bool
	Available bool
}

// RecordServerHealth records per-server health gauges from SHOW SERVERS results.
func (m *ClusterMetrics) RecordServerHealth(servers []ServerHealth) {
	for _, s := range servers {
		value := 0.0
		if s.Enabled && s.Available {
			value = 1.0
		}
		serverHealth.WithLabelValues(m.clusterName, m.namespace, s.Name, s.Address).Set(value)
	}
}
