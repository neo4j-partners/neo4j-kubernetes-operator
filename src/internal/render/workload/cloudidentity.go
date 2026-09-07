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

package workload

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// azureWorkloadIdentityUseLabel is the pod label the Azure Workload Identity webhook keys its
// token/env injection off (https://azure.github.io/azure-workload-identity).
const azureWorkloadIdentityUseLabel = "azure.workload.identity/use"

// BackupServiceAccountName is the dedicated, per-instance ServiceAccount the backup / aggregate /
// prune / metadata Job pods run as (ADR-016). It is derived from the Neo4j CR name so it is stable
// and predictable: a user pre-binds this exact name to a cloud IAM role for workload identity
// (IRSA trust policy / GKE IAM binding / Azure federated credential), which is why WI is an
// instance-level concern and cannot vary per backup. The pods never use the namespace default SA.
func BackupServiceAccountName(neo4j *neo4jv1beta1.Neo4j) string { return neo4j.Name + "-backup" }

// cloudIdentity returns the instance-level object-store identity (spec.security.cloudIdentity), or
// nil when unset.
func cloudIdentity(neo4j *neo4jv1beta1.Neo4j) *neo4jv1beta1.CloudIdentity {
	if s := neo4j.Spec.Security; s != nil {
		return s.CloudIdentity
	}
	return nil
}

// clusterWorkloadIdentity returns the instance-level workload identity opt-in
// (spec.security.cloudIdentity.workloadIdentity), or nil when unset or when static keys are used.
func clusterWorkloadIdentity(neo4j *neo4jv1beta1.Neo4j) *neo4jv1beta1.WorkloadIdentity {
	if ci := cloudIdentity(neo4j); ci != nil {
		return ci.WorkloadIdentity
	}
	return nil
}

// ApplyWorkloadPodIdentity gives the Neo4j server pods the instance's object-store identity so the
// restore seed providers can read s3://, gs://, or azb:// backups (ADR-016). A static-key Secret is
// projected as env into the Neo4j container; workload identity relies on the operand SA's annotations
// (see OperandServiceAccount) plus, for Azure, the pod label the webhook keys token injection off.
// It complements the backup-write side (ApplyBackupPodIdentity) with the same primitives.
func ApplyWorkloadPodIdentity(neo4j *neo4jv1beta1.Neo4j, tmpl *corev1.PodTemplateSpec) {
	ci := cloudIdentity(neo4j)
	if ci == nil {
		return
	}
	if ci.StaticKeySecret != "" && len(tmpl.Spec.Containers) > 0 {
		tmpl.Spec.Containers[0].EnvFrom = append(tmpl.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: ci.StaticKeySecret}},
		})
	}
	if wi := ci.WorkloadIdentity; wi != nil && wi.Provider == neo4jv1beta1.CloudProviderAzure {
		if tmpl.Labels == nil {
			tmpl.Labels = map[string]string{}
		}
		tmpl.Labels[azureWorkloadIdentityUseLabel] = "true"
	}
}

// BackupServiceAccount builds the backup Job ServiceAccount. It carries the workload-identity
// annotations (IRSA role-arn / GKE gcp-service-account / Azure client-id) when the target opts in
// via spec.security.cloudIdentity.workloadIdentity; otherwise it is a plain SA whose only job is to
// keep the backup pods off the namespace default SA (hygiene). It is owned by the Neo4j (applied by
// the workload reconciler) and referenced by name from the Job pods.
func BackupServiceAccount(ctx render.Context) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupServiceAccountName(ctx.Neo4j),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("backup"),
		},
	}
	if wi := clusterWorkloadIdentity(ctx.Neo4j); wi != nil && len(wi.Annotations) > 0 {
		sa.Annotations = make(map[string]string, len(wi.Annotations))
		for k, v := range wi.Annotations {
			sa.Annotations[k] = v
		}
	}
	return sa
}

// ApplyBackupPodIdentity points a backup Job pod template at the dedicated backup ServiceAccount
// and, for Azure workload identity, adds the pod label the Azure webhook keys its token injection
// off. AWS (IRSA / Pod Identity) and GKE need only the SA plus its annotations, which the platform
// webhook / GKE metadata server consume.
//
// ponytail: the projected service-account token is injected by the platform WI webhook (Azure, EKS)
// or served by the node metadata server (GKE); the upgrade path is an explicit projected-token
// volume if we ever target a provider that ships no such webhook.
func ApplyBackupPodIdentity(neo4j *neo4jv1beta1.Neo4j, tmpl *corev1.PodTemplateSpec) {
	tmpl.Spec.ServiceAccountName = BackupServiceAccountName(neo4j)
	if wi := clusterWorkloadIdentity(neo4j); wi != nil && wi.Provider == neo4jv1beta1.CloudProviderAzure {
		if tmpl.Labels == nil {
			tmpl.Labels = map[string]string{}
		}
		tmpl.Labels[azureWorkloadIdentityUseLabel] = "true"
	}
}
