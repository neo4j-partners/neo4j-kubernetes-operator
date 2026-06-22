/*
Copyright 2025 Priyo Lahiri.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package neo4j

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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// generateSelfSignedCAPEM creates a self-signed CA certificate PEM for testing.
func generateSelfSignedCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestBuildTLSConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	caPEM := generateSelfSignedCAPEM(t)

	tests := []struct {
		name             string
		tlsSpec          *neo4jv1beta1.TLSSpec
		resourceName     string
		namespace        string
		secrets          []corev1.Secret
		wantNil          bool
		wantInsecureSkip bool
		wantRootCAs      bool
	}{
		{
			name:    "nil TLS spec returns nil",
			tlsSpec: nil,
			wantNil: true,
		},
		{
			name:    "disabled mode returns nil",
			tlsSpec: &neo4jv1beta1.TLSSpec{Mode: "disabled"},
			wantNil: true,
		},
		{
			name:             "cert-manager with no secrets falls back to insecure",
			tlsSpec:          &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
			resourceName:     "my-cluster",
			namespace:        "default",
			secrets:          []corev1.Secret{},
			wantInsecureSkip: true,
		},
		{
			name:         "auto-discovers CA from cert-manager secret",
			tlsSpec:      &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
			resourceName: "my-cluster",
			namespace:    "default",
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-tls-secret", Namespace: "default"},
					Data:       map[string][]byte{"ca.crt": caPEM, "tls.crt": []byte("cert"), "tls.key": []byte("key")},
				},
			},
			wantRootCAs: true,
		},
		{
			name: "TrustedCASecret takes priority over auto-discover",
			tlsSpec: &neo4jv1beta1.TLSSpec{
				Mode:            "cert-manager",
				TrustedCASecret: "custom-ca",
			},
			resourceName: "my-cluster",
			namespace:    "default",
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "custom-ca", Namespace: "default"},
					Data:       map[string][]byte{"ca.crt": caPEM},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-tls-secret", Namespace: "default"},
					Data:       map[string][]byte{"ca.crt": caPEM},
				},
			},
			wantRootCAs: true,
		},
		{
			name: "TrustedCASecret missing falls back to insecure",
			tlsSpec: &neo4jv1beta1.TLSSpec{
				Mode:            "cert-manager",
				TrustedCASecret: "nonexistent",
			},
			resourceName:     "my-cluster",
			namespace:        "default",
			secrets:          []corev1.Secret{},
			wantInsecureSkip: true,
		},
		{
			name:         "secret exists but no ca.crt key falls back to insecure",
			tlsSpec:      &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
			resourceName: "my-cluster",
			namespace:    "default",
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-tls-secret", Namespace: "default"},
					Data:       map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")},
				},
			},
			wantInsecureSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for i := range tt.secrets {
				builder = builder.WithObjects(&tt.secrets[i])
			}
			k8sClient := builder.Build()

			result := buildTLSConfig(context.Background(), k8sClient, tt.namespace, tt.resourceName, tt.tlsSpec)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil TLS config, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil TLS config, got nil")
			}

			if tt.wantInsecureSkip {
				if !result.InsecureSkipVerify {
					t.Error("expected InsecureSkipVerify=true")
				}
				if result.RootCAs != nil {
					t.Error("expected nil RootCAs when falling back to insecure")
				}
			}

			if tt.wantRootCAs {
				if result.RootCAs == nil {
					t.Error("expected RootCAs to be set")
				}
				if result.InsecureSkipVerify {
					t.Error("expected InsecureSkipVerify=false when RootCAs set")
				}
			}
		})
	}
}
