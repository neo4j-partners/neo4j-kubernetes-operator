package workload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func backupIdentityNeo4j(wi *neo4jv1beta1.WorkloadIdentity) *neo4jv1beta1.Neo4j {
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "team"},
		Spec:       neo4jv1beta1.Neo4jSpec{Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone}},
	}
	if wi != nil {
		n.Spec.Security = &neo4jv1beta1.SecuritySpec{CloudIdentity: &neo4jv1beta1.CloudIdentity{WorkloadIdentity: wi}}
	}
	return n
}

func TestBackupServiceAccountName(t *testing.T) {
	if got := BackupServiceAccountName(backupIdentityNeo4j(nil)); got != "prod-backup" {
		t.Fatalf("got %q", got)
	}
}

func TestBackupServiceAccountCarriesWorkloadIdentityAnnotations(t *testing.T) {
	n := backupIdentityNeo4j(&neo4jv1beta1.WorkloadIdentity{
		Provider:    neo4jv1beta1.CloudProviderAWS,
		Annotations: map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::123:role/neo4j-s3"},
	})
	sa := BackupServiceAccount(render.StandaloneContext(n))
	if sa.Name != "prod-backup" || sa.Namespace != "team" {
		t.Fatalf("meta = %s/%s", sa.Namespace, sa.Name)
	}
	if sa.Annotations["eks.amazonaws.com/role-arn"] != "arn:aws:iam::123:role/neo4j-s3" {
		t.Fatalf("annotations = %#v", sa.Annotations)
	}
}

func TestBackupServiceAccountNoAnnotationsWithoutOptIn(t *testing.T) {
	sa := BackupServiceAccount(render.StandaloneContext(backupIdentityNeo4j(nil)))
	if len(sa.Annotations) != 0 {
		t.Fatalf("expected no annotations, got %#v", sa.Annotations)
	}
}

func TestApplyBackupPodIdentitySetsServiceAccountAndAzureLabel(t *testing.T) {
	// Azure is the only provider needing the pod label; AWS/GCP get the SA but no label.
	for provider, wantLabel := range map[neo4jv1beta1.CloudWorkloadIdentityProvider]bool{
		neo4jv1beta1.CloudProviderAzure: true,
		neo4jv1beta1.CloudProviderAWS:   false,
		neo4jv1beta1.CloudProviderGCP:   false,
	} {
		n := backupIdentityNeo4j(&neo4jv1beta1.WorkloadIdentity{Provider: provider})
		tmpl := &corev1.PodTemplateSpec{}
		ApplyBackupPodIdentity(n, tmpl)
		if tmpl.Spec.ServiceAccountName != "prod-backup" {
			t.Fatalf("%s: SA = %q", provider, tmpl.Spec.ServiceAccountName)
		}
		_, has := tmpl.Labels[azureWorkloadIdentityUseLabel]
		if has != wantLabel {
			t.Fatalf("%s: azure label present=%v want=%v", provider, has, wantLabel)
		}
	}
}

func TestApplyBackupPodIdentityNoOptInStillSetsServiceAccount(t *testing.T) {
	tmpl := &corev1.PodTemplateSpec{}
	ApplyBackupPodIdentity(backupIdentityNeo4j(nil), tmpl)
	if tmpl.Spec.ServiceAccountName != "prod-backup" {
		t.Fatalf("SA = %q", tmpl.Spec.ServiceAccountName)
	}
	if _, has := tmpl.Labels[azureWorkloadIdentityUseLabel]; has {
		t.Fatalf("azure label must be absent without opt-in")
	}
}

func serverPodTemplate() *corev1.PodTemplateSpec {
	return &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "neo4j"}}}}
}

func TestApplyWorkloadPodIdentityStaticKeyProjectsEnv(t *testing.T) {
	n := backupIdentityNeo4j(nil)
	n.Spec.Security = &neo4jv1beta1.SecuritySpec{CloudIdentity: &neo4jv1beta1.CloudIdentity{StaticKeySecret: "neo4j-cloud-creds"}}
	tmpl := serverPodTemplate()
	ApplyWorkloadPodIdentity(n, tmpl)
	env := tmpl.Spec.Containers[0].EnvFrom
	if len(env) != 1 || env[0].SecretRef == nil || env[0].SecretRef.Name != "neo4j-cloud-creds" {
		t.Fatalf("envFrom = %#v", env)
	}
	if _, has := tmpl.Labels[azureWorkloadIdentityUseLabel]; has {
		t.Fatalf("static-key path must not add the azure label")
	}
}

func TestApplyWorkloadPodIdentityAzureLabelOnly(t *testing.T) {
	n := backupIdentityNeo4j(&neo4jv1beta1.WorkloadIdentity{Provider: neo4jv1beta1.CloudProviderAzure})
	tmpl := serverPodTemplate()
	ApplyWorkloadPodIdentity(n, tmpl)
	if tmpl.Labels[azureWorkloadIdentityUseLabel] != "true" {
		t.Fatalf("expected azure label, got %#v", tmpl.Labels)
	}
	if len(tmpl.Spec.Containers[0].EnvFrom) != 0 {
		t.Fatalf("WI must not project static-key env")
	}
}

func TestApplyWorkloadPodIdentityNoOptInNoOp(t *testing.T) {
	tmpl := serverPodTemplate()
	ApplyWorkloadPodIdentity(backupIdentityNeo4j(nil), tmpl)
	if len(tmpl.Spec.Containers[0].EnvFrom) != 0 || len(tmpl.Labels) != 0 {
		t.Fatalf("expected no-op, got env=%#v labels=%#v", tmpl.Spec.Containers[0].EnvFrom, tmpl.Labels)
	}
}

func TestOperandServiceAccountCarriesWorkloadIdentityAnnotations(t *testing.T) {
	n := backupIdentityNeo4j(&neo4jv1beta1.WorkloadIdentity{
		Provider:    neo4jv1beta1.CloudProviderGCP,
		Annotations: map[string]string{"iam.gke.io/gcp-service-account": "neo4j@proj.iam.gserviceaccount.com"},
	})
	sa := OperandServiceAccount(render.StandaloneContext(n))
	if sa.Annotations["iam.gke.io/gcp-service-account"] != "neo4j@proj.iam.gserviceaccount.com" {
		t.Fatalf("operand SA annotations = %#v", sa.Annotations)
	}
}
