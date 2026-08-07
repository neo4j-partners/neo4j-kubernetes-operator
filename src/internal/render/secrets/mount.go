package secrets

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// MountableLabel is required on Secrets the operator will mount into Neo4j pods (NEO-005).
// Namespace owners set this; CR authors cannot escalate by naming arbitrary Secrets.
const MountableLabel = "neo4j.com/mountable-by-operator"

const MountableLabelValue = "true"

// AllowedForLabel delegates a BYO auth Secret to one Neo4j CR name (ADD-01).
// Operator-managed auth Secrets use app.kubernetes.io/managed-by + instance instead.
const AllowedForLabel = "neo4j.com/allowed-for"

// ValidateSpec checks CR-only mount policy (no API reads): trustedCert projection
// allowlist and required items for secret mounts (NEO-005 §1, §3).
func ValidateSpec(neo4j *neo4jv1beta1.Neo4j) error {
	if err := validateTrustedCertSources(neo4j); err != nil {
		return err
	}
	if neo4j.Spec.Storage != nil {
		for name, sm := range neo4j.Spec.Storage.SecretMounts {
			if len(sm.Items) == 0 {
				return fmt.Errorf("storage.secretMounts[%q]: items is required (mount only named keys; NEO-005)", name)
			}
		}
	}
	return nil
}

func validateTrustedCertSources(neo4j *neo4jv1beta1.Neo4j) error {
	if neo4j.Spec.Trust == nil || neo4j.Spec.Trust.Certificates == nil {
		return nil
	}
	certs := neo4j.Spec.Trust.Certificates
	for _, p := range []*neo4jv1beta1.TLSPolicySpec{certs.Bolt, certs.HTTPS, certs.Cluster} {
		if p == nil || p.TrustedCerts == nil {
			continue
		}
		for i, src := range p.TrustedCerts.Sources {
			field := fmt.Sprintf("trust.certificates.*.trustedCerts.sources[%d]", i)
			n := 0
			if src.Secret != nil {
				n++
				if src.Secret.Name == "" {
					return fmt.Errorf("%s: secret.name is required", field)
				}
				if len(src.Secret.Items) == 0 {
					return fmt.Errorf("%s: secret.items is required (mount only named keys; NEO-005)", field)
				}
			}
			if src.ConfigMap != nil {
				n++
				if src.ConfigMap.Name == "" {
					return fmt.Errorf("%s: configMap.name is required", field)
				}
				if len(src.ConfigMap.Items) == 0 {
					return fmt.Errorf("%s: configMap.items is required (mount only named keys; NEO-005)", field)
				}
			}
			if src.ServiceAccountToken != nil {
				return fmt.Errorf("%s: serviceAccountToken projections are not allowed", field)
			}
			if src.DownwardAPI != nil {
				return fmt.Errorf("%s: downwardAPI projections are not allowed", field)
			}
			if src.ClusterTrustBundle != nil {
				return fmt.Errorf("%s: clusterTrustBundle projections are not allowed", field)
			}
			if n != 1 {
				return fmt.Errorf("%s: exactly one of secret or configMap is required", field)
			}
		}
	}
	return nil
}

// ReferencedMountSecrets returns Secret names the CR asks the operator to mount
// (excludes Secrets the operator itself creates for generatePassword).
func ReferencedMountSecrets(neo4j *neo4jv1beta1.Neo4j) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(n string) {
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}

	if neo4j.Spec.Auth != nil && neo4j.Spec.Auth.PasswordSecretRef != nil {
		add(neo4j.Spec.Auth.PasswordSecretRef.Name)
	}
	if neo4j.Spec.Storage != nil {
		for name, sm := range neo4j.Spec.Storage.SecretMounts {
			if sm.SecretName != "" {
				add(sm.SecretName)
			} else {
				add(name)
			}
		}
	}
	for _, n := range rendertrust.BYOSecretNames(neo4j) {
		add(n)
	}
	if neo4j.Spec.PluginDefinitions != nil {
		for _, def := range neo4j.Spec.PluginDefinitions {
			add(def.LicenseSecretRef)
		}
	}
	sort.Strings(names)
	return names
}

// EnsureMountable verifies each referenced mount Secret exists and carries MountableLabel.
// BYO auth Secrets (passwordSecretRef) must also be delegated to this CR (ADD-01).
func EnsureMountable(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j) error {
	authRef := ""
	if neo4j.Spec.Auth != nil && neo4j.Spec.Auth.PasswordSecretRef != nil {
		authRef = neo4j.Spec.Auth.PasswordSecretRef.Name
	}
	for _, name := range ReferencedMountSecrets(neo4j) {
		var secret corev1.Secret
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: neo4j.Namespace}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("secret %q not found (required for Neo4j mount)", name)
			}
			return fmt.Errorf("get secret %q: %w", name, err)
		}
		if err := RequireMountable(&secret); err != nil {
			return err
		}
		if name == authRef {
			if err := RequireAuthSecretDelegated(&secret, neo4j); err != nil {
				return err
			}
		}
	}
	return nil
}

// RequireMountable fails unless the Secret opts in to operator mounts.
func RequireMountable(secret *corev1.Secret) error {
	if secret.Labels != nil && secret.Labels[MountableLabel] == MountableLabelValue {
		return nil
	}
	return fmt.Errorf("secret %q is missing label %s=%s (namespace owner must opt in before the operator mounts it; NEO-005)",
		secret.Name, MountableLabel, MountableLabelValue)
}

// RequireAuthSecretDelegated fails unless the Secret is operator-managed for this CR
// or the namespace owner labeled it AllowedForLabel=<neo4j.Name> (ADD-01).
func RequireAuthSecretDelegated(secret *corev1.Secret, neo4j *neo4jv1beta1.Neo4j) error {
	if secret.Labels != nil {
		if secret.Labels[render.LabelManagedBy] == render.ManagedByValue &&
			secret.Labels[render.LabelInstance] == neo4j.Name {
			return nil
		}
		if secret.Labels[AllowedForLabel] == neo4j.Name {
			return nil
		}
	}
	return fmt.Errorf("auth secret %q is not delegated to Neo4j %q (need label %s=%s, or an operator-managed auth Secret; ADD-01)",
		secret.Name, neo4j.Name, AllowedForLabel, neo4j.Name)
}

// WithMountableLabel copies labels and ensures the opt-in label is set (operator-created Secrets).
func WithMountableLabel(labels map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range labels {
		out[k] = v
	}
	out[MountableLabel] = MountableLabelValue
	return out
}
