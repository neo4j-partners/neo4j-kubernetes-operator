package formation

import (
	"context"
	"crypto/x509"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
)

// adminConnectOpts builds NEO-004 dial options: verified TLS when bolt material exists,
// else trust.insecureAdminConnection for explicit plaintext (Warning event).
func (r *Reconciler) adminConnectOpts(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) (intneo4j.ConnectOpts, error) {
	if BoltTLSEnabled(neo4j) {
		pool, err := loadBoltRootCAs(ctx, r.Client, neo4j)
		if err != nil {
			return intneo4j.ConnectOpts{}, err
		}
		return intneo4j.ConnectOpts{RootCAs: pool}, nil
	}
	if neo4j.Spec.Trust != nil && neo4j.Spec.Trust.InsecureAdminConnection {
		if r.Recorder != nil {
			r.Recorder.Event(neo4j, corev1.EventTypeWarning, "InsecureAdminConnection",
				"operator admin Bolt is unencrypted (trust.insecureAdminConnection=true); prefer trust.certificates.bolt")
		}
		return intneo4j.ConnectOpts{AllowPlaintext: true}, nil
	}
	msg := "admin Bolt requires trust.certificates.bolt (verified TLS) or trust.insecureAdminConnection=true (NEO-004)"
	if r.Recorder != nil {
		r.Recorder.Event(neo4j, corev1.EventTypeWarning, "AdminBoltTLSRequired", msg)
	}
	return intneo4j.ConnectOpts{}, fmt.Errorf("%s", msg)
}

func loadBoltRootCAs(ctx context.Context, c client.Client, neo4j *neo4jv1beta1.Neo4j) (*x509.CertPool, error) {
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

	// Self-signed / leaf-as-trust: use the public certificate when no CA pool was projected.
	if added == 0 && bolt.PublicCertificate != nil && bolt.PublicCertificate.SecretName != "" {
		var secret corev1.Secret
		key := types.NamespacedName{Name: bolt.PublicCertificate.SecretName, Namespace: neo4j.Namespace}
		if err := c.Get(ctx, key, &secret); err != nil {
			return nil, fmt.Errorf("trust.certificates.bolt.publicCertificate: %w", err)
		}
		certKey := "public.crt"
		if bolt.PublicCertificate.SubPath != "" {
			certKey = bolt.PublicCertificate.SubPath
		}
		pemBytes, ok := secret.Data[certKey]
		if !ok || len(pemBytes) == 0 {
			return nil, fmt.Errorf("trust.certificates.bolt.publicCertificate: secret %q missing key %q", secret.Name, certKey)
		}
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("trust.certificates.bolt.publicCertificate: no certificates parsed from %q", certKey)
		}
		added++
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
