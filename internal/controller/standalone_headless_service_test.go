/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Contract tests for the standalone headless Service ({name}-headless).
// Added with #184: the standalone headless service historically exposed only
// the backup port (6362), unlike the cluster headless service which also
// exposes bolt/http for direct pod addressing. These tests pin the parity
// additions (bolt/http, +https under TLS) AND the deliberate omission of the
// cluster-only clustering ports (a standalone is single-node, nothing listens
// on RAFT/discovery/tx), so neither regresses.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

func headlessPortNames(svc *corev1.Service) map[string]int32 {
	out := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		out[p.Name] = p.Port
	}
	return out
}

func TestStandaloneHeadlessService_ExposesClientPortsForParity(t *testing.T) {
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	r := &Neo4jEnterpriseStandaloneReconciler{}
	svc := r.createHeadlessService(standalone)
	require.NotNil(t, svc)

	assert.Equal(t, "my-standalone-headless", svc.Name)
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP, "headless service must have ClusterIP=None")
	assert.True(t, svc.Spec.PublishNotReadyAddresses, "headless service must publish not-ready addresses for startup reachability")
	assert.Equal(t, standalone.Name, svc.Spec.Selector["app"], "headless selector must match the standalone pod label")

	ports := headlessPortNames(svc)

	// Parity with the cluster headless service: bolt + http for direct pod
	// addressing, plus the backup port.
	assert.Equal(t, int32(resources.BoltPort), ports["bolt"], "bolt port must be exposed (#184 parity)")
	assert.Equal(t, int32(resources.HTTPPort), ports["http"], "http port must be exposed (#184 parity)")
	assert.Equal(t, int32(resources.BackupPort), ports["backup"], "backup port must remain exposed")

	// Without TLS, https must NOT be present.
	_, hasHTTPS := ports["https"]
	assert.False(t, hasHTTPS, "https must not be exposed when TLS is disabled")

	// Cluster-only clustering ports must NOT appear on a standalone: the single
	// node runs no RAFT/discovery, so nothing listens on them.
	for _, p := range []int32{resources.DiscoveryPort, resources.RoutingPort, resources.RaftPort, resources.TransactionPort} {
		for name, port := range ports {
			assert.NotEqual(t, p, port, "clustering port %d (%q) must not be exposed on a standalone headless service", p, name)
		}
	}
}

func TestStandaloneHeadlessService_ExposesHTTPSUnderTLS(t *testing.T) {
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-standalone", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Storage: neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
			TLS:     &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
		},
	}

	r := &Neo4jEnterpriseStandaloneReconciler{}
	svc := r.createHeadlessService(standalone)
	require.NotNil(t, svc)

	ports := headlessPortNames(svc)
	assert.Equal(t, int32(resources.HTTPSPort), ports["https"], "https must be exposed under cert-manager TLS, mirroring the client service")
	// bolt/http/backup still present alongside https.
	assert.Equal(t, int32(resources.BoltPort), ports["bolt"])
	assert.Equal(t, int32(resources.HTTPPort), ports["http"])
	assert.Equal(t, int32(resources.BackupPort), ports["backup"])
}
