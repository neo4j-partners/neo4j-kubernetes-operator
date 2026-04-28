/*
Copyright 2025.

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

package validation

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func newUserValidatorClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func sampleCluster() *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
	}
}

func samplePasswordSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Data:       map[string][]byte{"password": []byte("supersecret")},
	}
}

func TestUserValidator_UsernameRules(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster(), samplePasswordSecret("creds"))
	v := NewUserValidator(cl)

	cases := []struct {
		name      string
		username  string
		wantError bool
	}{
		{"valid", "alice", false},
		{"valid with dot/hyphen", "alice.smith-1", false},
		{"starts with digit", "1bob", true},
		{"too long", strings.Repeat("a", 70), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := &neo4jv1beta1.Neo4jUser{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"},
				Spec: neo4jv1beta1.Neo4jUserSpec{
					ClusterRef:        "c",
					Username:          tc.username,
					PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "creds"},
				},
			}
			res := v.Validate(context.Background(), user)
			gotError := len(res.Errors) > 0
			if gotError != tc.wantError {
				t.Fatalf("username %q: gotError=%v wantError=%v errs=%v", tc.username, gotError, tc.wantError, res.Errors)
			}
		})
	}
}

func TestUserValidator_RequiresAuth(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster())
	v := NewUserValidator(cl)

	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jUserSpec{ClusterRef: "c", Username: "alice"},
	}
	res := v.Validate(context.Background(), user)
	if len(res.Errors) == 0 {
		t.Fatalf("expected error when neither password nor externalAuth is set")
	}

	user.Spec.ExternalAuth = []neo4jv1beta1.ExternalAuthProvider{{Provider: "oidc-okta", ID: "alice@okta"}}
	res = v.Validate(context.Background(), user)
	for _, e := range res.Errors {
		if strings.Contains(e.Error(), "auth provider") {
			t.Fatalf("did not expect 'auth provider' error after adding externalAuth, got %v", res.Errors)
		}
	}
}

func TestUserValidator_PasswordSecretMissing(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster())
	v := NewUserValidator(cl)
	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jUserSpec{
			ClusterRef:        "c",
			Username:          "alice",
			PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "missing"},
		},
	}
	res := v.Validate(context.Background(), user)
	if len(res.Errors) == 0 {
		t.Fatalf("expected error for missing secret")
	}
}

func TestUserValidator_PasswordSecretWrongKey(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "ns"},
		Data:       map[string][]byte{"other": []byte("x")},
	}
	cl := newUserValidatorClient(t, sampleCluster(), secret)
	v := NewUserValidator(cl)
	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jUserSpec{
			ClusterRef:        "c",
			Username:          "alice",
			PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "creds"},
		},
	}
	res := v.Validate(context.Background(), user)
	if len(res.Errors) == 0 {
		t.Fatalf("expected error for missing key")
	}
}

func TestUserValidator_NativeProviderInExternalAuthRejected(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster(), samplePasswordSecret("creds"))
	v := NewUserValidator(cl)
	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jUserSpec{
			ClusterRef:   "c",
			Username:     "alice",
			ExternalAuth: []neo4jv1beta1.ExternalAuthProvider{{Provider: "native", ID: "alice"}},
		},
	}
	res := v.Validate(context.Background(), user)
	if len(res.Errors) == 0 {
		t.Fatalf("expected rejection of 'native' in externalAuth, got none")
	}
}

func TestUserValidator_PublicWarning(t *testing.T) {
	cl := newUserValidatorClient(t, sampleCluster(), samplePasswordSecret("creds"))
	v := NewUserValidator(cl)
	user := &neo4jv1beta1.Neo4jUser{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jUserSpec{
			ClusterRef:        "c",
			Username:          "alice",
			PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "creds"},
			Roles:             []string{"PUBLIC", "reader"},
		},
	}
	res := v.Validate(context.Background(), user)
	if len(res.Warnings) == 0 {
		t.Fatalf("expected a warning about explicit PUBLIC, got %v", res.Warnings)
	}
}
