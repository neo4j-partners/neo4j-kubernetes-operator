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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Neo4jPasswordRotationSpec is the desired state of a one-shot, immutable admin-password
// rotation record (BDR-017 Option F, ADR-019). The rotation owner stages the new password in
// its own Secret and leaves the instance's live auth Secret (the old value) untouched; the
// operator authenticates with the still-live old value, runs ALTER USER neo4j SET PASSWORD, then
// writes the new value into the live auth Secret. The old value therefore survives until the
// ALTER lands — no persistent shadow Secret.
type Neo4jPasswordRotationSpec struct {
	// Neo4jRef is the target workload whose admin (neo4j) password is rotated.
	// +kubebuilder:validation:Required
	Neo4jRef Neo4jRef `json:"neo4jRef"`
	// NewPasswordSecretRef references the Secret holding the NEW credential (key NEO4J_AUTH,
	// value "neo4j/<new>"), staged by the rotation owner. It must carry the same delegation
	// labels as any operator-mounted auth Secret (NEO-005 / ADD-01). The instance's current auth
	// Secret is read as the OLD value and is not named here.
	// +kubebuilder:validation:Required
	NewPasswordSecretRef PasswordSecretRef `json:"newPasswordSecretRef"`
	// DeleteNewSecretOnSuccess opts the operator into deleting the staged Secret once the rotation
	// reports Succeeded. Default false leaves the staged Secret to its owner. The operator never
	// deletes it on a failure path — it is the sole copy of the new password until write-back lands.
	DeleteNewSecretOnSuccess bool `json:"deleteNewSecretOnSuccess,omitempty"`
}

// Neo4jPasswordRotationStatus is the observed state of a rotation run.
type Neo4jPasswordRotationStatus struct {
	// Phase is the coarse run state (Pending, Running, Succeeded, Failed).
	Phase RunPhase `json:"phase,omitempty"`
	// Conditions are Kubernetes-standard signals.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last spec generation reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Reason is a stable machine reason on failure (oracle catalog).
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable detail; never the password itself.
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=n4jpr,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.neo4jRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="Neo4jPasswordRotation spec is immutable (it is a run-to-completion record); file a new rotation to rotate again"
// Neo4jPasswordRotation is an immutable, one-shot admin-password rotation record (BDR-017, ADR-019).
type Neo4jPasswordRotation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Neo4jPasswordRotationSpec   `json:"spec,omitempty"`
	Status Neo4jPasswordRotationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type Neo4jPasswordRotationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Neo4jPasswordRotation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Neo4jPasswordRotation{}, &Neo4jPasswordRotationList{})
}
