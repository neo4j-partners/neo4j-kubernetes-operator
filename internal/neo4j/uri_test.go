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

package neo4j

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// TestBuildConnectionURIForEnterprise locks in the routing scheme for the
// operator's outbound Bolt client. Reverting to bolt:// silently disables
// leader routing for admin commands (CREATE/DROP USER, GRANT/REVOKE,
// CREATE OR REPLACE AUTH RULE, etc.) on multi-server clusters and produces
// visible Ready ↔ Failed flicker on Neo4jRole/User/AuthRule controllers.
func TestBuildConnectionURIForEnterprise(t *testing.T) {
	cases := []struct {
		name     string
		tls      *neo4jv1beta1.TLSSpec
		expected string
	}{
		{
			name:     "TLS disabled → neo4j://",
			tls:      &neo4jv1beta1.TLSSpec{Mode: "disabled"},
			expected: "neo4j://my-cluster-client.my-ns.svc.cluster.local:7687",
		},
		{
			name:     "TLS nil → neo4j://",
			tls:      nil,
			expected: "neo4j://my-cluster-client.my-ns.svc.cluster.local:7687",
		},
		{
			name:     "TLS cert-manager → neo4j+s://",
			tls:      &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
			expected: "neo4j+s://my-cluster-client.my-ns.svc.cluster.local:7687",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "my-ns"},
				Spec:       neo4jv1beta1.Neo4jEnterpriseClusterSpec{TLS: tc.tls},
			}
			got := buildConnectionURIForEnterprise(cluster)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// TestBuildConnectionURIForStandalone enforces routing-scheme parity with the
// cluster builder. Single-member topologies still respond to
// dbms.routing.getRoutingTable, so neo4j:// works identically to bolt://;
// keeping both code paths uniform guards against drift.
func TestBuildConnectionURIForStandalone(t *testing.T) {
	cases := []struct {
		name     string
		tls      *neo4jv1beta1.TLSSpec
		expected string
	}{
		{
			name:     "TLS disabled → neo4j://",
			tls:      &neo4jv1beta1.TLSSpec{Mode: "disabled"},
			expected: "neo4j://my-standalone-service.my-ns.svc.cluster.local:7687",
		},
		{
			name:     "TLS nil → neo4j://",
			tls:      nil,
			expected: "neo4j://my-standalone-service.my-ns.svc.cluster.local:7687",
		},
		{
			name:     "TLS cert-manager → neo4j+s://",
			tls:      &neo4jv1beta1.TLSSpec{Mode: "cert-manager"},
			expected: "neo4j+s://my-standalone-service.my-ns.svc.cluster.local:7687",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{
				ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "my-ns"},
				Spec:       neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{TLS: tc.tls},
			}
			got := buildConnectionURIForStandalone(standalone)
			assert.Equal(t, tc.expected, got)
		})
	}
}
