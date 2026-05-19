/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// Neo4jPodUID is the UID/GID Neo4j runs as inside the Enterprise image.
const Neo4jPodUID int64 = 7474

// DefaultNeo4jPodSecurityContext returns the hardened PodSecurityContext
// applied by default to every operator-managed pod that runs Neo4j or
// touches the Neo4j data volume (cluster servers, standalone, backup,
// restore, plugin jobs). Callers MAY override on a per-CR basis where
// the CRD exposes a spec.securityContext field; if unset, this default
// is enforced.
//
// Single source of truth — do not inline this elsewhere. The cluster,
// standalone, backup, restore, and plugin controllers all delegate
// here so that pod hardening cannot regress in one place while
// staying correct in another.
func DefaultNeo4jPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsUser:    ptr.To(Neo4jPodUID),
		RunAsGroup:   ptr.To(Neo4jPodUID),
		FSGroup:      ptr.To(Neo4jPodUID),
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// DefaultNeo4jContainerSecurityContext returns the hardened container
// SecurityContext for the same set of pods. Drops all Linux capabilities,
// disables privilege escalation, runs as the non-root Neo4j UID.
//
// ReadOnlyRootFilesystem is intentionally false: the Neo4j Docker
// entrypoint writes to several locations under the root filesystem
// (notably /var/lib/neo4j/conf for plugin config wiring, /tmp for
// startup scripts). A read-only root would break the entrypoint and
// the plugin/extension loaders. The data path itself is on a separate
// PV and is unaffected.
func DefaultNeo4jContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsUser:                ptr.To(Neo4jPodUID),
		RunAsGroup:               ptr.To(Neo4jPodUID),
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}
