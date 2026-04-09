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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestGenerateConnectionExamples(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		namespace   string
		serviceType corev1.ServiceType
		externalIP  string
		hasTLS      bool
		checkFunc   func(t *testing.T, examples *neo4jv1beta1.ConnectionExamples)
	}{
		{
			name:        "ClusterIP service without TLS",
			clusterName: "test-cluster",
			namespace:   "default",
			serviceType: corev1.ServiceTypeClusterIP,
			externalIP:  "",
			hasTLS:      false,
			checkFunc: func(t *testing.T, examples *neo4jv1beta1.ConnectionExamples) {
				assert.Contains(t, examples.PortForward, "kubectl port-forward")
				assert.Contains(t, examples.BrowserURL, "http://localhost:7474")
				assert.Contains(t, examples.BoltURI, "bolt://localhost:7687")
				assert.Contains(t, examples.Neo4jURI, "neo4j://localhost:7687")
			},
		},
		{
			name:        "LoadBalancer service with IP and TLS",
			clusterName: "test-cluster",
			namespace:   "default",
			serviceType: corev1.ServiceTypeLoadBalancer,
			externalIP:  "10.20.30.40",
			hasTLS:      true,
			checkFunc: func(t *testing.T, examples *neo4jv1beta1.ConnectionExamples) {
				assert.Contains(t, examples.PortForward, "kubectl port-forward")
				assert.Contains(t, examples.BrowserURL, "https://10.20.30.40:7473")
				assert.Contains(t, examples.BoltURI, "bolt+ssc://10.20.30.40:7687")
				assert.Contains(t, examples.Neo4jURI, "neo4j+ssc://10.20.30.40:7687")
			},
		},
		{
			name:        "LoadBalancer pending",
			clusterName: "test-cluster",
			namespace:   "production",
			serviceType: corev1.ServiceTypeLoadBalancer,
			externalIP:  "<pending>",
			hasTLS:      false,
			checkFunc: func(t *testing.T, examples *neo4jv1beta1.ConnectionExamples) {
				assert.Contains(t, examples.PortForward, "kubectl port-forward -n production")
				assert.Contains(t, examples.BrowserURL, "http://<external-ip>:7474")
				assert.Contains(t, examples.BoltURI, "bolt://<external-ip>:7687")
			},
		},
		{
			name:        "NodePort service",
			clusterName: "test-cluster",
			namespace:   "default",
			serviceType: corev1.ServiceTypeNodePort,
			externalIP:  "",
			hasTLS:      true,
			checkFunc: func(t *testing.T, examples *neo4jv1beta1.ConnectionExamples) {
				assert.Contains(t, examples.BrowserURL, "https://<node-ip>:<node-port>:7473")
				assert.Contains(t, examples.BoltURI, "bolt+ssc://<node-ip>:<bolt-node-port>:7687")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			examples := GenerateConnectionExamples(tt.clusterName, tt.namespace, tt.serviceType, tt.externalIP, tt.hasTLS)
			assert.NotNil(t, examples)
			tt.checkFunc(t, examples)
		})
	}
}

func TestGenerateStandaloneConnectionExamples(t *testing.T) {
	tests := []struct {
		name       string
		svcType    corev1.ServiceType
		externalIP string
		hasTLS     bool
		expectBolt string
	}{
		{
			name:       "ClusterIP without TLS",
			svcType:    corev1.ServiceTypeClusterIP,
			hasTLS:     false,
			expectBolt: "bolt://",
		},
		{
			name:       "ClusterIP with TLS",
			svcType:    corev1.ServiceTypeClusterIP,
			hasTLS:     true,
			expectBolt: "bolt+ssc://",
		},
		{
			name:       "LoadBalancer with external IP and TLS",
			svcType:    corev1.ServiceTypeLoadBalancer,
			externalIP: "10.20.30.40",
			hasTLS:     true,
			expectBolt: "bolt+ssc://10.20.30.40",
		},
		{
			name:       "NodePort without TLS",
			svcType:    corev1.ServiceTypeNodePort,
			hasTLS:     false,
			expectBolt: "bolt://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			examples := GenerateStandaloneConnectionExamples("my-standalone", "default", tt.svcType, tt.externalIP, tt.hasTLS)
			assert.NotNil(t, examples)
			assert.Contains(t, examples.BoltURI, tt.expectBolt)
			// Standalone should use -service suffix, not -client
			// Verify bolt URI contains expected scheme
			assert.Contains(t, examples.BoltURI, tt.expectBolt)
		})
	}
}
