package workload

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestValidateSecurityRejectsPrivileged(t *testing.T) {
	priv := true
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Security: &neo4jv1beta1.SecuritySpec{
				ContainerSecurityContext: &corev1.SecurityContext{Privileged: &priv},
			},
		},
	}
	err := ValidateSecurity(neo4j)
	if err == nil || !strings.Contains(err.Error(), "privileged") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSecurityRejectsHostRootUser(t *testing.T) {
	uid := int64(0)
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Security: &neo4jv1beta1.SecuritySpec{
				PodSecurityContext: &corev1.PodSecurityContext{RunAsUser: &uid},
			},
		},
	}
	if err := ValidateSecurity(neo4j); err == nil || !strings.Contains(err.Error(), "runAsUser 0") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSecurityRejectsDangerousCapability(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Security: &neo4jv1beta1.SecuritySpec{
				ContainerSecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN"}},
				},
			},
		},
	}
	if err := ValidateSecurity(neo4j); err == nil || !strings.Contains(err.Error(), "SYS_ADMIN") {
		t.Fatalf("got %v", err)
	}
}

func TestContainerSecurityContextMergesOverDefaults(t *testing.T) {
	uid := int64(1000)
	priv := true // would be rejected by Validate; merge must still force false
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Security: &neo4jv1beta1.SecuritySpec{
				ContainerSecurityContext: &corev1.SecurityContext{
					RunAsUser:  &uid,
					Privileged: &priv,
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"NET_BIND_SERVICE"},
					},
				},
			},
		},
	}
	csc := containerSecurityContext(render.StandaloneContext(neo4j))
	if csc.RunAsUser == nil || *csc.RunAsUser != 1000 {
		t.Fatalf("RunAsUser = %#v", csc.RunAsUser)
	}
	if csc.Privileged == nil || *csc.Privileged {
		t.Fatalf("Privileged must be forced false, got %#v", csc.Privileged)
	}
	if csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation {
		t.Fatalf("AllowPrivilegeEscalation must be false, got %#v", csc.AllowPrivilegeEscalation)
	}
	if csc.RunAsNonRoot == nil || !*csc.RunAsNonRoot {
		t.Fatalf("default RunAsNonRoot must remain, got %#v", csc.RunAsNonRoot)
	}
	if len(csc.Capabilities.Drop) == 0 || csc.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("Drop ALL must remain, got %#v", csc.Capabilities)
	}
}
