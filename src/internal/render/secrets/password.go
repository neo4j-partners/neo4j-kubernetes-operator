package secrets

import "crypto/rand"

// passwordAlphabet is alphanumeric on purpose: the value is used both as a Neo4j password (fed to
// neo4j-admin as a positional arg, so a leading "-" would be parsed as an option) and inside
// NEO4J_AUTH (parsed on "/"). Staying in [A-Za-z0-9] sidesteps both. 24 symbols ≈ 143 bits of entropy.
// ponytail: domain/workload has an equivalent private generator; this is the shared, exported one for
// new callers (Studio asset-store). The upgrade path is pointing workload at this too.
const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// RandomPassword returns a cryptographically-random alphanumeric string of length n.
func RandomPassword(n int) (string, error) {
	// Bytes at or above the largest whole multiple of the alphabet are redrawn, otherwise the leading
	// symbols would be slightly more likely than the rest.
	limit := byte(256 - 256%len(passwordAlphabet))
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out = append(out, passwordAlphabet[int(b)%len(passwordAlphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
