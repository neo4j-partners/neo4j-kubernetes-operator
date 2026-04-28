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

// Neo4jRoleBindingSpec defines a desired set of role grants for a username
// the operator does NOT manage as a Neo4jUser.
//
// The use case is SSO/LDAP/OIDC users provisioned externally (e.g. on first
// login by Neo4j's user-auth provider integration, or by a bulk import
// pipeline outside the operator's control). The operator does not create
// or drop the user, but does keep the role grants for that user in sync
// with .spec.roles.
//
// Don't use Neo4jRoleBinding for users you also manage with Neo4jUser —
// the validator rejects that overlap to prevent two CRs fighting over the
// same role set. The Neo4jUser CR already manages role bindings via
// .spec.roles.
type Neo4jRoleBindingSpec struct {
	// ClusterRef is the name of the Neo4jEnterpriseCluster or
	// Neo4jEnterpriseStandalone in the same namespace whose security graph
	// owns the user.
	// +kubebuilder:validation:Required
	ClusterRef string `json:"clusterRef"`

	// Username is the Neo4j username to manage role grants for. Must already
	// exist in Neo4j (created externally — e.g. via SSO first-login
	// provisioning) by the time the cluster is Ready. If absent, the binding
	// enters the UserNotFound condition and reconciles when the user appears.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_.\-]*$`
	// +kubebuilder:validation:MaxLength=65
	Username string `json:"username"`

	// Roles is the desired set of Neo4j role names to grant to the user.
	// Built-ins (reader, editor, publisher, architect, admin) are accepted
	// directly; custom role names must correspond to existing roles in
	// Neo4j (typically managed by Neo4jRole CRs in the same namespace).
	// PUBLIC is implicit and is never granted/revoked.
	// +kubebuilder:validation:MinItems=1
	Roles []string `json:"roles"`

	// EnforceExclusive, when true, makes .spec.roles authoritative for the
	// user's full role set: roles granted out-of-band that are not listed
	// here will be REVOKEd on every reconcile. Default is false — the
	// binding only adds and removes the roles it knows about, and tolerates
	// roles granted via other mechanisms (manual, other tools, LDAP-mapped).
	// +kubebuilder:default=false
	// +optional
	EnforceExclusive bool `json:"enforceExclusive,omitempty"`

	// DeletionPolicy controls what happens when this CR is deleted.
	//   Revoke (default): REVOKE every role this binding granted.
	//   Retain:           leave grants in place; only remove the finalizer.
	// +kubebuilder:validation:Enum=Revoke;Retain
	// +kubebuilder:default=Revoke
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// Neo4jRoleBindingStatus describes the observed state of a Neo4jRoleBinding.
type Neo4jRoleBindingStatus struct {
	// Conditions reflects the latest reconcile state. Conditions used:
	//   Ready                — the user exists and the desired grants are in place
	//   RolesSynced          — granted roles include all of .spec.roles
	//   UserNotFound         — the referenced user does not exist in Neo4j
	//   PendingDependencies  — one or more .spec.roles do not yet exist
	//   ClusterNotReady      — the referenced cluster is not Ready
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase is a coarse summary: Pending, Ready, Failed.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message carries a short human-readable explanation of the current Phase.
	// +optional
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the .metadata.generation observed by the
	// controller during the last successful reconcile.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// GrantedRoles is the subset of .spec.roles that the controller has
	// successfully granted (and observed via SHOW USERS YIELD roles).
	// +optional
	GrantedRoles []string `json:"grantedRoles,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=n4jrb;n4jrolebindings,categories=neo4j
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.spec.username`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jRoleBinding is the Schema for the neo4jrolebindings API.
type Neo4jRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jRoleBindingSpec   `json:"spec,omitempty"`
	Status Neo4jRoleBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jRoleBindingList contains a list of Neo4jRoleBinding.
type Neo4jRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jRoleBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jRoleBinding{}, &Neo4jRoleBindingList{})
}
