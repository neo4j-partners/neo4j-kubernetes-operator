package trust

import (
	"testing"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
)

// With trust.reload the leaf cert+key are live-directory mounts Neo4j hot-reloads, so a reissue
// (new leaf bytes on scale/renewal) must not move the restart checksum — that is what stops the
// pods rolling. A subPath-mounted trustedCerts CA cannot hot-update, so it stays restart-critical.
func TestRestartCriticalSecretKeysReloadExcludesLeaf(t *testing.T) {
	n := clusterWithCertManager()
	n.Spec.Trust.Reload = &neo4jv1.TrustReloadSpec{Enabled: true}

	on := RestartCriticalSecretKeys(n)
	for _, k := range on {
		if k.Key == certManagerCertKey || k.Key == certManagerKeyKey {
			t.Fatalf("reload on: hot-reloaded leaf/key %q must be excluded from the restart checksum, got %#v", k.Key, on)
		}
	}
	foundCA := false
	for _, k := range on {
		if k.Key == certManagerCAKey {
			foundCA = true
		}
	}
	if !foundCA {
		t.Fatalf("reload on: subPath trustedCerts ca.crt must stay restart-critical, got %#v", on)
	}

	n.Spec.Trust.Reload = nil
	if off := RestartCriticalSecretKeys(n); len(off) <= len(on) {
		t.Fatalf("reload off must add the leaf+key back: off=%#v on=%#v", off, on)
	}
}

func TestMaterialChecksumChangesWithCertBytes(t *testing.T) {
	neo4j := standaloneWithBoltTrust()
	keys := MountedSecretKeys(neo4j)
	if len(keys) != 2 {
		t.Fatalf("keys = %#v, want bolt key+cert", keys)
	}

	data := map[string][]byte{
		"dev-bolt-key\x00private.key": []byte("old-key"),
		"dev-bolt-cert\x00public.crt": []byte("old-cert"),
	}
	lookup := func(secret, key string) []byte {
		return data[secret+"\x00"+key]
	}
	before := MaterialChecksum(keys, lookup)
	if before == "" {
		t.Fatal("checksum must not be empty when keys are mounted")
	}

	data["dev-bolt-cert\x00public.crt"] = []byte("new-cert")
	after := MaterialChecksum(keys, lookup)
	if after == before {
		t.Fatal("checksum must change when the certificate bytes change")
	}

	if !ReferencesSecret(neo4j, "dev-bolt-cert") || ReferencesSecret(neo4j, "unrelated") {
		t.Fatal("ReferencesSecret")
	}
}

func TestMaterialChecksumEmptyWithoutKeys(t *testing.T) {
	if got := MaterialChecksum(nil, func(string, string) []byte { return nil }); got != "" {
		t.Fatalf("checksum = %q, want empty", got)
	}
}

func TestMountedSecretKeysCertManager(t *testing.T) {
	neo4j := clusterWithCertManager()
	keys := MountedSecretKeys(neo4j)
	got := map[string]string{}
	for _, k := range keys {
		got[k.SecretName] = k.Key
	}
	if got["prod-bolt-tls-secret"] != "tls.crt" && got["prod-bolt-tls-secret"] != "tls.key" {
		t.Fatalf("cert-manager bolt keys missing: %#v", keys)
	}
	if !ReferencesSecret(neo4j, "prod-bolt-tls-secret") {
		t.Fatal("cert-manager target Secret must enqueue a reconcile")
	}
}
