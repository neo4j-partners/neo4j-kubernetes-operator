package main

import (
	"fmt"
	"os"
	"strings"
)

// watchScope is the resolved reconcile scope (BDR-003): either an explicit namespace list or
// opt-in cluster-wide. OperatorNS is excluded from a cluster-wide cache and skipped by the
// reconciler (NEO-016).
type watchScope struct {
	AllNamespaces bool
	Namespaces    []string
	OperatorNS    string
}

// resolveWatchScope reads WATCH_ALL_NAMESPACES first (opt-in cluster-wide, out-of-band so an
// unset/typo'd scope fails closed), otherwise falls back to the explicit WATCH_NAMESPACE list.
func resolveWatchScope() (watchScope, error) {
	podNS := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WATCH_ALL_NAMESPACES")), "true") {
		if strings.TrimSpace(os.Getenv("WATCH_NAMESPACE")) != "" {
			return watchScope{}, fmt.Errorf("WATCH_ALL_NAMESPACES=true is mutually exclusive with WATCH_NAMESPACE; leave WATCH_NAMESPACE empty for cluster-wide scope")
		}
		return watchScope{AllNamespaces: true, OperatorNS: podNS}, nil
	}
	ns, err := watchNamespaces()
	if err != nil {
		return watchScope{}, err
	}
	return watchScope{Namespaces: ns, OperatorNS: podNS}, nil
}

// watchNamespaces returns the configured watch list from WATCH_NAMESPACE.
// Comma-separated; empty or "*" is invalid. Cluster-wide is opt-in via WATCH_ALL_NAMESPACES.
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
