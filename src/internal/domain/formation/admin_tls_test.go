package formation

import (
	"context"
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestAdminConnectOptsFailClosed(t *testing.T) {
	rec := record.NewFakeRecorder(2)
	r := &Reconciler{Recorder: rec}
	n := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}}
	_, err := r.adminConnectOpts(context.Background(), n)
	if err == nil || !strings.Contains(err.Error(), "insecureAdminConnection") {
		t.Fatalf("expected fail-closed, got %v", err)
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, "AdminBoltTLSRequired") {
			t.Fatalf("event %q", e)
		}
	default:
		t.Fatal("expected Warning event")
	}
}

func TestAdminConnectOptsInsecureEmitsWarning(t *testing.T) {
	rec := record.NewFakeRecorder(2)
	r := &Reconciler{Recorder: rec}
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Trust: &neo4jv1beta1.TrustSpec{InsecureAdminConnection: true},
		},
	}
	opts, err := r.adminConnectOpts(context.Background(), n)
	if err != nil || !opts.AllowPlaintext || opts.RootCAs != nil {
		t.Fatalf("opts=%#v err=%v", opts, err)
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, corev1.EventTypeWarning) || !strings.Contains(e, "InsecureAdminConnection") {
			t.Fatalf("event %q", e)
		}
	default:
		t.Fatal("expected Warning event")
	}
}
