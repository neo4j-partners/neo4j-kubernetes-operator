package trust

import "testing"

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
