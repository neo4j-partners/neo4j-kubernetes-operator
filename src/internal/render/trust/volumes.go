package trust

import (
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
)

const (
	certDir           = "/var/lib/neo4j/certificates"
	defaultPrivateKey = "private.key"
	defaultPublicCert = "public.crt"
	secretVolumeMode  = int32(0o440)
)

// byoPolicy is one Neo4j SSL framework policy (Helm ssl.{name} parity).
type byoPolicy struct {
	name string
	get  func(*neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec
	// clusterOnly: mount/conf only when topology.mode is Cluster (TLS-003).
	clusterOnly bool
	// forceClientAuth: if non-empty, always emit this client_auth (cluster → REQUIRE).
	forceClientAuth string
	// setBoltTLSLevel: when material present, set server.bolt.tls_level=REQUIRED.
	setBoltTLSLevel bool
}

var byoPolicies = []byoPolicy{
	{
		name:            "cluster",
		get:             func(c *neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec { return c.Cluster },
		clusterOnly:     true,
		forceClientAuth: "REQUIRE",
	},
	{
		name: "https",
		get:  func(c *neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec { return c.HTTPS },
	},
	{
		name:            "bolt",
		get:             func(c *neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec { return c.Bolt },
		setBoltTLSLevel: true,
	},
}

// TrustEnabled reports whether TLS material should be applied.
func TrustEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j != nil && neo4j.Spec.Trust != nil && neo4j.Spec.Trust.Enabled
}

func certificates(neo4j *neo4jv1beta1.Neo4j) *neo4jv1beta1.TrustCertificatesSpec {
	if !TrustEnabled(neo4j) {
		return nil
	}
	return neo4j.Spec.Trust.Certificates
}

func policyOf(neo4j *neo4jv1beta1.Neo4j, name string) *neo4jv1beta1.TLSPolicySpec {
	c := certificates(neo4j)
	if c == nil {
		return nil
	}
	for _, p := range byoPolicies {
		if p.name == name {
			return p.get(c)
		}
	}
	return nil
}

// PolicyMaterialPresent is true when BYO key+cert secret names are set.
func PolicyMaterialPresent(p *neo4jv1beta1.TLSPolicySpec) bool {
	return p != nil &&
		p.PrivateKey != nil && p.PrivateKey.SecretName != "" &&
		p.PublicCertificate != nil && p.PublicCertificate.SecretName != ""
}

func policyActive(neo4j *neo4jv1beta1.Neo4j, def byoPolicy) (*neo4jv1beta1.TLSPolicySpec, bool) {
	c := certificates(neo4j)
	if c == nil {
		return nil, false
	}
	if def.clusterOnly && !render.IsClusterMode(neo4j) {
		return nil, false
	}
	spec := def.get(c)
	if !PolicyMaterialPresent(spec) {
		return nil, false
	}
	return spec, true
}

// AppendVolumes mounts BYO TLS secrets for enabled policies (Helm _ssl.tpl parity).
func AppendVolumes(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if !TrustEnabled(ctx.Neo4j) {
		return
	}
	for _, def := range byoPolicies {
		spec, ok := policyActive(ctx.Neo4j, def)
		if !ok {
			continue
		}
		appendPolicyVolumes(def.name, spec, container, podSpec)
	}
}

func appendPolicyVolumes(policy string, spec *neo4jv1beta1.TLSPolicySpec, container *corev1.Container, podSpec *corev1.PodSpec) {
	mode := secretVolumeMode
	certVol := policy + "-cert"
	keyVol := policy + "-key"
	certSub := defaultPublicCert
	if spec.PublicCertificate.SubPath != "" {
		certSub = spec.PublicCertificate.SubPath
	}
	keySub := defaultPrivateKey
	if spec.PrivateKey.SubPath != "" {
		keySub = spec.PrivateKey.SubPath
	}

	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: certVol,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.PublicCertificate.SecretName,
					DefaultMode: &mode,
				},
			},
		},
		corev1.Volume{
			Name: keyVol,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.PrivateKey.SecretName,
					DefaultMode: &mode,
				},
			},
		},
	)
	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{
			Name:      certVol,
			MountPath: path.Join(certDir, policy, "public.crt"),
			SubPath:   certSub,
			ReadOnly:  true,
		},
		corev1.VolumeMount{
			Name:      keyVol,
			MountPath: path.Join(certDir, policy, "private.key"),
			SubPath:   keySub,
			ReadOnly:  true,
		},
	)

	if spec.TrustedCerts == nil || len(spec.TrustedCerts.Sources) == 0 {
		return
	}
	trustedVol := policy + "-trusted"
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: trustedVol,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &mode,
				Sources:     spec.TrustedCerts.Sources,
			},
		},
	})
	// Mount each cert as a file via subPath. A directory mount exposes K8s "..data"
	// entries that Neo4j tries (and fails) to parse as PEM.
	mounted := false
	for _, src := range spec.TrustedCerts.Sources {
		if src.Secret == nil {
			continue
		}
		for _, item := range src.Secret.Items {
			file := item.Path
			if file == "" {
				file = item.Key
			}
			if file == "" {
				continue
			}
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      trustedVol,
				MountPath: path.Join(certDir, policy, "trusted", file),
				SubPath:   file,
				ReadOnly:  true,
			})
			mounted = true
		}
	}
	if !mounted {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      trustedVol,
			MountPath: path.Join(certDir, policy, "trusted"),
			ReadOnly:  true,
		})
	}
}

// Neo4jConfKeys returns operator-owned ssl policy keys (Helm neo4j-config.yaml parity).
func Neo4jConfKeys(ctx render.Context) map[string]string {
	if !TrustEnabled(ctx.Neo4j) {
		return nil
	}
	keys := map[string]string{
		"internal.dbms.ssl.system.ignore_dot_files": "true",
	}
	for _, def := range byoPolicies {
		spec, ok := policyActive(ctx.Neo4j, def)
		if !ok {
			continue
		}
		keys["dbms.ssl.policy."+def.name+".enabled"] = "true"
		if def.forceClientAuth != "" {
			keys["dbms.ssl.policy."+def.name+".client_auth"] = def.forceClientAuth
		} else {
			keys["dbms.ssl.policy."+def.name+".client_auth"] = clientAuthValue(spec.ClientAuth)
		}
		if def.setBoltTLSLevel {
			keys["server.bolt.tls_level"] = "REQUIRED"
		}
	}
	if ctx.Neo4j.Spec.Trust.Reload != nil && ctx.Neo4j.Spec.Trust.Reload.Enabled {
		keys["dbms.security.tls_reload_enabled"] = "true"
	}
	return keys
}

func clientAuthValue(auth neo4jv1beta1.TLSClientAuth) string {
	switch auth {
	case "Require":
		return "REQUIRE"
	case "Optional":
		return "OPTIONAL"
	case "None":
		return "NONE"
	default:
		return "NONE"
	}
}

func requireTrustedIfMTLS(policy string, p *neo4jv1beta1.TLSPolicySpec) error {
	if p.ClientAuth != neo4jv1beta1.TLSClientAuth("Require") {
		return nil
	}
	if p.TrustedCerts == nil || len(p.TrustedCerts.Sources) == 0 {
		return fmt.Errorf("trust.certificates.%s.clientAuth Require requires trustedCerts.sources", policy)
	}
	return nil
}

func requireBYOMaterial(policy string, p *neo4jv1beta1.TLSPolicySpec) error {
	if PolicyMaterialPresent(p) {
		return requireTrustedIfMTLS(policy, p)
	}
	return fmt.Errorf("trust.certificates.%s requires privateKey.secretName and publicCertificate.secretName", policy)
}

// ValidateBYO runs all BYO trust checks (cluster / https / bolt coupling).
func ValidateBYO(neo4j *neo4jv1beta1.Neo4j) error {
	if err := ValidateHTTPSBYOShape(neo4j); err != nil {
		return err
	}
	if err := ValidateBoltBYOShape(neo4j); err != nil {
		return err
	}
	return ValidateClusterBYOShape(neo4j)
}

// ValidateClusterBYOShape returns a user-facing error if cluster trust is incomplete.
func ValidateClusterBYOShape(neo4j *neo4jv1beta1.Neo4j) error {
	if !TrustEnabled(neo4j) {
		return nil
	}
	if !render.IsClusterMode(neo4j) {
		return fmt.Errorf("trust.enabled is only supported for topology.mode Cluster in V1")
	}
	p := policyOf(neo4j, "cluster")
	if err := requireBYOMaterial("cluster", p); err != nil {
		return err
	}
	if p.ClientAuth == neo4jv1beta1.TLSClientAuth("None") {
		return fmt.Errorf("trust.certificates.cluster.clientAuth cannot be None (cluster mTLS requires Require)")
	}
	return nil
}

// ValidateHTTPSBYOShape enforces TLS-LISTENER-001: listeners.https requires https (+ bolt for Browser).
func ValidateHTTPSBYOShape(neo4j *neo4jv1beta1.Neo4j) error {
	ctx := render.Context{Neo4j: neo4j}
	if !ctx.HTTPSEnabled() {
		return nil
	}
	if !TrustEnabled(neo4j) {
		return fmt.Errorf("connectivity.listeners.https requires trust.enabled and trust.certificates.https")
	}
	if err := requireBYOMaterial("https", policyOf(neo4j, "https")); err != nil {
		return fmt.Errorf("connectivity.listeners.https: %w", err)
	}
	if !PolicyMaterialPresent(policyOf(neo4j, "bolt")) {
		return fmt.Errorf("connectivity.listeners.https requires trust.certificates.bolt (Neo4j Browser uses bolt+s over HTTPS)")
	}
	return ValidateBoltBYOShape(neo4j)
}

// ValidateBoltBYOShape validates bolt BYO material when the bolt block is present.
func ValidateBoltBYOShape(neo4j *neo4jv1beta1.Neo4j) error {
	p := policyOf(neo4j, "bolt")
	if p == nil {
		return nil
	}
	return requireBYOMaterial("bolt", p)
}

// BYOSecretNames returns Secret names referenced by enabled BYO policies.
func BYOSecretNames(neo4j *neo4jv1beta1.Neo4j) []string {
	if !TrustEnabled(neo4j) {
		return nil
	}
	var names []string
	seen := map[string]struct{}{}
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
	for _, def := range byoPolicies {
		spec, ok := policyActive(neo4j, def)
		if !ok {
			continue
		}
		add(spec.PrivateKey.SecretName)
		add(spec.PublicCertificate.SecretName)
		if spec.TrustedCerts == nil {
			continue
		}
		for _, src := range spec.TrustedCerts.Sources {
			if src.Secret != nil {
				add(src.Secret.Name)
			}
		}
	}
	return names
}
