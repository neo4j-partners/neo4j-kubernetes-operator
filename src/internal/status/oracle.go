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

package status

// Entry is one stable condition reason — the test oracle for operator errors.
// Keep in sync with docs/03-user-documentation/reference/error-overview.md.
type Entry struct {
	Condition string // status.conditions[].type
	Reason    string // status.conditions[].reason
	Severity  string // error | warn | info
	Summary   string // short MkDocs-friendly description
}

// ErrorOracle is the canonical catalog of condition reasons operators and tests
// assert against. Prefer matching Reason (stable) over Message (free-form).
var ErrorOracle = []Entry{
	{Condition: ConditionError, Reason: "ReconcileFailed", Severity: "error", Summary: "A pipeline step returned an error"},
	{Condition: ConditionReady, Reason: "ReconcileError", Severity: "error", Summary: "Ready cleared because reconcile failed"},
	{Condition: ConditionReconciling, Reason: "Failed", Severity: "error", Summary: "Reconciling stopped after failure"},
	{Condition: ConditionTLSReady, Reason: "SecretMissing", Severity: "error", Summary: "Required TLS/auth Secret is missing or incomplete"},
	{Condition: ConditionStorageReady, Reason: "PVCPending", Severity: "warn", Summary: "Data PVC not Bound yet (or missing StorageClass)"},
	{Condition: ConditionReady, Reason: "OfflineMaintenance", Severity: "info", Summary: "spec.maintenance.offlineMode is true"},
	{Condition: "ClusterFormed", Reason: "BoltUnavailable", Severity: "warn", Summary: "Cannot reach Bolt to form/align the cluster"},
	{Condition: "ClusterFormed", Reason: "ShowServersFailed", Severity: "error", Summary: "SHOW SERVERS failed over Bolt"},
	{Condition: "ClusterFormed", Reason: "UnsupportedSinglePrimary", Severity: "error", Summary: "Neo4j forbids shrinking to a single primary"},
	{Condition: "ClusterFormed", Reason: "UnsupportedSystemScaleUp", Severity: "error", Summary: "Cannot grow system DB from 1 primary via ENABLE alone"},
	{Condition: "ClusterFormed", Reason: "WaitingSystemLeader", Severity: "warn", Summary: "Waiting for a system database leader"},
	{Condition: "ClusterFormed", Reason: "WaitingQuorum", Severity: "warn", Summary: "Waiting for primary quorum / enable completion"},
	{Condition: "ServersPendingDrain", Reason: "UnsupportedSinglePrimary", Severity: "error", Summary: "Drain blocked — would leave a single primary"},
	{Condition: "ServersPendingDrain", Reason: "ShrinkingTopology", Severity: "info", Summary: "Scale-in in progress"},
	{Condition: "ServersPendingDrain", Reason: "Draining", Severity: "info", Summary: "Server drain / DEALLOCATE in progress"},
	{Condition: "ServersPendingDrain", Reason: "AwaitingSTSShrink", Severity: "info", Summary: "Waiting for StatefulSet replica shrink after drain"},
}
