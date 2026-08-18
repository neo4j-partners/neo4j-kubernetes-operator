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
	if err := rejectWatchingOperatorNamespace(out, strings.TrimSpace(os.Getenv("POD_NAMESPACE"))); err != nil {
		return nil, err
	}
	return out, nil
}

// rejectWatchingOperatorNamespace forbids reconciling Neo4j CRs in the operator
// install namespace (NEO-016). A CR there can adopt the operator ServiceAccount.
// POD_NAMESPACE empty (local make run) skips the check.
func rejectWatchingOperatorNamespace(watched []string, podNS string) error {
	if podNS == "" {
		return nil
	}
	for _, ns := range watched {
		if ns == podNS {
			return fmt.Errorf("WATCH_NAMESPACE includes the operator namespace %q; install the operator in a dedicated namespace and watch workload namespaces only (NEO-016)", podNS)
		}
	}
	return nil
}
