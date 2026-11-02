// Package platform records the cluster platform the operator targets so operand pod
// security contexts can adapt (NEO-026). The single non-portable decision is whether to
// pin the Neo4j container UID/GID/fsGroup to 7474:
//
//   - OpenShift rejects a fixed runAsUser/fsGroup under the default restricted-v2 SCC
//     (they must fall inside the namespace's allocated range) but injects valid ones when
//     left unset. So on OpenShift the operand must omit them.
//   - Plain Kubernetes does not inject an fsGroup, so a non-root process could not write a
//     freshly provisioned PVC. There the operand pins 7474.
//
// The mode is resolved once at operator startup (explicit flag/env, else autodetect) and
// read during render. Mirrors the set-once imagepolicy allowlist.
package platform

import (
	"strings"
	"sync"
)

// Mode is the resolved (or requested) target platform.
type Mode string

const (
	// Auto requests autodetection at startup; it is never stored as the resolved mode.
	Auto Mode = "auto"
	// OpenShift omits the fixed UID/GID/fsGroup so the SCC injects an in-range UID.
	OpenShift Mode = "openshift"
	// Kubernetes pins 7474 so non-root pods can write freshly provisioned PVCs.
	Kubernetes Mode = "kubernetes"
)

var (
	mu sync.RWMutex
	// Default to Kubernetes: the safe, backward-compatible behavior when Set is never
	// called (e.g. unit tests). The operator always calls Set after resolving at startup.
	current = Kubernetes
)

// Set records the resolved platform. Anything other than OpenShift falls back to Kubernetes
// so an unexpected value never silently drops the fsGroup pin that plain K8s needs.
func Set(m Mode) {
	mu.Lock()
	defer mu.Unlock()
	if m == OpenShift {
		current = OpenShift
	} else {
		current = Kubernetes
	}
}

// Current returns the resolved platform.
func Current() Mode {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// IsOpenShift reports whether operand pods must omit the fixed UID/GID/fsGroup.
func IsOpenShift() bool {
	return Current() == OpenShift
}

// Resolve picks the platform from the explicit flag/env value, falling back to the
// autodetected result when the value is empty or "auto" (anything unrecognized is treated
// as auto). Detection itself is the caller's job (it needs a discovery client); this keeps
// the decision pure and testable.
func Resolve(explicit string, detectedOpenShift bool) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(explicit))) {
	case OpenShift:
		return OpenShift
	case Kubernetes:
		return Kubernetes
	default: // "", "auto", or anything unrecognized → autodetect
		if detectedOpenShift {
			return OpenShift
		}
		return Kubernetes
	}
}
