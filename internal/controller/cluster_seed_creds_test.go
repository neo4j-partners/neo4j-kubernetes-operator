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

package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// TestEnsureClusterHasSeedCreds_Matrix pins the three branches of the
// helper: already-configured (no-op), auto-inherit annotation set
// (patches the cluster), and not configured + no annotation (returns an
// actionable error).
func TestEnsureClusterHasSeedCreds_Matrix(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	type tc struct {
		name                string
		clusterSpec         neo4jv1beta1.Neo4jEnterpriseClusterSpec
		clusterAnnotations  map[string]string
		credsSecretName     string
		wantAutoInherited   bool
		wantErrSubstr       string
		wantExtraEnvFromLen int // expected length AFTER call
	}

	cases := []tc{
		{
			name:                "empty credsSecretName → no-op",
			credsSecretName:     "",
			wantAutoInherited:   false,
			wantExtraEnvFromLen: 0,
		},
		{
			name: "already configured → no-op",
			clusterSpec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				ExtraEnvFrom: []corev1.EnvFromSource{
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"}}},
				},
			},
			credsSecretName:     "minio-creds",
			wantAutoInherited:   false,
			wantExtraEnvFromLen: 1,
		},
		{
			name:                "missing creds + no annotation → actionable error",
			credsSecretName:     "minio-creds",
			wantErrSubstr:       "extraEnvFrom",
			wantExtraEnvFromLen: 0,
		},
		{
			name:               "missing creds + auto-inherit annotation → patches cluster",
			clusterAnnotations: map[string]string{AutoInheritSeedCredsAnnotation: "true"},
			credsSecretName:    "minio-creds",
			wantAutoInherited:  true,
			// After the call: 0 (initial) + 1 (auto-inherited) = 1.
			wantExtraEnvFromLen: 1,
		},
		{
			name: "auto-inherit appends — doesn't replace existing entries",
			clusterSpec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				AcceptLicenseAgreement: "eval",
				ExtraEnvFrom: []corev1.EnvFromSource{
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "other-secret"}}},
				},
			},
			clusterAnnotations:  map[string]string{AutoInheritSeedCredsAnnotation: "true"},
			credsSecretName:     "minio-creds",
			wantAutoInherited:   true,
			wantExtraEnvFromLen: 2,
		},
		{
			name:               "auto-inherit annotation set to non-true → still errors",
			clusterAnnotations: map[string]string{AutoInheritSeedCredsAnnotation: "false"},
			credsSecretName:    "minio-creds",
			wantErrSubstr:      "extraEnvFrom",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ec",
					Namespace:   "default",
					Annotations: c.clusterAnnotations,
				},
				Spec: c.clusterSpec,
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cluster).
				Build()

			autoInherited, err := EnsureClusterHasSeedCreds(context.Background(), fakeClient, cluster, c.credsSecretName)

			if c.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrSubstr) {
					t.Errorf("err=%v, want substring %q", err, c.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if autoInherited != c.wantAutoInherited {
				t.Errorf("autoInherited=%v, want %v", autoInherited, c.wantAutoInherited)
			}
			if got := len(cluster.Spec.ExtraEnvFrom); got != c.wantExtraEnvFromLen {
				t.Errorf("len(cluster.Spec.ExtraEnvFrom)=%d, want %d (entries: %+v)", got, c.wantExtraEnvFromLen, cluster.Spec.ExtraEnvFrom)
			}
			if autoInherited {
				// Audit annotation MUST be set after auto-inherit.
				if got := cluster.Annotations[AutoInheritedFromAnnotation]; got != c.credsSecretName {
					t.Errorf("AutoInheritedFromAnnotation=%q, want %q", got, c.credsSecretName)
				}
			}
		})
	}
}
