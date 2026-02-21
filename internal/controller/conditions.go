// Package controller provides shared condition type and reason constants for all Neo4j operator controllers.
package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Standard condition types following Kubernetes API conventions.
// Flux health checks use the "Ready" condition type automatically.
const (
	ConditionTypeAvailable   = "Available"
	ConditionTypeProgressing = "Progressing"
	ConditionTypeDegraded    = "Degraded"
	ConditionTypeReady       = "Ready"

	// ConditionTypeServersHealthy indicates all Neo4j servers in the cluster
	// are in Enabled state and Available health.
	ConditionTypeServersHealthy = "ServersHealthy"

	// ConditionTypeDatabasesHealthy indicates all expected user databases are online.
	ConditionTypeDatabasesHealthy = "DatabasesHealthy"
)

// Reason constants for the Ready condition across all CRDs.
const (
	ConditionReasonReady           = "ClusterReady"
	ConditionReasonForming         = "ClusterForming"
	ConditionReasonFailed          = "ReconciliationFailed"
	ConditionReasonUpgrading       = "UpgradeInProgress"
	ConditionReasonPending         = "Pending"
	ConditionReasonDatabaseReady   = "DatabaseReady"
	ConditionReasonDatabaseFailed  = "DatabaseCreationFailed"
	ConditionReasonBackupSucceeded = "BackupSucceeded"
	ConditionReasonBackupFailed    = "BackupFailed"
	ConditionReasonRestoreComplete = "RestoreCompleted"
	ConditionReasonRestoreFailed   = "RestoreFailed"
	ConditionReasonPluginInstalled = "PluginInstalled"
	ConditionReasonPluginFailed    = "PluginInstallFailed"

	ConditionReasonAllServersHealthy      = "AllServersHealthy"
	ConditionReasonServerDegraded         = "ServerDegraded"
	ConditionReasonAllDatabasesOnline     = "AllDatabasesOnline"
	ConditionReasonDatabaseOffline        = "DatabaseOffline"
	ConditionReasonDiagnosticsUnavailable = "DiagnosticsUnavailable"
)

// SetReadyCondition sets the standard "Ready" condition on a conditions slice.
// It preserves LastTransitionTime when status and reason are unchanged.
// Returns true if the condition was changed (new or status/reason changed).
func SetReadyCondition(conditions *[]metav1.Condition, generation int64, status metav1.ConditionStatus, reason, message string) bool {
	existing := findCondition(*conditions, ConditionTypeReady)
	if existing != nil && existing.Status == status && existing.Reason == reason {
		// Only update generation and message — preserve LastTransitionTime
		existing.ObservedGeneration = generation
		existing.Message = message
		return false
	}
	newCond := metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             status,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	*conditions = upsertCondition(*conditions, newCond)
	return true
}

// SetNamedCondition upserts any named condition type on a conditions slice.
// It preserves LastTransitionTime when status and reason are unchanged.
// Returns true if the condition changed.
func SetNamedCondition(conditions *[]metav1.Condition, condType string, generation int64, status metav1.ConditionStatus, reason, message string) bool {
	existing := findCondition(*conditions, condType)
	if existing != nil && existing.Status == status && existing.Reason == reason {
		existing.ObservedGeneration = generation
		existing.Message = message
		return false
	}
	newCond := metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	*conditions = upsertCondition(*conditions, newCond)
	return true
}

// PhaseToConditionStatus maps a phase string to a ConditionStatus and Ready condition reason.
func PhaseToConditionStatus(phase string) (metav1.ConditionStatus, string) {
	switch phase {
	case "Ready", "Installed":
		return metav1.ConditionTrue, ConditionReasonReady
	case "Completed":
		return metav1.ConditionTrue, ConditionReasonBackupSucceeded
	case "Failed", "Degraded", "Suspended":
		return metav1.ConditionFalse, ConditionReasonFailed
	case "Upgrading":
		return metav1.ConditionUnknown, ConditionReasonUpgrading
	case "Forming", "Creating":
		return metav1.ConditionUnknown, ConditionReasonForming
	case "Installing", "Running", "Validating", "Pending":
		return metav1.ConditionUnknown, ConditionReasonPending
	default:
		return metav1.ConditionUnknown, ConditionReasonPending
	}
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func upsertCondition(conditions []metav1.Condition, cond metav1.Condition) []metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == cond.Type {
			conditions[i] = cond
			return conditions
		}
	}
	return append(conditions, cond)
}
