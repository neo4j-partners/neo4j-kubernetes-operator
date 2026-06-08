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

// Neo4jUserSpec defines the desired state of a Neo4j user.
//
// A Neo4jUser maps 1:1 to a user in the referenced Neo4j cluster's `system`
// database. Identity (username), authentication material (password from a
// Secret, or external auth providers such as OIDC/LDAP), account state
// (active/suspended), home database, and role bindings are all reconciled
// to match the spec on every loop.
//
// Privileges are NOT managed here — they belong on Neo4jRole resources.
// A Neo4jUser only references roles by name; the controller does not create
// roles named in .roles. If a referenced custom role is missing the
// PendingDependencies condition is set and the reconcile requeues.
type Neo4jUserSpec struct {
	// ClusterRef is the name of the Neo4jEnterpriseCluster or
	// Neo4jEnterpriseStandalone in the same namespace whose security graph
	// owns this user.
	// +kubebuilder:validation:Required
	ClusterRef string `json:"clusterRef"`

	// Username is the user name in Neo4j. Defaults to metadata.name when
	// empty. Must start with an ASCII letter and contain only letters,
	// digits, underscore, dot, at-sign, or hyphen (at-sign supports
	// email-style SSO/LDAP usernames). Maximum 65 characters.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_.@\-]*$`
	// +kubebuilder:validation:MaxLength=65
	// +optional
	Username string `json:"username,omitempty"`

	// PasswordSecretRef references a Kubernetes Secret containing the
	// native-auth password. Required unless one or more ExternalAuth
	// providers are configured. The Secret value is hashed (SHA-256) and
	// stored in status to detect rotation; the password itself is never
	// echoed back into the resource.
	// +optional
	PasswordSecretRef *SecretKeyRef `json:"passwordSecretRef,omitempty"`

	// RequirePasswordChange forces the user to change the password on next
	// login. Mapped to Neo4j's `SET PASSWORD CHANGE REQUIRED` clause.
	// +kubebuilder:default=false
	// +optional
	RequirePasswordChange bool `json:"requirePasswordChange,omitempty"`

	// AccountStatus is the desired Neo4j account state.
	//   active    — user can authenticate (default).
	//   suspended — user cannot authenticate; native users lose all role
	//               assignments, LDAP/OIDC users retain provider roles.
	// +kubebuilder:validation:Enum=active;suspended
	// +kubebuilder:default=active
	// +optional
	AccountStatus string `json:"accountStatus,omitempty"`

	// HomeDatabase sets the user's home database (or alias). When empty
	// the user falls back to the DBMS default. Removing this field after
	// previously setting it will issue `ALTER USER ... REMOVE HOME DATABASE`.
	// +optional
	HomeDatabase string `json:"homeDatabase,omitempty"`

	// Roles is the desired set of Neo4j role names to grant to this user.
	// Names may be either built-in (reader, editor, publisher, architect,
	// admin) or correspond to a Neo4jRole CR managing the role in this
	// cluster. PUBLIC is implicit and need not be listed; the controller
	// will never attempt to revoke PUBLIC.
	// +optional
	Roles []string `json:"roles,omitempty"`

	// ExternalAuth lists non-native authentication providers (LDAP, OIDC,
	// SSO) the user may authenticate through. Translates to one or more
	// `SET AUTH '<provider>' { SET ID '<id>' }` clauses on ALTER/CREATE
	// USER. Either PasswordSecretRef or at least one ExternalAuth entry
	// must be present (Neo4j requires at least one auth provider).
	// +optional
	ExternalAuth []ExternalAuthProvider `json:"externalAuth,omitempty"`

	// DeletionPolicy controls what happens to the underlying Neo4j user
	// when this CR is deleted.
	//   Delete (default): execute DROP USER on CR deletion.
	//   Retain:           leave the user in Neo4j; only remove the finalizer.
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// ExternalAuthProvider configures a single external authentication provider
// for a Neo4jUser, mapping to Neo4j's `SET AUTH '<provider>' { SET ID '...' }`
// syntax. See the Neo4j docs on managing users with external authentication
// providers for the supported provider names.
type ExternalAuthProvider struct {
	// Provider is the auth provider name, e.g. "oidc-okta", "ldap1", "saml1".
	// The provider must already be configured at the DBMS level via
	// `dbms.security.authentication_providers` etc.
	// +kubebuilder:validation:Required
	Provider string `json:"provider"`

	// ID is the user's identifier within that provider (e.g. an OIDC sub
	// claim or an LDAP DN).
	// +kubebuilder:validation:Required
	ID string `json:"id"`
}

// Neo4jUserStatus describes the observed state of a Neo4jUser.
type Neo4jUserStatus struct {
	// Conditions reflects the latest reconcile state. Conditions used:
	//   Ready                — user is created and in sync with spec
	//   RolesSynced          — granted roles match spec
	//   PasswordSynced       — password hash on the cluster matches the
	//                          last-applied Secret value
	//   PendingDependencies  — at least one referenced custom role does not
	//                          yet exist in Neo4j
	//   ClusterNotReady      — referenced cluster is not Ready
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

	// CurrentRoles lists the role names actually granted to the user in
	// Neo4j as observed via SHOW USERS YIELD roles.
	// +optional
	CurrentRoles []string `json:"currentRoles,omitempty"`

	// PasswordSecretHash is a SHA-256 fingerprint of the password value
	// last applied to the cluster. It is a change-detection fingerprint —
	// not a password-storage hash and not used in any authentication path.
	// The controller uses it to answer "did the Secret rotate since the
	// last reconcile?" via equality comparison; the actual password lives
	// in the referenced Kubernetes Secret and Neo4j stores its own hash
	// (currently scrypt-based) inside the `system` database. SHA-256 is
	// the right choice here — collision resistance is what matters for
	// change detection; computational cost would only slow the loop.
	// +optional
	PasswordSecretHash string `json:"passwordSecretHash,omitempty"`

	// PasswordLastRotated records the last time the controller issued
	// `ALTER USER ... SET PASSWORD ...` against the cluster.
	// +optional
	PasswordLastRotated *metav1.Time `json:"passwordLastRotated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=n4juser;n4jusers,categories=neo4j
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef`
// +kubebuilder:printcolumn:name="Username",type=string,JSONPath=`.spec.username`
// +kubebuilder:printcolumn:name="AccountStatus",type=string,JSONPath=`.spec.accountStatus`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Neo4jUser is the Schema for the neo4jusers API.
type Neo4jUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jUserSpec   `json:"spec,omitempty"`
	Status Neo4jUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Neo4jUserList contains a list of Neo4jUser.
type Neo4jUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jUser{}, &Neo4jUserList{})
}
