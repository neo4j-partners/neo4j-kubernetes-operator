package trust

import (
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	certDir           = "/var/lib/neo4j/certificates"
	defaultPrivateKey = "private.key"
	defaultPublicCert = "public.crt"
	secretVolumeMode  = int32(0o440)

	// cert-manager issues kubernetes.io/tls Secrets with these fixed data keys.
	certManagerKeyKey  = "tls.key"
	certManagerCertKey = "tls.crt"
)

// tlsPolicy is one Neo4j SSL framework policy (Helm ssl.{name} parity).
type tlsPolicy struct {
	name string
	get  func(*neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec
	// clusterOnly: mount/conf only when topology.mode is Cluster (TLS-003).
	clusterOnly bool
	// forceClientAuth: if non-empty, always emit this client_auth (cluster → REQUIRE).
	forceClientAuth string
	// setBoltTLSLevel: when material present, set server.bolt.tls_level=REQUIRED.
	setBoltTLSLevel bool
	// certManagerSANs: cert-manager Certificates for this policy carry the caller-facing
	// SANs (trust.certManager.dnsNames, ingress hosts). Cluster certs stay on member
	// discovery DNS only (BDR-006).
	certManagerSANs bool
}

var tlsPolicies = []tlsPolicy{
	{
		name:            "cluster",
		get:             func(c *neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec { return c.Cluster },
		clusterOnly:     true,
		forceClientAuth: "REQUIRE",
	},
	{
		name:            "https",
		get:             func(c *neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec { return c.HTTPS },
		certManagerSANs: true,
	},
	{
		name:            "bolt",
		get:             func(c *neo4jv1beta1.TrustCertificatesSpec) *neo4jv1beta1.TLSPolicySpec { return c.Bolt },
		setBoltTLSLevel: true,
		certManagerSANs: true,
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
	for _, p := range tlsPolicies {
		if p.name == name {
			return p.get(c)
		}
	}
	return nil
}

// CertManagerEnabled reports whether the operator provisions cert-manager Certificates
// instead of consuming user-supplied Secrets (BDR-006; default false).
func CertManagerEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return TrustEnabled(neo4j) &&
		neo4j.Spec.Trust.CertManager != nil && neo4j.Spec.Trust.CertManager.Enabled
}

// Material is the effective Secret coordinates for one policy's key and certificate.
// It collapses the two shapes BDR-006 allows — BYO (privateKey + publicCertificate, each
// naming its own Secret and data key) and cert-manager (a single secretName holding
// tls.key + tls.crt) — so mounting, conf keys, and Secret checks need one code path.
type Material struct {
	KeySecret  string
	KeyPath    string
	CertSecret string
	CertPath   string
	// Provisioned marks cert-manager material: the operator creates the Certificate and
	// cert-manager fills the Secret, so it is legitimately absent until first issuance
	// and must not be treated as a user error.
	Provisioned bool
}

// PolicyMaterial resolves the named policy's certificate material, if any is configured.
func PolicyMaterial(neo4j *neo4jv1beta1.Neo4j, policy string) (Material, bool) {
	return materialOf(neo4j, policyOf(neo4j, policy))
}

func materialOf(neo4j *neo4jv1beta1.Neo4j, p *neo4jv1beta1.TLSPolicySpec) (Material, bool) {
	if p == nil {
		return Material{}, false
	}
	if CertManagerEnabled(neo4j) {
		if p.SecretName == "" {
			return Material{}, false
		}
		return Material{
			KeySecret:   p.SecretName,
			KeyPath:     certManagerKeyKey,
			CertSecret:  p.SecretName,
			CertPath:    certManagerCertKey,
			Provisioned: true,
		}, true
	}
	if p.PrivateKey == nil || p.PrivateKey.SecretName == "" ||
		p.PublicCertificate == nil || p.PublicCertificate.SecretName == "" {
		return Material{}, false
	}
	return Material{
		KeySecret:  p.PrivateKey.SecretName,
		KeyPath:    subPathOr(p.PrivateKey.SubPath, defaultPrivateKey),
		CertSecret: p.PublicCertificate.SecretName,
		CertPath:   subPathOr(p.PublicCertificate.SubPath, defaultPublicCert),
	}, true
}

func subPathOr(subPath, def string) string {
	if subPath != "" {
		return subPath
	}
	return def
}

func materialPresent(neo4j *neo4jv1beta1.Neo4j, p *neo4jv1beta1.TLSPolicySpec) bool {
	_, ok := materialOf(neo4j, p)
	return ok
}

func policyActive(neo4j *neo4jv1beta1.Neo4j, def tlsPolicy) (*neo4jv1beta1.TLSPolicySpec, Material, bool) {
	c := certificates(neo4j)
	if c == nil {
		return nil, Material{}, false
	}
	if def.clusterOnly && !render.IsClusterMode(neo4j) {
		return nil, Material{}, false
	}
	spec := def.get(c)
	mat, ok := materialOf(neo4j, spec)
	if !ok {
		return nil, Material{}, false
	}
	return spec, mat, true
}

// AppendVolumes mounts TLS material for enabled policies (Helm _ssl.tpl parity).
func AppendVolumes(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if !TrustEnabled(ctx.Neo4j) {
		return
	}
	for _, def := range tlsPolicies {
		spec, mat, ok := policyActive(ctx.Neo4j, def)
		if !ok {
			continue
		}
		appendPolicyVolumes(def.name, spec, mat, container, podSpec)
	}
}

func appendPolicyVolumes(policy string, spec *neo4jv1beta1.TLSPolicySpec, mat Material, container *corev1.Container, podSpec *corev1.PodSpec) {
	mode := secretVolumeMode
	certVol := policy + "-cert"
	keyVol := policy + "-key"

	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: certVol,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  mat.CertSecret,
					DefaultMode: &mode,
				},
			},
		},
		corev1.Volume{
			Name: keyVol,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  mat.KeySecret,
					DefaultMode: &mode,
				},
			},
		},
	)
	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{
			Name:      certVol,
			MountPath: path.Join(certDir, policy, defaultPublicCert),
			SubPath:   mat.CertPath,
			ReadOnly:  true,
		},
		corev1.VolumeMount{
			Name:      keyVol,
			MountPath: path.Join(certDir, policy, defaultPrivateKey),
			SubPath:   mat.KeyPath,
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
	for _, def := range tlsPolicies {
		spec, _, ok := policyActive(ctx.Neo4j, def)
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

// requireTrustedIfMTLS enforces TLS-004. Optional is included deliberately: it still
// accepts client certificates, so without a CA bundle to verify them against every
// presented certificate is rejected — a silently broken mTLS setup.
func requireTrustedIfMTLS(policy string, p *neo4jv1beta1.TLSPolicySpec) error {
	if p.ClientAuth != neo4jv1beta1.TLSClientAuth("Require") &&
		p.ClientAuth != neo4jv1beta1.TLSClientAuth("Optional") {
		return nil
	}
	if p.TrustedCerts == nil || len(p.TrustedCerts.Sources) == 0 {
		return fmt.Errorf("trust.certificates.%s.clientAuth %s requires trustedCerts.sources (TLS-004)",
			policy, p.ClientAuth)
	}
	return nil
}

func requireMaterial(neo4j *neo4jv1beta1.Neo4j, policy string, p *neo4jv1beta1.TLSPolicySpec) error {
	if !materialPresent(neo4j, p) {
		if CertManagerEnabled(neo4j) {
			return fmt.Errorf("trust.certificates.%s requires secretName when trust.certManager.enabled (TLS-002c)", policy)
		}
		return fmt.Errorf("trust.certificates.%s requires privateKey.secretName and publicCertificate.secretName", policy)
	}
	return requireTrustedIfMTLS(policy, p)
}

// Validate runs all trust shape checks (cluster / https / bolt coupling, cert-manager).
func Validate(neo4j *neo4jv1beta1.Neo4j) error {
	if err := ValidateHTTPSShape(neo4j); err != nil {
		return err
	}
	if err := ValidateBoltShape(neo4j); err != nil {
		return err
	}
	if err := ValidateClusterShape(neo4j); err != nil {
		return err
	}
	return validateCertManager(neo4j)
}

// validateCertManager re-checks TLS-001 and TLS-007 outside CEL so the coupling still
// holds when the operator runs against a CRD that predates those rules.
func validateCertManager(neo4j *neo4jv1beta1.Neo4j) error {
	if !CertManagerEnabled(neo4j) {
		return nil
	}
	cm := neo4j.Spec.Trust.CertManager
	if cm.IssuerRef == nil || cm.IssuerRef.Name == "" {
		return fmt.Errorf("trust.certManager.enabled requires issuerRef.name (TLS-001)")
	}
	if cm.IncludeIngressHosts && len(ingressHosts(neo4j)) == 0 {
		return fmt.Errorf("trust.certManager.includeIngressHosts requires at least one connectivity.ingress.rules[].host (TLS-007)")
	}
	return nil
}

// ValidateClusterShape validates cluster TLS when mode is Cluster, or Standalone bolt/https-only trust.
func ValidateClusterShape(neo4j *neo4jv1beta1.Neo4j) error {
	if !TrustEnabled(neo4j) {
		return nil
	}
	if !render.IsClusterMode(neo4j) {
		return validateStandaloneShape(neo4j)
	}
	p := policyOf(neo4j, "cluster")
	if err := requireMaterial(neo4j, "cluster", p); err != nil {
		return err
	}
	if p.ClientAuth == neo4jv1beta1.TLSClientAuth("None") {
		return fmt.Errorf("trust.certificates.cluster.clientAuth cannot be None (cluster mTLS requires Require)")
	}
	return nil
}

// Standalone: no cluster policy; require bolt and/or https material.
func validateStandaloneShape(neo4j *neo4jv1beta1.Neo4j) error {
	if policyOf(neo4j, "cluster") != nil {
		return fmt.Errorf("trust.certificates.cluster is only valid when topology.mode is Cluster")
	}
	boltOK := materialPresent(neo4j, policyOf(neo4j, "bolt"))
	httpsOK := materialPresent(neo4j, policyOf(neo4j, "https"))
	if !boltOK && !httpsOK {
		return fmt.Errorf("trust.enabled on Standalone requires trust.certificates.bolt and/or trust.certificates.https")
	}
	if p := policyOf(neo4j, "bolt"); p != nil {
		if err := requireMaterial(neo4j, "bolt", p); err != nil {
			return err
		}
	}
	if p := policyOf(neo4j, "https"); p != nil {
		if err := requireMaterial(neo4j, "https", p); err != nil {
			return err
		}
	}
	return nil
}

// ValidateHTTPSShape enforces TLS-LISTENER-001 and TLS-LISTENER-007: listeners.https
// requires https material, plus bolt material because Browser is served over HTTPS and
// browsers block its plaintext WebSocket to Bolt as mixed content.
func ValidateHTTPSShape(neo4j *neo4jv1beta1.Neo4j) error {
	ctx := render.Context{Neo4j: neo4j}
	if !ctx.HTTPSEnabled() {
		return nil
	}
	if !TrustEnabled(neo4j) {
		return fmt.Errorf("connectivity.listeners.https requires trust.enabled and trust.certificates.https")
	}
	if err := requireMaterial(neo4j, "https", policyOf(neo4j, "https")); err != nil {
		return fmt.Errorf("connectivity.listeners.https: %w", err)
	}
	if !materialPresent(neo4j, policyOf(neo4j, "bolt")) {
		return fmt.Errorf("connectivity.listeners.https requires trust.certificates.bolt (Neo4j Browser uses bolt+s over HTTPS; TLS-LISTENER-007)")
	}
	return ValidateBoltShape(neo4j)
}

// ValidateBoltShape validates bolt material when the bolt block is present.
func ValidateBoltShape(neo4j *neo4jv1beta1.Neo4j) error {
	p := policyOf(neo4j, "bolt")
	if p == nil {
		return nil
	}
	return requireMaterial(neo4j, "bolt", p)
}

// BoltTLSEnabled is true when bolt material is present (server.bolt.tls_level REQUIRED).
func BoltTLSEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return materialPresent(neo4j, policyOf(neo4j, "bolt"))
}

// SecretKeyNeed is a Secret name + data key required for a TLS mount.
type SecretKeyNeed struct {
	SecretName string
	Key        string
}

type secretKeySet struct {
	out  []SecretKeyNeed
	seen map[string]struct{}
}

func (s *secretKeySet) add(secret, key string) {
	if secret == "" || key == "" {
		return
	}
	id := secret + "\x00" + key
	if _, ok := s.seen[id]; ok {
		return
	}
	s.seen[id] = struct{}{}
	s.out = append(s.out, SecretKeyNeed{SecretName: secret, Key: key})
}

// provisionedSecretNames is the set of Secret names cert-manager fills for this CR. Any
// reference to one of these — including a trustedCerts source, since cert-manager publishes
// ca.crt next to the leaf — must be treated as pending issuance rather than user input, or
// the first reconcile deadlocks: the Secret cannot exist until the operator has created the
// Certificate, which happens after the up-front mount checks.
func provisionedSecretNames(neo4j *neo4jv1beta1.Neo4j) map[string]struct{} {
	if !CertManagerEnabled(neo4j) {
		return nil
	}
	out := map[string]struct{}{}
	for _, def := range tlsPolicies {
		_, mat, ok := policyActive(neo4j, def)
		if !ok || !mat.Provisioned {
			continue
		}
		out[mat.KeySecret] = struct{}{}
		out[mat.CertSecret] = struct{}{}
	}
	return out
}

// collectSecretKeys walks active policies and splits every referenced Secret data key into
// the ones the user must supply and the ones cert-manager will publish.
func collectSecretKeys(neo4j *neo4jv1beta1.Neo4j) (user, provisioned []SecretKeyNeed) {
	if !TrustEnabled(neo4j) {
		return nil, nil
	}
	fromCertManager := provisionedSecretNames(neo4j)
	userKeys := &secretKeySet{seen: map[string]struct{}{}}
	provisionedKeys := &secretKeySet{seen: map[string]struct{}{}}
	target := func(secret string) *secretKeySet {
		if _, ok := fromCertManager[secret]; ok {
			return provisionedKeys
		}
		return userKeys
	}

	for _, def := range tlsPolicies {
		spec, mat, ok := policyActive(neo4j, def)
		if !ok {
			continue
		}
		target(mat.KeySecret).add(mat.KeySecret, mat.KeyPath)
		target(mat.CertSecret).add(mat.CertSecret, mat.CertPath)
		if spec.TrustedCerts == nil {
			continue
		}
		for _, src := range spec.TrustedCerts.Sources {
			if src.Secret == nil || src.Secret.Name == "" {
				continue
			}
			// Whole-secret projection — keys unknown; existence checked via BYOSecretNames.
			for _, item := range src.Secret.Items {
				target(src.Secret.Name).add(src.Secret.Name, item.Key)
			}
		}
	}
	return userKeys.out, provisionedKeys.out
}

// RequiredSecretKeys lists Secret data keys the user must supply for active policies.
func RequiredSecretKeys(neo4j *neo4jv1beta1.Neo4j) []SecretKeyNeed {
	user, _ := collectSecretKeys(neo4j)
	return user
}

// ProvisionedSecretKeys lists the Secret data keys cert-manager must publish before the
// pods can mount TLS material. Absence means issuance is pending, not invalid input.
func ProvisionedSecretKeys(neo4j *neo4jv1beta1.Neo4j) []SecretKeyNeed {
	_, provisioned := collectSecretKeys(neo4j)
	return provisioned
}

// BYOSecretNames returns the user-supplied Secret names enabled policies reference.
// cert-manager target Secrets are omitted: the operator creates them via a Certificate,
// so they are neither user-authored nor present on the first reconcile.
func BYOSecretNames(neo4j *neo4jv1beta1.Neo4j) []string {
	if !TrustEnabled(neo4j) {
		return nil
	}
	fromCertManager := provisionedSecretNames(neo4j)
	var names []string
	seen := map[string]struct{}{}
	add := func(n string) {
		if n == "" {
			return
		}
		if _, ok := fromCertManager[n]; ok {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	for _, def := range tlsPolicies {
		spec, mat, ok := policyActive(neo4j, def)
		if !ok {
			continue
		}
		add(mat.KeySecret)
		add(mat.CertSecret)
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
