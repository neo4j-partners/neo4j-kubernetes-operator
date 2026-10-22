/*
Copyright Neo4j.

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

package studio

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func neo4j(name string, edition neo4jv1.Edition) *neo4jv1.Neo4j {
	return &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Spec: neo4jv1.Neo4jSpec{
			Edition:  edition,
			Version:  "2025.01.0",
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
		},
	}
}

func studioCR(deps ...neo4jv1.StudioDeployment) *neo4jv1.Neo4jStudio {
	return &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			Version:     "1.0.0",
			Deployments: deps,
			AssetStore:  neo4jv1.StudioAssetStore{URI: "bolt://a:7687", Database: "assets", Auth: neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "a-auth"}}},
		},
	}
}

func TestResolveDeployments(t *testing.T) {
	s := testScheme(t)

	t.Run("external uri used verbatim", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "ext", URI: "neo4j+s://x.example:7687", Mode: neo4jv1.StudioModeDirect})
		got, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr != nil {
			t.Fatalf("unexpected error: %v", rerr)
		}
		if len(got) != 1 || got[0].URI != "neo4j+s://x.example:7687" {
			t.Fatalf("external uri not echoed verbatim: %+v", got)
		}
	})

	t.Run("in-cluster neo4jRef resolves to service URI per mode", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j("g", neo4jv1.EditionEnterprise)).Build()
		cr := studioCR(
			neo4jv1.StudioDeployment{Name: "direct", Neo4jRef: &neo4jv1.Neo4jRef{Name: "g"}, Mode: neo4jv1.StudioModeDirect},
			neo4jv1.StudioDeployment{Name: "proxy", Neo4jRef: &neo4jv1.Neo4jRef{Name: "g"}, Mode: neo4jv1.StudioModeReverseProxy},
		)
		got, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr != nil {
			t.Fatalf("unexpected error: %v", rerr)
		}
		// uri is always the server-reachable HTTP API; Direct additionally exposes the Bolt publicUri.
		if got[0].URI != "http://g.ns.svc:7474" {
			t.Errorf("Direct URI = %q, want http://g.ns.svc:7474", got[0].URI)
		}
		if got[0].PublicURI != "bolt://g.ns.svc:7687" {
			t.Errorf("Direct PublicURI = %q, want bolt://g.ns.svc:7687", got[0].PublicURI)
		}
		if got[1].URI != "http://g.ns.svc:7474" {
			t.Errorf("ReverseProxy URI = %q, want http://g.ns.svc:7474", got[1].URI)
		}
		if got[1].PublicURI != "" {
			t.Errorf("ReverseProxy PublicURI must be empty, got %q", got[1].PublicURI)
		}
	})

	t.Run("missing neo4jRef is retryable TargetNotFound", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "d", Neo4jRef: &neo4jv1.Neo4jRef{Name: "ghost"}, Mode: neo4jv1.StudioModeDirect})
		_, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr == nil || rerr.Kind != ResolveTargetNotFound {
			t.Fatalf("want ResolveTargetNotFound, got %+v", rerr)
		}
	})

	t.Run("community edition is terminal EditionUnsupported", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j("comm", neo4jv1.EditionCommunity)).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "d", Neo4jRef: &neo4jv1.Neo4jRef{Name: "comm"}, Mode: neo4jv1.StudioModeDirect})
		_, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr == nil || rerr.Kind != ResolveEditionUnsupported {
			t.Fatalf("want ResolveEditionUnsupported, got %+v", rerr)
		}
	})
}
