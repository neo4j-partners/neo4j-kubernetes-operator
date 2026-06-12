/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Pinning tests for the #215 client-Service naming unification: the canonical
// {name}-client carries the full spec.service configuration; the deprecated
// {name}-service alias survives one release as a ClusterIP DNS shim; TLS
// certs carry dual SANs; and the one-time TLS roll is gated on the re-issued
// cert actually containing the canonical SAN.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func tlsStandalone(name string) *neo4jv1beta1.Neo4jEnterpriseStandalone {
	return &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Storage: neo4jv1beta1.StorageSpec{Size: "2Gi"},
			Service: &neo4jv1beta1.ServiceSpec{Type: "LoadBalancer"},
			TLS: &neo4jv1beta1.TLSSpec{
				Mode:      "cert-manager",
				IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
			},
		},
	}
}

func TestStandaloneCanonicalAndAliasServices(t *testing.T) {
	r := &Neo4jEnterpriseStandaloneReconciler{}
	sa := tlsStandalone("db1")

	canonical := r.createService(sa)
	assert.Equal(t, "db1-client", canonical.Name,
		"the canonical client Service uses the cluster-wide {name}-client convention (#215)")
	assert.Equal(t, corev1.ServiceTypeLoadBalancer, canonical.Spec.Type,
		"spec.service (external exposure) lives on the canonical Service")

	alias := r.createAliasService(sa)
	assert.Equal(t, "db1-service", alias.Name)
	assert.Equal(t, corev1.ServiceTypeClusterIP, alias.Spec.Type,
		"the deprecated alias is a ClusterIP-only DNS shim — never a second LB")
	assert.Equal(t, "db1-client", alias.Annotations["neo4j.com/deprecated-alias-of"])
	portNames := map[string]bool{}
	for _, p := range alias.Spec.Ports {
		portNames[p.Name] = true
	}
	assert.True(t, portNames["bolt"] && portNames["http"] && portNames["https"],
		"alias keeps the client ports (incl. https under TLS) so saved connection strings work")
}

func TestStandaloneCertCarriesDualSANs(t *testing.T) {
	r := &Neo4jEnterpriseStandaloneReconciler{}
	cert := r.createTLSCertificate(tlsStandalone("db1"))
	want := []string{
		"db1-client", "db1-client.ns", "db1-client.ns.svc", "db1-client.ns.svc.cluster.local",
		"db1-service", "db1-service.ns", "db1-service.ns.svc", "db1-service.ns.svc.cluster.local",
	}
	for _, w := range want {
		assert.Contains(t, cert.Spec.DNSNames, w)
	}
}

// selfSignedCertPEM builds a throwaway cert with the given SANs.
func selfSignedCertPEM(t *testing.T, dnsNames []string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestTLSCertHasClientSAN_GatesTheRoll(t *testing.T) {
	scheme := newTestScheme()
	sa := tlsStandalone("db1")

	oldSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db1-tls-secret", Namespace: "ns"},
		Data:       map[string][]byte{"tls.crt": selfSignedCertPEM(t, []string{"db1-service", "db1-service.ns.svc.cluster.local"})},
	}
	r := &Neo4jEnterpriseStandaloneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa, oldSecret).Build()}
	assert.False(t, r.tlsCertHasClientSAN(context.Background(), sa),
		"pre-rename cert (no -client SAN) must NOT trigger the roll — restarting would reload the old cert")
	sts := r.createStatefulSet(context.Background(), sa)
	_, stamped := sts.Spec.Template.Annotations["neo4j.com/tls-cert-sans"]
	assert.False(t, stamped)

	newSecret := oldSecret.DeepCopy()
	newSecret.Data["tls.crt"] = selfSignedCertPEM(t, []string{"db1-client", "db1-client.ns.svc.cluster.local", "db1-service"})
	r2 := &Neo4jEnterpriseStandaloneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa, newSecret).Build()}
	assert.True(t, r2.tlsCertHasClientSAN(context.Background(), sa))
	sts2 := r2.createStatefulSet(context.Background(), sa)
	assert.Equal(t, "client-v1", sts2.Spec.Template.Annotations["neo4j.com/tls-cert-sans"],
		"once the re-issued cert carries the canonical SAN, the template rolls the pod exactly once")

	// Missing secret (TLS not yet issued) — no roll, no panic.
	r3 := &Neo4jEnterpriseStandaloneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()}
	assert.False(t, r3.tlsCertHasClientSAN(context.Background(), sa))
}
