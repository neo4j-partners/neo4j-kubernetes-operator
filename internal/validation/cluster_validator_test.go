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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestClusterValidator_ValidateCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewClusterValidator(fakeClient)

	tests := []struct {
		name    string
		cluster *neo4jv1alpha1.Neo4jEnterpriseCluster
		wantErr bool
	}{
		{
			name: "valid cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 4,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid image version",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "4.4.0",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 4,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid topology with even servers (warnings only)",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26.0",
						PullPolicy: "IfNotPresent",
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
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
	_ = neo4jv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewClusterValidator(fakeClient)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0",
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "100Gi",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
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

	if cluster.Spec.Auth == nil || cluster.Spec.Auth.Provider != "native" {
		t.Errorf("Expected auth provider to be defaulted to 'native'")
	}
}
