package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// ChecksumAnnotation is stamped on the pod template so a rotated leaf certificate
// triggers a rolling restart. Leaf key/cert are subPath mounts, which Kubernetes
// never updates in place (BDR-006 known deviation).
const ChecksumAnnotation = "neo4j.com/tls-checksum"

// MountedSecretKeys is every Secret data key an active policy mounts — BYO and
// cert-manager alike.
func MountedSecretKeys(neo4j *neo4jv1beta1.Neo4j) []SecretKeyNeed {
	user, provisioned := collectSecretKeys(neo4j)
	out := make([]SecretKeyNeed, 0, len(user)+len(provisioned))
	return append(append(out, user...), provisioned...)
}

// ReferencesSecret reports whether an active TLS policy mounts data from name.
func ReferencesSecret(neo4j *neo4jv1beta1.Neo4j, name string) bool {
	if name == "" {
		return false
	}
	for _, k := range MountedSecretKeys(neo4j) {
		if k.SecretName == name {
			return true
		}
	}
	return false
}

// MaterialChecksum is a stable digest of the mounted TLS bytes. lookup may
// return nil for a missing key; that still hashes, so a later populate rolls.
func MaterialChecksum(keys []SecretKeyNeed, lookup func(secret, key string) []byte) string {
	if len(keys) == 0 {
		return ""
	}
	sorted := append([]SecretKeyNeed(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].SecretName != sorted[j].SecretName {
			return sorted[i].SecretName < sorted[j].SecretName
		}
		return sorted[i].Key < sorted[j].Key
	})
	h := sha256.New()
	for _, k := range sorted {
		h.Write([]byte(k.SecretName))
		h.Write([]byte{0})
		h.Write([]byte(k.Key))
		h.Write([]byte{0})
		h.Write(lookup(k.SecretName, k.Key))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
