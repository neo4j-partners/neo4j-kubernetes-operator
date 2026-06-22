package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestStandaloneSecurityContextDefaults(t *testing.T) {
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}

	podSC := podSecurityContextForStandalone(standalone)
	if podSC == nil || podSC.RunAsUser == nil || *podSC.RunAsUser != 7474 {
		t.Fatalf("expected default pod RunAsUser 7474, got %+v", podSC)
	}
	if podSC.FSGroup == nil || *podSC.FSGroup != 7474 {
		t.Fatalf("expected default pod FSGroup 7474, got %+v", podSC.FSGroup)
	}

	containerSC := containerSecurityContextForStandalone(standalone)
	if containerSC == nil || containerSC.RunAsUser == nil || *containerSC.RunAsUser != 7474 {
		t.Fatalf("expected default container RunAsUser 7474, got %+v", containerSC)
	}
}

func TestStandaloneSecurityContextOverrides(t *testing.T) {
	customPodSC := &corev1.PodSecurityContext{
		RunAsUser: ptr.To[int64](1000),
	}
	customContainerSC := &corev1.SecurityContext{
		RunAsUser: ptr.To[int64](1000),
	}

	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			AcceptLicenseAgreement: "eval",
			SecurityContext: &neo4jv1beta1.SecurityContextSpec{
				PodSecurityContext:       customPodSC,
				ContainerSecurityContext: customContainerSC,
			},
		},
	}

	if got := podSecurityContextForStandalone(standalone); got != customPodSC {
		t.Fatalf("expected pod security context override to be used")
	}
	if got := containerSecurityContextForStandalone(standalone); got != customContainerSC {
		t.Fatalf("expected container security context override to be used")
	}
}
