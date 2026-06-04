/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

import (
	"context"
	goerrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestIsStrictPeerValidationEnabled(t *testing.T) {
	ptrBool := func(b bool) *bool { return &b }

	cases := []struct {
		name string
		spec *neo4jv1beta1.TLSSpec
		want bool
	}{
		{
			name: "no TLS spec → not strict",
			spec: nil,
			want: false,
		},
		{
			name: "tls.mode=disabled → not strict",
			spec: &neo4jv1beta1.TLSSpec{Mode: "disabled"},
			want: false,
		},
		{
			name: "cert-manager + field omitted → strict (default true)",
			spec: &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
			want: true,
		},
		{
			name: "cert-manager + explicit true → strict",
			spec: &neo4jv1beta1.TLSSpec{Mode: "cert-manager", StrictPeerValidation: ptrBool(true)},
			want: true,
		},
		{
			name: "cert-manager + explicit false → opt-out",
			spec: &neo4jv1beta1.TLSSpec{Mode: "cert-manager", StrictPeerValidation: ptrBool(false)},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{TLS: tc.spec},
			}
			assert.Equal(t, tc.want, isStrictPeerValidationEnabled(cluster))
		})
	}
}

func TestVerifyTLSSecretHasCA(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))

	mkCluster := func(name string) *neo4jv1beta1.Neo4jEnterpriseCluster {
		return &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				TLS: &neo4jv1beta1.TLSSpec{
					Mode:      "cert-manager",
					IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
				},
			},
		}
	}

	t.Run("Secret missing → errTLSSecretPending sentinel", func(t *testing.T) {
		// Without the Secret in the fake client, Get returns NotFound. The
		// preflight returns the sentinel error so the caller can update
		// status to Initializing AND block downstream STS emission.
		//
		// Returning nil (the prior behavior) would allow the reconciler to
		// proceed and emit a strict-mode STS that requires ca.crt in its
		// Secret volume projection — Pods would then be stuck in
		// CreateContainerConfigError if the Secret eventually appeared
		// without ca.crt.
		cluster := mkCluster("cluster-pending")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: c, Scheme: scheme}
		err := r.verifyTLSSecretHasCA(ctx, cluster)
		require.Error(t, err)
		assert.True(t, goerrors.Is(err, errTLSSecretPending),
			"expected errTLSSecretPending so caller distinguishes bootstrap from permanent failure; got %v", err)
	})

	t.Run("Secret present with ca.crt → no error", func(t *testing.T) {
		// Test fixture uses opaque non-PEM byte strings on purpose:
		// the function under test only checks for the presence and
		// non-emptiness of the ca.crt key, and shipping a fake PEM
		// header in the test would trip the gitleaks pre-commit hook.
		cluster := mkCluster("cluster-ok")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-ok-tls-secret", Namespace: "default"},
			Data: map[string][]byte{
				"tls.crt": []byte("leaf-cert-bytes"),
				"tls.key": []byte("leaf-key-bytes"),
				"ca.crt":  []byte("ca-cert-bytes"),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: c, Scheme: scheme}
		assert.NoError(t, r.verifyTLSSecretHasCA(ctx, cluster))
	})

	t.Run("Secret present but ca.crt missing → error names issuer", func(t *testing.T) {
		cluster := mkCluster("cluster-no-ca")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-no-ca-tls-secret", Namespace: "default"},
			Data: map[string][]byte{
				"tls.crt": []byte("leaf"),
				"tls.key": []byte("key"),
				// no ca.crt — mimics an external issuer that doesn't populate it.
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: c, Scheme: scheme}
		err := r.verifyTLSSecretHasCA(ctx, cluster)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ca.crt")
		assert.Contains(t, err.Error(), "ca-cluster-issuer", "error must name the offending issuer")
		assert.Contains(t, err.Error(), "strictPeerValidation=false", "error must point users at the opt-out")
	})

	t.Run("Secret present, ca.crt key but empty → error", func(t *testing.T) {
		cluster := mkCluster("cluster-empty-ca")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-empty-ca-tls-secret", Namespace: "default"},
			Data: map[string][]byte{
				"tls.crt": []byte("leaf"),
				"tls.key": []byte("key"),
				"ca.crt":  {}, // present but empty
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: c, Scheme: scheme}
		err := r.verifyTLSSecretHasCA(ctx, cluster)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-empty ca.crt")
	})
}
