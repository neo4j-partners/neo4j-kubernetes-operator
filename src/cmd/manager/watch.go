package main

import (
	"fmt"
	"os"
	"strings"
)

// watchNamespaces returns the configured watch list from WATCH_NAMESPACE.
// Comma-separated; empty or "*" is invalid (cluster-wide is not supported with Role RBAC).
func watchNamespaces() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("WATCH_NAMESPACE"))
	if raw == "" {
		return nil, fmt.Errorf("WATCH_NAMESPACE is required (comma-separated namespace list)")
	}
	if raw == "*" {
		return nil, fmt.Errorf("WATCH_NAMESPACE=* (cluster-wide) is not supported; use an explicit namespace list")
	}
	var out []string
	seen := map[string]struct{}{}
	for _, p := range strings.Split(raw, ",") {
		ns := strings.TrimSpace(p)
		if ns == "" {
			continue
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		out = append(out, ns)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("WATCH_NAMESPACE has no namespaces")
	}
	return out, nil
}
