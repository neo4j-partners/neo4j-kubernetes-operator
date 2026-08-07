package formation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestDesiredAndTailMembers(t *testing.T) {
	neo4j := testClusterCR(3)
	desired := DesiredMembers(neo4j)
	if len(desired) != 3 {
		t.Fatalf("desired len = %d", len(desired))
	}
	if desired[0].BoltAddress != "prod-primary-0.default.svc.cluster.local:7687" {
		t.Fatalf("addr = %q", desired[0].BoltAddress)
	}
	if desired[0].ModeConstraint != "PRIMARY" {
		t.Fatalf("mode = %q", desired[0].ModeConstraint)
	}

	tail := TailMembers(neo4j, render.PoolPrimary, 5)
	if len(tail) != 2 || tail[0].Ordinal != 4 || tail[1].Ordinal != 3 {
		t.Fatalf("tail = %#v", tail)
	}
	if TailMembers(neo4j, render.PoolPrimary, 3) != nil {
		t.Fatal("no tail when current==desired")
	}
}

func TestDrainOKAnnotationRoundTrip(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "prod"}}
	SetDrainOK(neo4j, render.PoolPrimary, 3, false)
	SetDrainOK(neo4j, render.PoolRead, 1, false)
	got := ParseDrainOK(neo4j)
	if got["primary"] != 3 || got["read"] != 1 {
		t.Fatalf("parse = %v", got)
	}
	SetDrainOK(neo4j, render.PoolRead, 0, true)
	if _, ok := ParseDrainOK(neo4j)["read"]; ok {
		t.Fatal("read should be cleared")
	}
}

func TestEffectiveReplicasHoldsUntilDrainOK(t *testing.T) {
	neo4j := testClusterCR(3)
	if got := EffectiveReplicas(neo4j, render.PoolPrimary, 3, 5); got != 5 {
		t.Fatalf("hold = %d", got)
	}
	SetDrainOK(neo4j, render.PoolPrimary, 3, false)
	if got := EffectiveReplicas(neo4j, render.PoolPrimary, 3, 5); got != 3 {
		t.Fatalf("after drain-ok = %d", got)
	}
	if got := EffectiveReplicas(neo4j, render.PoolPrimary, 5, 3); got != 5 {
		t.Fatalf("scale-up = %d", got)
	}
}

func TestEffectiveReplicasPrimaryCap(t *testing.T) {
	neo4j := testClusterCR(3)
	SetPrimaryReplicasCap(neo4j, 1, false)
	if got := EffectiveReplicas(neo4j, render.PoolPrimary, 3, 1); got != 1 {
		t.Fatalf("cap holds growth = %d", got)
	}
	if got := EffectiveReplicas(neo4j, render.PoolPrimary, 3, 3); got != 3 {
		t.Fatalf("already overshot hold = %d", got)
	}
	if got := EffectiveReplicas(neo4j, render.PoolAnalytics, 2, 1); got != 2 {
		t.Fatalf("analytics uncapped = %d", got)
	}
}

func TestAdminBoltURIUsesRouting(t *testing.T) {
	neo4j := testClusterCR(3)
	got := AdminBoltURI(neo4j)
	want := "neo4j://prod.default.svc:7687"
	if got != want {
		t.Fatalf("AdminBoltURI = %q want %q", got, want)
	}
}

func TestClientBoltURIIgnoresAttackerClusterDomain(t *testing.T) {
	neo4j := testClusterCR(3)
	neo4j.Spec.Connectivity = &neo4jv1beta1.ConnectivitySpec{ClusterDomain: "evil.example.com"}
	got := ClientBoltURI(neo4j)
	want := "bolt://prod.default.svc:7687"
	if got != want {
		t.Fatalf("ClientBoltURI = %q want %q (must not use CR clusterDomain)", got, want)
	}
	if AdminBoltURI(neo4j) != "neo4j://prod.default.svc:7687" {
		t.Fatalf("AdminBoltURI still embeds clusterDomain: %q", AdminBoltURI(neo4j))
	}
}
