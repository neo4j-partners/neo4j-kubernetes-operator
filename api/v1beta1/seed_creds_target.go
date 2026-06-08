/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1beta1

import corev1 "k8s.io/api/core/v1"

// Methods that make Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone
// implement the controller.SeedCredsTarget interface used by the shared
// EnsureSeedCredsProjected helper. The methods live here (not in the
// resource files) so the api package isn't forced to import the controller
// package and the interface stays defined where it's consumed.

// GetExtraEnvFrom returns the cluster CR's spec.extraEnvFrom slice.
func (c *Neo4jEnterpriseCluster) GetExtraEnvFrom() []corev1.EnvFromSource {
	return c.Spec.ExtraEnvFrom
}

// SetExtraEnvFrom replaces the cluster CR's spec.extraEnvFrom slice.
// In-place mutation is intentional — the caller wants to Update() the CR
// with the new value, not work on a copy.
func (c *Neo4jEnterpriseCluster) SetExtraEnvFrom(v []corev1.EnvFromSource) {
	c.Spec.ExtraEnvFrom = v
}

// TargetKindLabel is the human-readable label used in actionable errors
// from EnsureSeedCredsProjected so users see the right CR type to edit.
func (c *Neo4jEnterpriseCluster) TargetKindLabel() string { return "cluster" }

// GetExtraEnvFrom returns the standalone CR's spec.extraEnvFrom slice.
func (s *Neo4jEnterpriseStandalone) GetExtraEnvFrom() []corev1.EnvFromSource {
	return s.Spec.ExtraEnvFrom
}

// SetExtraEnvFrom replaces the standalone CR's spec.extraEnvFrom slice.
func (s *Neo4jEnterpriseStandalone) SetExtraEnvFrom(v []corev1.EnvFromSource) {
	s.Spec.ExtraEnvFrom = v
}

// TargetKindLabel is the human-readable label used in actionable errors
// from EnsureSeedCredsProjected.
func (s *Neo4jEnterpriseStandalone) TargetKindLabel() string { return "standalone" }
