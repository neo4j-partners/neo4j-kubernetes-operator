package imagepolicy

import (
	"fmt"
	"strings"
	"sync"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// DefaultAllowedRepositories is used when the operator flag/env is unset (NEO-012).
var DefaultAllowedRepositories = []string{
	"neo4j",
	"docker.io/neo4j",
	"index.docker.io/neo4j",
}

var (
	mu       sync.RWMutex
	allowed  = append([]string(nil), DefaultAllowedRepositories...)
	allowAll bool
)

// SetAllowedRepositories configures the registry allowlist from a comma-separated
// list. Empty keeps defaults. A single "*" allows any repository (lab only).
func SetAllowedRepositories(csv string) {
	mu.Lock()
	defer mu.Unlock()
	raw := strings.TrimSpace(csv)
	if raw == "" {
		allowed = append([]string(nil), DefaultAllowedRepositories...)
		allowAll = false
		return
	}
	if raw == "*" {
		allowed = nil
		allowAll = true
		return
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.TrimSuffix(p, "/"))
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		allowed = append([]string(nil), DefaultAllowedRepositories...)
		allowAll = false
		return
	}
	allowed = out
	allowAll = false
}

// AllowedRepositories returns a copy of the current allowlist (nil if allow-all).
func AllowedRepositories() []string {
	mu.RLock()
	defer mu.RUnlock()
	if allowAll {
		return nil
	}
	return append([]string(nil), allowed...)
}

// Validate checks spec.image.repository against the operator allowlist and digest shape (NEO-012).
func Validate(neo4j *neo4jv1beta1.Neo4j) error {
	repo := "neo4j"
	var digest string
	if neo4j.Spec.Image != nil {
		if neo4j.Spec.Image.Repository != "" {
			repo = neo4j.Spec.Image.Repository
		}
		digest = neo4j.Spec.Image.Digest
	}
	if err := validateDigest(digest); err != nil {
		return err
	}
	if !repositoryAllowed(repo) {
		return fmt.Errorf("spec.image.repository %q is not in the operator allowlist %v (NEO-012); set --allowed-image-repositories or ALLOWED_IMAGE_REPOSITORIES", repo, AllowedRepositories())
	}
	return nil
}

func validateDigest(digest string) error {
	if digest == "" {
		return nil
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(digest, prefix) {
		return fmt.Errorf("spec.image.digest must start with sha256:")
	}
	hex := digest[len(prefix):]
	if len(hex) != 64 {
		return fmt.Errorf("spec.image.digest must be sha256: followed by 64 hex characters")
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return fmt.Errorf("spec.image.digest must be sha256: followed by 64 hex characters")
		}
	}
	return nil
}

func repositoryAllowed(repo string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if allowAll {
		return true
	}
	repo = strings.TrimSuffix(repo, "/")
	for _, prefix := range allowed {
		if repo == prefix || strings.HasPrefix(repo, prefix+"/") {
			return true
		}
	}
	return false
}
