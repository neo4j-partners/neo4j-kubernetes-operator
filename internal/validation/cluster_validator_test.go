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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// TestClusterValidator_WiresConfigValidation proves ConfigValidator actually
// runs as part of the cluster validation flow the reconciler uses
// (ValidateCreate → validateCluster). Regression for the wiring gap where
// ConfigValidator existed but was never invoked, so its rejections (discovery,
// SSL, deprecated, and the runtime-managed advertised/topology keys) silently
// did nothing in production.
func TestClusterValidator_WiresConfigValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	validator := NewClusterValidator(fake.NewClientBuilder().WithScheme(scheme).Build())

	base := func() *neo4jv1beta1.Neo4jEnterpriseCluster {
		return &neo4jv1beta1.Neo4jEnterpriseCluster{
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0", PullPolicy: "IfNotPresent"},
				Storage:  neo4jv1beta1.StorageSpec{ClassName: "fast-ssd", Size: "100Gi"},
				Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
			},
		}
	}

	// Sanity: base cluster is valid, so a rejection below is attributable to config.
	if err := validator.ValidateCreate(context.Background(), base()); err != nil {
		t.Fatalf("base cluster should validate, got: %v", err)
	}

	// Each of these is rejected only if ConfigValidator runs.
	for _, key := range []string{
		"server.cluster.advertised_address", // runtime-managed (per-pod FQDN)
		"dbms.cluster.endpoints",            // operator-managed discovery
		"dbms.ssl.policy.bolt.client_auth",  // operator-managed SSL
	} {
		c := base()
		c.Spec.Config = map[string]string{key: "x"}
		if err := validator.ValidateCreate(context.Background(), c); err == nil {
			t.Errorf("ValidateCreate accepted spec.config[%q]; ConfigValidator not wired?", key)
		}
	}
}

func TestClusterValidator_ValidateCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewClusterValidator(fakeClient)

	tests := []struct {
		name    string
		cluster *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErr bool
	}{
		{
			name: "valid cluster",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 4,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid image version",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "4.4.0",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 4,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid topology with even servers (warnings only)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 5, // 4 + 1 total servers
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCreate(context.Background(), tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("ClusterValidator.ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClusterValidator_ApplyDefaults(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewClusterValidator(fakeClient)

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0",
			},
			Storage: neo4jv1beta1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "100Gi",
			},
			Topology: neo4jv1beta1.TopologyConfiguration{
				Servers: 2, // 2 servers
			},
		},
	}

	validator.ApplyDefaults(context.Background(), cluster)

	// Edition field removed - operator only supports enterprise edition

	if cluster.Spec.Image.PullPolicy != "IfNotPresent" {
		t.Errorf("Expected image pull policy to be defaulted to 'IfNotPresent', got %s", cluster.Spec.Image.PullPolicy)
	}

	if cluster.Spec.Topology.Servers != 2 {
		t.Errorf("Expected servers to remain unchanged at 2, got %d", cluster.Spec.Topology.Servers)
	}

	if cluster.Spec.TLS == nil || cluster.Spec.TLS.Mode != "disabled" {
		t.Errorf("Expected TLS mode to be defaulted to 'disabled'")
	}

	if cluster.Spec.Auth == nil {
		t.Errorf("Expected auth to be defaulted")
	} else if len(cluster.Spec.Auth.AuthenticationProviders) == 0 || cluster.Spec.Auth.AuthenticationProviders[0] != "native" {
		t.Errorf("Expected auth authenticationProviders to be defaulted to ['native'], got %v", cluster.Spec.Auth.AuthenticationProviders)
	}
}

func TestClusterValidator_NameLength(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewClusterValidator(fakeClient)

	tests := []struct {
		name    string
		cluster *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid short name",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster"},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0", PullPolicy: "IfNotPresent"},
					Storage:  neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
					Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
				},
			},
			wantErr: false,
		},
		{
			name: "name too long for DNS label",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", 57)},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26.0", PullPolicy: "IfNotPresent"},
					Storage:  neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "10Gi"},
					Topology: neo4jv1beta1.TopologyConfiguration{Servers: 3},
				},
			},
			wantErr: true,
			errMsg:  "no more than 56 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCreate(context.Background(), tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}
