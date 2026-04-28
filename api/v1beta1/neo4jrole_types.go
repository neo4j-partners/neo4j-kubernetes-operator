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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4jRoleSpec defines the desired state of a Neo4j role.
//
// A Neo4jRole maps 1:1 to a role in the referenced Neo4j cluster's `system`
// database. Privileges are managed declaratively: the controller diffs the
// spec against `SHOW ROLE <r> PRIVILEGES AS COMMANDS` and applies the
// difference. Built-in roles (PUBLIC, reader, editor, publisher, architect,
// admin) cannot be managed unless `adoptBuiltin` is true.
type Neo4jRoleSpec struct {
	// ClusterRef is the name of the Neo4jEnterpriseCluster or
	// Neo4jEnterpriseStandalone in the same namespace whose security graph
	// owns this role.
	// +kubebuilder:validation:Required
	ClusterRef string `json:"clusterRef"`

	// Name is the role name in Neo4j. Defaults to metadata.name when empty.
	// Must start with an ASCII letter and contain only letters, digits, or
	// underscores (Neo4j role-naming rules).
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_]*$`
	// +optional
	Name string `json:"name,omitempty"`

	// CopyOf names an existing role to seed privileges from at creation time.
	// Honoured only on first reconcile when the role does not yet exist;
	// ignored thereafter (the role's privileges are then driven by .privileges).
	// +optional
	CopyOf string `json:"copyOf,omitempty"`

	// Privileges is the desired set of GRANT or DENY statements for this role.
	// Each entry MUST be a complete Cypher statement starting with GRANT or
	// DENY and ending with `TO <role-name>` matching .name.
	//
	// Example:
	//   - "GRANT ACCESS ON DATABASE analytics TO analytics_reader"
	//   - "GRANT MATCH {*} ON GRAPH analytics NODES * TO analytics_reader"
	//   - "DENY WRITE ON GRAPH analytics TO analytics_reader"
	//
	// On every reconcile the controller reads `SHOW ROLE <name> PRIVILEGES
	// AS COMMANDS`, canonicalises both sides, and applies the difference.
	// Manual privilege changes made directly in Neo4j will be reverted on
	// the next loop unless EnforcePrivileges is false.
	// +optional
	Privileges []string `json:"privileges,omitempty"`

	// EnforcePrivileges controls drift reconciliation for privileges.
	// When true (default) the controller reverts manual privilege changes
	// to match .privileges. When false the controller only applies
	// statements at creation time and never revokes privileges added
	// out-of-band.
	// +kubebuilder:default=true
	// +optional
	EnforcePrivileges bool `json:"enforcePrivileges,omitempty"`

	// AdoptBuiltin allows .name to reference one of the built-in roles
	// (PUBLIC, reader, editor, publisher, architect, admin). When true the
	// controller will manage privileges on the existing role but will NEVER
	// drop it on CR delete. PUBLIC's role assignment to all users is
	// preserved regardless.
	// +kubebuilder:default=false
	// +optional
	AdoptBuiltin bool `json:"adoptBuiltin,omitempty"`

	// DeletionPolicy controls what happens to the underlying Neo4j role
	// when this CR is deleted.
	//   Delete (default): execute DROP ROLE on CR deletion.
	//   Retain:           leave the role in Neo4j; only remove the finalizer.
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// Neo4jRoleStatus describes the observed state of a Neo4jRole.
type Neo4jRoleStatus struct {
	// Conditions reflects the latest reconcile state. Conditions used:
	//   Ready             — role exists and privileges in sync
	//   PrivilegesSynced  — desired privileges match Neo4j
	//   ClusterNotReady   — referenced cluster is not Ready
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase is a coarse summary of the reconcile state:
	// Pending, Ready, Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message carries a short human-readable explanation of the current Phase.
	// +optional
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the .metadata.generation observed by the
	// controller during the last successful reconcile.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// AppliedPrivileges is the canonicalised set of privilege statements
	// last observed on this role via SHOW ROLE PRIVILEGES AS COMMANDS.
	// Useful for debugging drift between spec and live state.
	// +optional
	AppliedPrivileges []string `json:"appliedPrivileges,omitempty"`

	// PrivilegeDrift is true when the live privileges differ from spec
	// at the end of a reconcile (e.g. immutable privileges that cannot be
	// removed). When EnforcePrivileges is false this is informational only.
	// +optional
	PrivilegeDrift bool `json:"privilegeDrift,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=n4jrole;n4jroles,categories=neo4j
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Drift",type=boolean,JSONPath=`.status.privilegeDrift`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jRole is the Schema for the neo4jroles API.
type Neo4jRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jRoleSpec   `json:"spec,omitempty"`
	Status Neo4jRoleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jRoleList contains a list of Neo4jRole.
type Neo4jRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jRole{}, &Neo4jRoleList{})
}
