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
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
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

func TestResolveDeploymentsDefaultsModeToReverseProxy(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	// Mode omitted (empty) must normalise to ReverseProxy (BDR-015 usability revision).
	cr := studioCR(neo4jv1.StudioDeployment{Name: "ext", URI: "neo4j+s://x.example:7687"})
	got, rerr := ResolveDeployments(context.Background(), c, cr)
	if rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if len(got) != 1 || got[0].Mode != string(neo4jv1.StudioModeReverseProxy) {
		t.Fatalf("mode = %q, want ReverseProxy", got[0].Mode)
	}
	// ReverseProxy must not set a Direct publicUri.
	if got[0].PublicURI != "" {
		t.Fatalf("publicUri = %q, want empty for ReverseProxy", got[0].PublicURI)
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
			neo4jv1.StudioDeployment{Name: "direct", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "g"}, Mode: neo4jv1.StudioModeDirect},
			neo4jv1.StudioDeployment{Name: "proxy", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "g"}, Mode: neo4jv1.StudioModeReverseProxy},
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

	t.Run("neo4jRef resolves to HTTPS when the Neo4j exposes the https connector", func(t *testing.T) {
		// Customer feedback: no longer hard-coded to http://…:7474. A backing Neo4j that publishes the
		// https connector on its client Service is reached over TLS, honouring its declared https port.
		n := neo4j("tls", neo4jv1.EditionEnterprise)
		httpsPort := int32(7473)
		n.Spec.Connectivity = &neo4jv1.ConnectivitySpec{
			Listeners: &neo4jv1.ConnectivityListenersSpec{HTTPS: &httpsPort},
			Service:   &neo4jv1.ConnectivityServiceSpec{Expose: []string{"bolt", "https"}},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "proxy", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "tls"}, Mode: neo4jv1.StudioModeReverseProxy})
		got, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr != nil {
			t.Fatalf("unexpected error: %v", rerr)
		}
		if got[0].URI != "https://tls.ns.svc:7473" {
			t.Fatalf("URI = %q, want https://tls.ns.svc:7473", got[0].URI)
		}
	})

	t.Run("neo4jRef.pool targets the cluster pool's headless Service", func(t *testing.T) {
		// Customer feedback: the north-south client Service only fronts primaries, so the GDS analytics
		// pool is reachable only via its own pool Service. pool: analytics resolves there.
		n := neo4j("cl", neo4jv1.EditionEnterprise)
		n.Spec.Topology = neo4jv1.TopologySpec{
			Mode:        neo4jv1.TopologyModeCluster,
			Primaries:   &neo4jv1.PrimariesSpec{Members: 3},
			Secondaries: &neo4jv1.SecondariesSpec{Analytics: &neo4jv1.SecondaryPoolSpec{Members: 1}},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "gds", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "cl", Pool: "analytics"}, Mode: neo4jv1.StudioModeReverseProxy})
		got, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr != nil {
			t.Fatalf("unexpected error: %v", rerr)
		}
		if got[0].URI != "http://cl-analytics.ns.svc:7474" {
			t.Fatalf("URI = %q, want http://cl-analytics.ns.svc:7474", got[0].URI)
		}
	})

	t.Run("neo4jRef.pool on an unconfigured pool fails fast", func(t *testing.T) {
		n := neo4j("cl", neo4jv1.EditionEnterprise)
		n.Spec.Topology = neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeCluster, Primaries: &neo4jv1.PrimariesSpec{Members: 3}}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "gds", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "cl", Pool: "analytics"}})
		_, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr == nil {
			t.Fatalf("want error for missing analytics pool, got nil")
		}
	})

	t.Run("missing neo4jRef is retryable TargetNotFound", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "d", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "ghost"}, Mode: neo4jv1.StudioModeDirect})
		_, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr == nil || rerr.Kind != ResolveTargetNotFound {
			t.Fatalf("want ResolveTargetNotFound, got %+v", rerr)
		}
	})

	t.Run("community edition is terminal EditionUnsupported", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j("comm", neo4jv1.EditionCommunity)).Build()
		cr := studioCR(neo4jv1.StudioDeployment{Name: "d", Neo4jRef: &neo4jv1.StudioNeo4jRef{Name: "comm"}, Mode: neo4jv1.StudioModeDirect})
		_, rerr := ResolveDeployments(context.Background(), c, cr)
		if rerr == nil || rerr.Kind != ResolveEditionUnsupported {
			t.Fatalf("want ResolveEditionUnsupported, got %+v", rerr)
		}
	})
}

func TestResolveAssetStore(t *testing.T) {
	s := testScheme(t)

	t.Run("external uri used verbatim", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		cr := studioCR() // studioCR seeds assetStore.URI = bolt://a:7687
		uri, rerr := ResolveAssetStore(context.Background(), c, cr)
		if rerr != nil {
			t.Fatalf("unexpected error: %v", rerr)
		}
		if uri != "bolt://a:7687" {
			t.Fatalf("uri = %q, want bolt://a:7687", uri)
		}
	})

	t.Run("in-cluster neo4jRef resolves to the Bolt client Service URI", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j("g", neo4jv1.EditionEnterprise)).Build()
		cr := studioCR()
		cr.Spec.AssetStore = neo4jv1.StudioAssetStore{
			Neo4jRef: &neo4jv1.Neo4jRef{Name: "g"},
			Database: "assets",
			Auth:     neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "a-auth"}},
		}
		uri, rerr := ResolveAssetStore(context.Background(), c, cr)
		if rerr != nil {
			t.Fatalf("unexpected error: %v", rerr)
		}
		if uri != "neo4j://g.ns.svc:7687" {
			t.Fatalf("uri = %q, want neo4j://g.ns.svc:7687", uri)
		}
	})

	t.Run("missing neo4jRef is retryable TargetNotFound", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		cr := studioCR()
		cr.Spec.AssetStore = neo4jv1.StudioAssetStore{
			Neo4jRef: &neo4jv1.Neo4jRef{Name: "ghost"},
			Database: "assets",
			Auth:     neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: "a-auth"}},
		}
		_, rerr := ResolveAssetStore(context.Background(), c, cr)
		if rerr == nil || rerr.Kind != ResolveTargetNotFound {
			t.Fatalf("want ResolveTargetNotFound, got %+v", rerr)
		}
	})
}

// recordingAdmin embeds the Admin interface (nil) and records only the provisioning calls under test.
type recordingAdmin struct {
	intneo4j.Admin
	db, user, pass, role string
	failOn               string
}

func (r *recordingAdmin) CreateDatabaseIfNotExists(_ context.Context, name string) error {
	r.db = name
	if r.failOn == "db" {
		return fmt.Errorf("boom")
	}
	return nil
}

func (r *recordingAdmin) CreateUserIfNotExists(_ context.Context, u, p string) error {
	r.user, r.pass = u, p
	if r.failOn == "user" {
		return fmt.Errorf("boom")
	}
	return nil
}

func (r *recordingAdmin) GrantRole(_ context.Context, role, _ string) error {
	r.role = role
	return nil
}

func TestProvisionAssetStore(t *testing.T) {
	t.Run("runs the three idempotent ops with the architect role and param password", func(t *testing.T) {
		a := &recordingAdmin{}
		if err := ProvisionAssetStore(context.Background(), a, "assets", "architect-svc", "s3cret"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.db != "assets" || a.user != "architect-svc" || a.pass != "s3cret" || a.role != assetStoreArchitectRole {
			t.Fatalf("unexpected calls: %+v", a)
		}
	})

	t.Run("empty inputs are rejected before dialing", func(t *testing.T) {
		if err := ProvisionAssetStore(context.Background(), &recordingAdmin{}, "", "u", "p"); err == nil {
			t.Fatal("want error for empty database")
		}
	})

	t.Run("a failing op is surfaced", func(t *testing.T) {
		if err := ProvisionAssetStore(context.Background(), &recordingAdmin{failOn: "user"}, "assets", "u", "p"); err == nil {
			t.Fatal("want error when CreateUser fails")
		}
	})
}
