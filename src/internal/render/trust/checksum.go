package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
)

// ChecksumAnnotation is stamped on the pod template so a change to TLS material that a running
// Neo4j cannot adopt on its own forces a rolling restart. With trust.reload the leaf cert+key are
// whole-Secret directory mounts kubelet refreshes in place and Neo4j hot-reloads, so they are
// excluded from the sum (RestartCriticalSecretKeys) and a reissue — on scale or renewal — no
// longer rolls the pods. trustedCerts bundles stay subPath-mounted (Kubernetes never updates those
// in place), so a CA change still rolls; and without trust.reload everything still rolls as before.
const ChecksumAnnotation = "neo4j.com/tls-checksum"

// MountedSecretKeys is every Secret data key an active policy mounts — BYO and
// cert-manager alike.
func MountedSecretKeys(neo4j *neo4jv1.Neo4j) []SecretKeyNeed {
	user, provisioned := collectSecretKeys(neo4j)
	out := make([]SecretKeyNeed, 0, len(user)+len(provisioned))
	return append(append(out, user...), provisioned...)
}

// RestartCriticalSecretKeys is the subset of mounted TLS keys whose change requires a pod restart
// to take effect — the input to the tls-checksum roll. Without trust.reload that is every mounted
// key. With trust.reload the leaf cert+key are live-directory mounts Neo4j reloads in place, so
// they drop out and only the subPath-mounted trustedCerts bundles remain (a CA change still rolls).
// When nothing restart-critical is mounted it returns nil, so the checksum is empty and no roll is
// stamped at all.
func RestartCriticalSecretKeys(neo4j *neo4jv1.Neo4j) []SecretKeyNeed {
	all := MountedSecretKeys(neo4j)
	if !ReloadEnabled(neo4j) {
		return all
	}
	hot := map[string]struct{}{}
	for _, def := range tlsPolicies {
		_, mat, ok := policyActive(neo4j, def)
		if !ok {
			continue
		}
		hot[mat.CertSecret+"\x00"+mat.CertPath] = struct{}{}
		hot[mat.KeySecret+"\x00"+mat.KeyPath] = struct{}{}
	}
	out := make([]SecretKeyNeed, 0, len(all))
	for _, k := range all {
		if _, h := hot[k.SecretName+"\x00"+k.Key]; h {
			continue
		}
		out = append(out, k)
	}
	return out
}

// ReferencesSecret reports whether an active TLS policy mounts data from name.
func ReferencesSecret(neo4j *neo4jv1.Neo4j, name string) bool {
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
