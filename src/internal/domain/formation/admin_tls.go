package formation

import (
	"context"
	"crypto/x509"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// adminConnectOpts builds NEO-004 dial options: verified TLS when bolt material exists,
// else trust.insecureAdminConnection for explicit plaintext (Warning event).
func (r *Reconciler) adminConnectOpts(ctx context.Context, neo4j *neo4jv1.Neo4j) (intneo4j.ConnectOpts, error) {
	if BoltTLSEnabled(neo4j) {
		pool, err := loadBoltRootCAs(ctx, r.Client, neo4j)
		if err != nil {
			return intneo4j.ConnectOpts{}, err
		}
		return intneo4j.ConnectOpts{RootCAs: pool}, nil
	}
	// Both warnings restate the spec, and this runs on every pass that dials Neo4j, so they go
	// through the advisory memo: repeated verbatim they would drain the object's Event budget.
	if neo4j.Spec.Trust != nil && neo4j.Spec.Trust.InsecureAdminConnection {
		r.advisories.Emit(r.Recorder, neo4j, corev1.EventTypeWarning, oracle.ReasonInsecureAdminConnection,
			"operator admin Bolt is unencrypted (trust.insecureAdminConnection=true); prefer trust.certificates.bolt")
		return intneo4j.ConnectOpts{AllowPlaintext: true}, nil
	}
	msg := "admin Bolt requires trust.certificates.bolt (verified TLS) or trust.insecureAdminConnection=true (NEO-004)"
	r.advisories.Emit(r.Recorder, neo4j, corev1.EventTypeWarning, oracle.ReasonAdminBoltTLSRequired, msg)
	return intneo4j.ConnectOpts{}, fmt.Errorf("%s", msg)
}

func loadBoltRootCAs(ctx context.Context, c client.Client, neo4j *neo4jv1.Neo4j) (*x509.CertPool, error) {
	bolt := neo4j.Spec.Trust.Certificates.Bolt
	pool := x509.NewCertPool()
	added := 0

	if bolt.TrustedCerts != nil {
		for i, src := range bolt.TrustedCerts.Sources {
			if src.Secret == nil || src.Secret.Name == "" {
				continue
			}
			var secret corev1.Secret
			key := types.NamespacedName{Name: src.Secret.Name, Namespace: neo4j.Namespace}
			if err := c.Get(ctx, key, &secret); err != nil {
				return nil, fmt.Errorf("trust.certificates.bolt.trustedCerts.sources[%d]: %w", i, err)
			}
			n, err := appendSecretPEMs(pool, &secret, src.Secret.Items)
			if err != nil {
				return nil, fmt.Errorf("trust.certificates.bolt.trustedCerts.sources[%d]: %w", i, err)
			}
			added += n
		}
	}

	// No explicit trustedCerts bundle projected: trust the policy's own anchor, resolved the
	// same way a client does (rendertrust.TrustAnchorKeys) — under cert-manager that is the
	// issuing CA (ca.crt), not the leaf tls.crt; for BYO it is the self-signed public cert.
	// Trusting the cert-manager leaf here was a bug: cert-manager reissues the leaf on renewal
	// or a SAN change (pool scale/teardown), the pods keep serving the previous leaf until they
	// restart, and the dial then failed with "certificate signed by unknown authority". The CA
	// survives every reissue, so this is also what the servers' clients trust.
	if added == 0 {
		for _, need := range rendertrust.TrustAnchorKeys(neo4j, "bolt") {
			var secret corev1.Secret
			key := types.NamespacedName{Name: need.SecretName, Namespace: neo4j.Namespace}
			if err := c.Get(ctx, key, &secret); err != nil {
				return nil, fmt.Errorf("trust.certificates.bolt trust anchor %q: %w", need.Key, err)
			}
			pemBytes, ok := secret.Data[need.Key]
			if !ok || len(pemBytes) == 0 {
				return nil, fmt.Errorf("trust.certificates.bolt trust anchor: secret %q missing key %q", need.SecretName, need.Key)
			}
			if !pool.AppendCertsFromPEM(pemBytes) {
				return nil, fmt.Errorf("trust.certificates.bolt trust anchor: no certificates parsed from %q", need.Key)
			}
			added++
		}
	}

	if added == 0 {
		return nil, fmt.Errorf("bolt TLS enabled but no trusted CA material for operator dial — set trustedCerts.sources or a parseable publicCertificate (NEO-004)")
	}
	return pool, nil
}

func appendSecretPEMs(pool *x509.CertPool, secret *corev1.Secret, items []corev1.KeyToPath) (int, error) {
	added := 0
	if len(items) == 0 {
		for k, v := range secret.Data {
			if len(v) == 0 {
				continue
			}
			if pool.AppendCertsFromPEM(v) {
				added++
			} else {
				return 0, fmt.Errorf("secret %q key %q: no PEM certificates", secret.Name, k)
			}
		}
		return added, nil
	}
	for _, item := range items {
		v, ok := secret.Data[item.Key]
		if !ok || len(v) == 0 {
			return 0, fmt.Errorf("secret %q missing key %q", secret.Name, item.Key)
		}
		if !pool.AppendCertsFromPEM(v) {
			return 0, fmt.Errorf("secret %q key %q: no PEM certificates", secret.Name, item.Key)
		}
		added++
	}
	return added, nil
}
