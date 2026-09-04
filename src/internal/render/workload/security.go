package workload

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// allowedCapabilityAdds is the only Capability.Add values permitted on the Neo4j container.
var allowedCapabilityAdds = map[corev1.Capability]struct{}{
	"NET_BIND_SERVICE": {},
}

// ValidateSecurity rejects CR fields that would grant node-level privilege via the
// operand StatefulSet (NEO-001): privileged containers, hostPath volumes are checked
// in storage.Validate; here we cover security contexts. Also NEO-002: reserved CR
// names and cloud workload-identity annotations on the operand ServiceAccount.
func ValidateSecurity(neo4j *neo4jv1beta1.Neo4j) error {
	if neo4j.Name == "default" {
		return fmt.Errorf("metadata.name %q is not allowed (would collide with the namespace default ServiceAccount)", neo4j.Name)
	}
	if neo4j.Spec.Security == nil {
		return nil
	}
	if err := validatePodSecurityContext(neo4j.Spec.Security.PodSecurityContext); err != nil {
		return err
	}
	if err := validateContainerSecurityContext(neo4j.Spec.Security.ContainerSecurityContext); err != nil {
		return err
	}
	return validateServiceAccountSpec(neo4j.Spec.Security.ServiceAccount)
}

func validateServiceAccountSpec(sa *neo4jv1beta1.ServiceAccountSpec) error {
	if sa == nil {
		return nil
	}
	for k := range sa.Annotations {
		if isCloudWorkloadIdentityAnnotation(k) {
			return fmt.Errorf("spec.security.serviceAccount.annotations[%q] is not allowed (cloud IAM / workload identity; NEO-002)", k)
		}
	}
	return nil
}

// isCloudWorkloadIdentityAnnotation matches keys that bind a K8s SA to cloud IAM.
func isCloudWorkloadIdentityAnnotation(key string) bool {
	switch {
	case key == "eks.amazonaws.com/role-arn":
		return true
	case key == "eks.amazonaws.com/audience":
		return true
	case strings.HasPrefix(key, "eks.amazonaws.com/"):
		return true
	case key == "iam.gke.io/gcp-service-account":
		return true
	case strings.HasPrefix(key, "iam.gke.io/"):
		return true
	case strings.HasPrefix(key, "azure.workload.identity/"):
		return true
	default:
		return false
	}
}

func validatePodSecurityContext(psc *corev1.PodSecurityContext) error {
	if psc == nil {
		return nil
	}
	if psc.RunAsNonRoot != nil && !*psc.RunAsNonRoot {
		return fmt.Errorf("spec.security.podSecurityContext.runAsNonRoot must not be false")
	}
	if psc.RunAsUser != nil && *psc.RunAsUser == 0 {
		return fmt.Errorf("spec.security.podSecurityContext.runAsUser 0 is not allowed")
	}
	return nil
}

func validateContainerSecurityContext(csc *corev1.SecurityContext) error {
	if csc == nil {
		return nil
	}
	if csc.Privileged != nil && *csc.Privileged {
		return fmt.Errorf("spec.security.containerSecurityContext.privileged is not allowed")
	}
	if csc.AllowPrivilegeEscalation != nil && *csc.AllowPrivilegeEscalation {
		return fmt.Errorf("spec.security.containerSecurityContext.allowPrivilegeEscalation must not be true")
	}
	if csc.RunAsNonRoot != nil && !*csc.RunAsNonRoot {
		return fmt.Errorf("spec.security.containerSecurityContext.runAsNonRoot must not be false")
	}
	if csc.RunAsUser != nil && *csc.RunAsUser == 0 {
		return fmt.Errorf("spec.security.containerSecurityContext.runAsUser 0 is not allowed")
	}
	if csc.Capabilities != nil {
		for _, c := range csc.Capabilities.Add {
			if _, ok := allowedCapabilityAdds[c]; !ok {
				return fmt.Errorf("spec.security.containerSecurityContext.capabilities.add %q is not allowed", c)
			}
		}
	}
	return nil
}

// PodSecurityContext and ContainerSecurityContext expose the hardened operand security contexts
// (defaults plus CR overrides) for reuse by satellite renders such as the backup Job. Sharing
// them makes those pods pass restricted Pod Security and, crucially, run with the operand's
// fsGroup so files the Job writes to a shared backups PVC are readable by the Neo4j servers.
func PodSecurityContext(ctx render.Context) *corev1.PodSecurityContext {
	return podSecurityContext(ctx)
}

// ContainerSecurityContext — see PodSecurityContext.
func ContainerSecurityContext(ctx render.Context) *corev1.SecurityContext {
	return containerSecurityContext(ctx)
}

func podSecurityContext(ctx render.Context) *corev1.PodSecurityContext {
	out := defaultPodSecurityContext()
	if ctx.Neo4j.Spec.Security == nil || ctx.Neo4j.Spec.Security.PodSecurityContext == nil {
		return out
	}
	user := ctx.Neo4j.Spec.Security.PodSecurityContext
	if user.RunAsUser != nil {
		out.RunAsUser = user.RunAsUser
	}
	if user.RunAsGroup != nil {
		out.RunAsGroup = user.RunAsGroup
	}
	if user.FSGroup != nil {
		out.FSGroup = user.FSGroup
	}
	if user.RunAsNonRoot != nil {
		out.RunAsNonRoot = user.RunAsNonRoot
	}
	if user.FSGroupChangePolicy != nil {
		out.FSGroupChangePolicy = user.FSGroupChangePolicy
	}
	if user.SeccompProfile != nil {
		out.SeccompProfile = user.SeccompProfile.DeepCopy()
	}
	if user.SupplementalGroups != nil {
		out.SupplementalGroups = append([]int64(nil), user.SupplementalGroups...)
	}
	return out
}

func containerSecurityContext(ctx render.Context) *corev1.SecurityContext {
	out := defaultContainerSecurityContext()
	falseVal := false
	out.Privileged = &falseVal
	out.AllowPrivilegeEscalation = &falseVal

	if ctx.Neo4j.Spec.Security == nil || ctx.Neo4j.Spec.Security.ContainerSecurityContext == nil {
		return out
	}
	user := ctx.Neo4j.Spec.Security.ContainerSecurityContext
	if user.RunAsUser != nil {
		out.RunAsUser = user.RunAsUser
	}
	if user.RunAsGroup != nil {
		out.RunAsGroup = user.RunAsGroup
	}
	if user.RunAsNonRoot != nil {
		out.RunAsNonRoot = user.RunAsNonRoot
	}
	if user.ReadOnlyRootFilesystem != nil {
		out.ReadOnlyRootFilesystem = user.ReadOnlyRootFilesystem
	}
	if user.AllowPrivilegeEscalation != nil && !*user.AllowPrivilegeEscalation {
		out.AllowPrivilegeEscalation = user.AllowPrivilegeEscalation
	}
	if user.SeccompProfile != nil {
		out.SeccompProfile = user.SeccompProfile.DeepCopy()
	}
	if user.Capabilities != nil {
		caps := &corev1.Capabilities{
			Drop: append([]corev1.Capability(nil), user.Capabilities.Drop...),
			Add:  append([]corev1.Capability(nil), user.Capabilities.Add...),
		}
		if len(caps.Drop) == 0 {
			caps.Drop = []corev1.Capability{"ALL"}
		}
		out.Capabilities = caps
	}
	// Defense in depth: never honor privileged from the CR.
	out.Privileged = &falseVal
	return out
}
