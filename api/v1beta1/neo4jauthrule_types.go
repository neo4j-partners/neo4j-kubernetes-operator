/*
Copyright 2026.

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

// Neo4jAuthRuleSpec defines the desired state of a Neo4j attribute-based
// access control (ABAC) AUTH RULE.
//
// AUTH RULE was introduced in Neo4j 2026.03; the Neo4jAuthRule controller
// refuses to reconcile against earlier clusters.
//
// An auth rule conditionally grants one or more roles when its Cypher
// `condition` evaluates to true at OIDC authentication time. Conditions
// reference user attributes from the OIDC token via
// `abac.oidc.user_attribute('claim_key')`.
//
// See https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/.
type Neo4jAuthRuleSpec struct {
	// ClusterRef is the name of the Neo4jEnterpriseCluster or
	// Neo4jEnterpriseStandalone in the same namespace whose security graph
	// owns this auth rule. The cluster must be Neo4j 2026.03 or later.
	// +kubebuilder:validation:Required
	ClusterRef string `json:"clusterRef"`

	// Name is the auth-rule name in Neo4j (the identifier shown in
	// `SHOW AUTH RULES`). Defaults to metadata.name when empty. Must start
	// with an ASCII letter and contain only letters, digits, underscores,
	// or hyphens.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=65
	// +optional
	Name string `json:"name,omitempty"`

	// Condition is the Cypher expression evaluated against the user's OIDC
	// token at authentication time. Returning true grants the listed
	// GrantedRoles for the duration of the session.
	//
	// Examples:
	//
	//   abac.oidc.user_attribute('department') = 'sales'
	//
	//   abac.oidc.user_attribute('region') = 'EMEA' AND
	//     time.transaction('UTC').hour >= 6 AND
	//     time.transaction('UTC').hour < 18
	//
	//   any(country IN abac.oidc.user_attribute('countries')
	//       WHERE country IN ['US', 'GB'])
	//
	// The full list of supported Cypher functions is documented at
	// https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/.
	// Conditions MUST be pure expressions — they cannot contain DDL like
	// CREATE/ALTER/DROP; the validator rejects such inputs defensively.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Condition string `json:"condition"`

	// Enabled toggles whether the rule actively maps claims to role grants.
	// Disabled rules are preserved in Neo4j (so they can be re-enabled
	// quickly) but do not affect any session.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// GrantedRoles names the Neo4j roles that this rule grants when its
	// Condition evaluates true. Roles MUST exist on the cluster (or as
	// Neo4jRole CRs in the same namespace) and MUST NOT contain any DENY
	// privileges — Neo4j refuses to assign deny-bearing roles to auth
	// rules to prevent privilege escalation if a rule unexpectedly fails.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	GrantedRoles []string `json:"grantedRoles"`

	// EnforceRoles controls whether the controller treats GrantedRoles as
	// the exclusive set of roles bound to this rule. When true (default)
	// the controller revokes any roles attached out-of-band that are not
	// in spec.grantedRoles. When false the controller only adds missing
	// grants and never revokes.
	// +kubebuilder:default=true
	// +optional
	EnforceRoles bool `json:"enforceRoles,omitempty"`

	// DeletionPolicy controls what happens to the underlying Neo4j auth
	// rule when this CR is deleted.
	//   Drop (default): execute DROP AUTH RULE on CR deletion.
	//   Retain:         leave the rule in Neo4j; only remove the finalizer.
	// +kubebuilder:validation:Enum=Drop;Retain
	// +kubebuilder:default=Drop
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// Neo4jAuthRuleStatus describes the observed state of a Neo4jAuthRule.
type Neo4jAuthRuleStatus struct {
	// Conditions reflects the latest reconcile state. Conditions used:
	//   Ready                  — auth rule exists and granted roles are in sync
	//   ConditionValid         — the Cypher condition parses successfully
	//   OIDCProviderConfigured — the cluster has dbms.security.abac.authorization_providers set
	//   GrantedRolesExist      — all spec.grantedRoles resolve in Neo4j
	//   ClusterNotReady        — referenced cluster is not Ready or too old
	//   PendingDependencies    — one or more granted roles do not yet exist
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase is a coarse summary of the reconcile state:
	// Pending, Ready, Failed, PendingDependencies.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message carries a short human-readable explanation of the current Phase.
	// +optional
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the .metadata.generation observed by the
	// controller during the last successful reconcile.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// AppliedRoles is the set of roles last observed as granted to this
	// rule via SHOW AUTH RULES. Useful for confirming drift reconciliation.
	// +optional
	AppliedRoles []string `json:"appliedRoles,omitempty"`

	// AppliedEnabled reflects the enabled flag last observed on the rule.
	// +optional
	AppliedEnabled *bool `json:"appliedEnabled,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=n4jauthrule;n4jauthrules,categories=neo4j
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.status.appliedEnabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jAuthRule is the Schema for the neo4jauthrules API.
type Neo4jAuthRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jAuthRuleSpec   `json:"spec,omitempty"`
	Status Neo4jAuthRuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jAuthRuleList contains a list of Neo4jAuthRule.
type Neo4jAuthRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jAuthRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jAuthRule{}, &Neo4jAuthRuleList{})
}
