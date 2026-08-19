package trust

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const (
	certManagerGroup   = "cert-manager.io"
	certManagerVersion = "v1"
	certificateKind    = "Certificate"

	defaultIssuerKind = "ClusterIssuer"
)

// CertificateGVK is the cert-manager Certificate type.
var CertificateGVK = schema.GroupVersionKind{
	Group:   certManagerGroup,
	Version: certManagerVersion,
	Kind:    certificateKind,
}

// CertificateName is the owned Certificate for one SSL policy. It is not the target
// Secret name — that stays user-chosen via certificates.{policy}.secretName so the
// Secret can be referenced from outside the CR.
func CertificateName(neo4j *neo4jv1beta1.Neo4j, policy string) string {
	return neo4j.Name + "-" + policy + "-tls"
}

// PolicyCertificate is one rendered cert-manager Certificate plus the policy it serves.
type PolicyCertificate struct {
	Policy string
	Object *unstructured.Unstructured
}

// Certificates renders one cert-manager Certificate per active policy (BDR-006).
// Unstructured keeps cert-manager out of go.mod, as with the Prometheus ServiceMonitor.
//
// secretLabels are stamped onto the issued Secret via secretTemplate so the mount-policy
// opt-in (NEO-005) holds for Secrets the operator provisions, exactly as it does for
// operator-generated auth Secrets.
func Certificates(neo4j *neo4jv1beta1.Neo4j, secretLabels map[string]string) []PolicyCertificate {
	if !CertManagerEnabled(neo4j) {
		return nil
	}
	var out []PolicyCertificate
	for _, def := range tlsPolicies {
		spec, mat, ok := policyActive(neo4j, def)
		if !ok {
			continue
		}
		out = append(out, PolicyCertificate{
			Policy: def.name,
			Object: certificateFor(neo4j, def, spec, mat, secretLabels),
		})
	}
	return out
}

func certificateFor(
	neo4j *neo4jv1beta1.Neo4j,
	def tlsPolicy,
	spec *neo4jv1beta1.TLSPolicySpec,
	mat Material,
	secretLabels map[string]string,
) *unstructured.Unstructured {
	cm := neo4j.Spec.Trust.CertManager

	issuerKind := cm.IssuerRef.Kind
	if issuerKind == "" {
		issuerKind = defaultIssuerKind
	}

	certSpec := map[string]interface{}{
		"secretName": mat.CertSecret,
		"issuerRef": map[string]interface{}{
			"name":  cm.IssuerRef.Name,
			"kind":  issuerKind,
			"group": certManagerGroup,
		},
		"dnsNames": toInterfaceSlice(dnsNamesFor(neo4j, def, spec)),
		"usages":   toInterfaceSlice(usagesFor(def)),
	}
	if len(secretLabels) > 0 {
		certSpec["secretTemplate"] = map[string]interface{}{
			"labels": toInterfaceMap(secretLabels),
		}
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(CertificateGVK)
	u.SetName(CertificateName(neo4j, def.name))
	u.SetNamespace(neo4j.Namespace)
	u.SetLabels(render.ClientServiceContext(neo4j).CommonLabels("trust"))
	_ = unstructured.SetNestedMap(u.Object, certSpec, "spec")
	return u
}

// usagesFor requests client auth only for the cluster policy: members authenticate to
// each other, so each certificate is presented from both ends of the connection.
func usagesFor(def tlsPolicy) []string {
	usages := []string{"digital signature", "key encipherment", "server auth"}
	if def.forceClientAuth != "" {
		usages = append(usages, "client auth")
	}
	return usages
}

// dnsNamesFor assembles the SANs for one policy. These must match the names that are
// actually dialed, or verification fails: the client Service for callers and the
// per-member Service FQDNs the operator writes into server.*.advertised_address.
func dnsNamesFor(neo4j *neo4jv1beta1.Neo4j, def tlsPolicy, spec *neo4jv1beta1.TLSPolicySpec) []string {
	names := &stringSet{seen: map[string]struct{}{}}

	for _, n := range inClusterNames(neo4j, def) {
		names.add(n)
	}
	// Per-policy dnsNames apply to that policy only.
	for _, n := range spec.DNSNames {
		names.add(n)
	}
	if def.certManagerSANs {
		cm := neo4j.Spec.Trust.CertManager
		for _, n := range cm.DNSNames {
			names.add(n)
		}
		if cm.IncludeIngressHosts {
			for _, h := range ingressHosts(neo4j) {
				names.add(h)
			}
		}
	}
	return names.out
}

// inClusterNames enumerates the operator-derived DNS names for a policy. Members are
// listed individually rather than covered by a namespace wildcard, so a certificate
// never validates a host outside this deployment. Scaling changes the Certificate spec
// and cert-manager reissues.
func inClusterNames(neo4j *neo4jv1beta1.Neo4j, def tlsPolicy) []string {
	clientCtx := render.ClientServiceContext(neo4j)
	ns := clientCtx.Namespace()
	domain := clientCtx.ClusterDomain()
	names := &stringSet{seen: map[string]struct{}{}}

	if def.clusterOnly {
		// Cluster traffic dials the per-member internals Services (SERVICE_NEO4J_INTERNALS).
		forEachMember(neo4j, func(ctx render.Context, podName string) {
			names.add(fmt.Sprintf("%s.%s.svc.%s", ctx.MemberInternalsServiceName(podName), ns, domain))
		})
		return names.out
	}

	// Callers reach Bolt/HTTPS through the client Service. The operator's own admin dial
	// uses the short form (formation.ClientBoltURI), so both must be present.
	names.add(fmt.Sprintf("%s.%s.svc", clientCtx.ClientServiceName(), ns))
	names.add(fmt.Sprintf("%s.%s.svc.%s", clientCtx.ClientServiceName(), ns, domain))
	forEachMember(neo4j, func(ctx render.Context, podName string) {
		names.add(ctx.HeadlessServiceDomain())
		// Per-member client FQDN — server.bolt.advertised_address (SERVICE_NEO4J).
		names.add(fmt.Sprintf("%s.%s.svc.%s", podName, ns, domain))
	})
	return names.out
}

func forEachMember(neo4j *neo4jv1beta1.Neo4j, fn func(ctx render.Context, podName string)) {
	for _, pool := range render.ActivePools(neo4j) {
		ctx := render.ContextForPool(neo4j, pool)
		replicas := ctx.PoolReplicas()
		for i := int32(0); i < replicas; i++ {
			fn(ctx, ctx.PodName(i))
		}
	}
}

// ingressHosts lists non-empty hosts from operator-managed Ingress rules. Hosts are a
// read-only input for Certificate SANs (BDR-006) — the operator does not manage Ingress
// TLS Secrets.
func ingressHosts(neo4j *neo4jv1beta1.Neo4j) []string {
	if neo4j.Spec.Connectivity == nil || neo4j.Spec.Connectivity.Ingress == nil {
		return nil
	}
	hosts := &stringSet{seen: map[string]struct{}{}}
	for _, rule := range neo4j.Spec.Connectivity.Ingress.Rules {
		hosts.add(rule.Host)
	}
	return hosts.out
}

type stringSet struct {
	out  []string
	seen map[string]struct{}
}

func (s *stringSet) add(v string) {
	if v == "" {
		return
	}
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.out = append(s.out, v)
}

func toInterfaceSlice(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func toInterfaceMap(in map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
