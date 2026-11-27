package formation

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/shared"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// readableClientPorts are the server-auth client ports whose served leaf the operator can read
// with a plain TLS dial to confirm a member adopted the reissued cert. The cluster policy is mTLS
// on the internal ports (6000/7000/7688) and rides along on the same reloadTLS() call, verified by
// proxy — bolt/https reissue in lockstep with it, because every leaf policy enumerates the same
// per-member SANs (render/trust certificate.go), so any scale or renewal moves all of them.
var readableClientPorts = map[string]int{"bolt": 7687, "https": 7473}

// TLSReloadReconciler keeps the running members serving the current leaf cert when trust.reload is
// on. The leaf is a whole-Secret directory mount kubelet refreshes in place (render/trust), so a
// reissue does not roll the pods — but Neo4j only re-reads it when told to (dbms.security.reloadTLS,
// 2025.03+). This step detects a leaf change, calls reloadTLS() on each member over a direct Bolt
// connection, and verifies convergence by reading the served leaf back, so a member whose kubelet
// has not propagated yet is simply retried rather than left silently stale (BDR-007).
type TLSReloadReconciler struct {
	Client   client.Client
	Recorder record.EventRecorder
	// Connect dials one member's Bolt directly; nil → the real verified-TLS driver. Test seam.
	Connect func(ctx context.Context, neo4j *neo4jv1.Neo4j, boltURI string) (intneo4j.Admin, error)
	// ServedLeaf returns the hex SHA-256 of the leaf a member serves at addr; nil → a real TLS
	// dial. Test seam.
	ServedLeaf func(ctx context.Context, addr string) (string, error)
}

func (r *TLSReloadReconciler) Reconcile(ctx context.Context, neo4j *neo4jv1.Neo4j) shared.StepResult {
	log := ctrllog.FromContext(ctx)
	if !rendertrust.ReloadEnabled(neo4j) {
		return shared.Done()
	}
	keys := rendertrust.HotLeafKeys(neo4j)
	if len(keys) == 0 {
		return shared.Done()
	}
	lookup, err := r.loadSecrets(ctx, neo4j, keys)
	if err != nil {
		// The trust domain already validates Secret presence and fails the pass when one is
		// missing; by here they exist, so a Get error is transient. Nothing to reload yet.
		log.V(1).Info("tls reload: leaf Secrets unavailable, skip", "err", err.Error())
		return shared.Done()
	}
	want := rendertrust.MaterialChecksum(keys, lookup)
	if want == neo4j.Status.TLSReloadChecksum {
		return shared.Done() // every member already serves this material
	}

	members := DesiredMembers(neo4j)
	if len(members) == 0 {
		return shared.Done()
	}

	// Desired served fingerprint per server-auth client policy we can verify.
	desired := map[string]string{}
	for _, m := range rendertrust.ActiveLeafMaterials(neo4j) {
		if _, ok := readableClientPorts[m.Policy]; !ok || !clientAuthReadable(neo4j, m.Policy) {
			continue
		}
		fp, perr := leafFingerprintFromPEM(lookup(m.CertSecret, m.CertKey))
		if perr != nil {
			log.Info("tls reload: cannot parse desired leaf, skip", "policy", m.Policy, "err", perr.Error())
			return shared.Done()
		}
		desired[m.Policy] = fp
	}

	served := r.ServedLeaf
	if served == nil {
		served = readServedFingerprint
	}

	if len(desired) == 0 {
		// No server-auth client port to verify against (e.g. cluster-only, or bolt/https are
		// mTLS). ponytail: reload every member once per material change and trust it — a member
		// whose kubelet had not propagated reloads stale and is not retried until the material
		// changes again. Configure a bolt/https policy (the normal case) for verified reload.
		for _, m := range members {
			if err := r.reloadMember(ctx, neo4j, boltHost(m)); err != nil {
				log.Info("tls reload (unverified): reloadTLS failed, requeue", "member", m.PodName, "err", err.Error())
				return shared.Requeue(requeueAfter)
			}
		}
		neo4j.Status.TLSReloadChecksum = want
		log.Info("tls reload: reloaded all members (unverified)", "checksum", short(want))
		return shared.Done()
	}

	converged := true
	for _, m := range members {
		host := boltHost(m)
		// A member is converged when every port we could actually read serves the new leaf. A read
		// *error* (a secondary pool does not expose https on its per-member Service, a member is
		// briefly down) is skipped, not treated as stale — otherwise an unreachable port wedges the
		// loop forever. A successful read that mismatches still counts as stale, so a real lag is
		// never masked. readable==0 (nothing answered) keeps the member unconverged so we retry.
		readable, matched := 0, 0
		for policy, fp := range desired {
			addr := fmt.Sprintf("%s:%d", host, readableClientPorts[policy])
			got, serr := served(ctx, addr)
			if serr != nil {
				log.V(1).Info("tls reload: served read failed, skipping port", "member", m.PodName, "policy", policy, "err", serr.Error())
				continue
			}
			readable++
			if got == fp {
				matched++
			}
		}
		if readable > 0 && matched == readable {
			continue
		}
		converged = false
		if err := r.reloadMember(ctx, neo4j, host); err != nil {
			log.Info("tls reload: reloadTLS failed", "member", m.PodName, "err", err.Error())
			continue
		}
		log.Info("tls reload: triggered reloadTLS", "member", m.PodName)
	}

	if !converged {
		return shared.Requeue(requeueAfter)
	}
	neo4j.Status.TLSReloadChecksum = want
	log.Info("tls reload: all members serve current leaf", "checksum", short(want))
	return shared.Done()
}

// reloadMember opens a direct Bolt session to one member and runs reloadTLS(). recorder is nil:
// the dial's NEO-004 advisory is already emitted once per generation by the formation Reconciler,
// so repeating it on this per-member hot path would spend the object's Event budget.
func (r *TLSReloadReconciler) reloadMember(ctx context.Context, neo4j *neo4jv1.Neo4j, host string) error {
	connect := r.Connect
	if connect == nil {
		connect = func(ctx context.Context, n *neo4jv1.Neo4j, uri string) (intneo4j.Admin, error) {
			return dialAt(ctx, r.Client, nil, n, uri)
		}
	}
	admin, err := connect(ctx, neo4j, "bolt://"+host+":7687")
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(ctx) }()
	return admin.ReloadTLS(ctx)
}

func (r *TLSReloadReconciler) loadSecrets(ctx context.Context, neo4j *neo4jv1.Neo4j, keys []rendertrust.SecretKeyNeed) (func(secret, key string) []byte, error) {
	cache := map[string]*corev1.Secret{}
	for _, k := range keys {
		if _, ok := cache[k.SecretName]; ok {
			continue
		}
		var s corev1.Secret
		if err := r.Client.Get(ctx, types.NamespacedName{Name: k.SecretName, Namespace: neo4j.Namespace}, &s); err != nil {
			return nil, err
		}
		cache[k.SecretName] = &s
	}
	return func(secret, key string) []byte {
		if s, ok := cache[secret]; ok {
			return s.Data[key]
		}
		return nil
	}, nil
}

// clientAuthReadable is true when a plain (no client cert) TLS dial can read the policy's served
// leaf — i.e. the policy does not demand mutual auth. clientAuth=Require aborts the handshake
// before we could compare, so such a policy is treated like the cluster policy (reload-by-proxy).
func clientAuthReadable(neo4j *neo4jv1.Neo4j, policy string) bool {
	certs := neo4j.Spec.Trust.Certificates
	if certs == nil {
		return false
	}
	var p *neo4jv1.TLSPolicySpec
	switch policy {
	case "bolt":
		p = certs.Bolt
	case "https":
		p = certs.HTTPS
	}
	if p == nil {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(string(p.ClientAuth)), "Require")
}

func boltHost(m Member) string { return strings.TrimSuffix(m.BoltAddress, ":7687") }

func short(checksum string) string {
	if len(checksum) > 12 {
		return checksum[:12]
	}
	return checksum
}

// leafFingerprintFromPEM returns the hex SHA-256 of the first certificate in a PEM bundle — the
// leaf, which is what a TLS peer presents first and what reloadTLS makes the member serve.
func leafFingerprintFromPEM(pemBytes []byte) (string, error) {
	rest := pemBytes
	for {
		block, more := pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return "", err
			}
			return certFingerprint(cert), nil
		}
		rest = more
	}
	return "", fmt.Errorf("no CERTIFICATE block in leaf PEM")
}

func certFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// readServedFingerprint dials addr, captures the server's leaf during the handshake, and returns
// its fingerprint. InsecureSkipVerify + the VerifyPeerCertificate capture are deliberate: this is
// not a trust decision (we only compare the fingerprint to our own Secret), and capturing in the
// callback yields the leaf even for an mTLS port that aborts the handshake for the missing client
// cert.
func readServedFingerprint(ctx context.Context, addr string) (string, error) {
	var leaf *x509.Certificate
	cfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // fingerprint read only; compared against our own Secret, not trusted
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) > 0 {
				leaf, _ = x509.ParseCertificate(raw[0])
			}
			return nil
		},
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: cfg}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if conn != nil {
		_ = conn.Close()
	}
	if leaf != nil {
		return certFingerprint(leaf), nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("no server certificate from %s", addr)
}
