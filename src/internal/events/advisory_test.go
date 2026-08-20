package events

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func obj(generation int64) *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{
		Name: "c", Namespace: "ns", UID: "uid-1", Generation: generation,
	}}
}

func drain(t *testing.T, rec *record.FakeRecorder) []string {
	t.Helper()
	var got []string
	for {
		select {
		case e := <-rec.Events:
			got = append(got, e)
		default:
			return got
		}
	}
}

// The reconcile loop revisits the same generation many times; the advisory belongs to the spec, so
// only the first pass may reach the recorder.
func TestEmitOncePerGeneration(t *testing.T) {
	rec := record.NewFakeRecorder(10)
	var a Advisory
	for range 5 {
		a.Emit(rec, obj(1), corev1.EventTypeWarning, "InsecureAdminConnection", "plaintext admin bolt")
	}
	if got := drain(t, rec); len(got) != 1 {
		t.Fatalf("expected 1 event, got %d: %v", len(got), got)
	}
}

// A new generation is a new statement about a new spec, so it is reported again.
func TestEmitAgainOnNewGeneration(t *testing.T) {
	rec := record.NewFakeRecorder(10)
	var a Advisory
	a.Emit(rec, obj(1), corev1.EventTypeWarning, "InsecureAdminConnection", "plaintext admin bolt")
	a.Emit(rec, obj(2), corev1.EventTypeWarning, "InsecureAdminConnection", "plaintext admin bolt")
	if got := drain(t, rec); len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(got), got)
	}
}

// One reason carries several distinct advisories — one per duplicate config key, one per mounted
// Secret — and collapsing them on the reason alone would hide all but the first.
func TestEmitKeepsDistinctMessagesOfOneReason(t *testing.T) {
	rec := record.NewFakeRecorder(10)
	var a Advisory
	a.Emitf(rec, obj(1), corev1.EventTypeNormal, "SecretMounted", "Mounting Secret %q", "alpha")
	a.Emitf(rec, obj(1), corev1.EventTypeNormal, "SecretMounted", "Mounting Secret %q", "beta")
	a.Emitf(rec, obj(1), corev1.EventTypeNormal, "SecretMounted", "Mounting Secret %q", "alpha")
	got := drain(t, rec)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "alpha") || !strings.Contains(got[1], "beta") {
		t.Fatalf("events %v", got)
	}
}

// Two objects share nothing: the memo is per UID, not per reason.
func TestEmitIsPerObject(t *testing.T) {
	rec := record.NewFakeRecorder(10)
	var a Advisory
	first := obj(1)
	second := obj(1)
	second.UID = "uid-2"
	a.Emit(rec, first, corev1.EventTypeWarning, "InsecureAdminConnection", "plaintext admin bolt")
	a.Emit(rec, second, corev1.EventTypeWarning, "InsecureAdminConnection", "plaintext admin bolt")
	if got := drain(t, rec); len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(got), got)
	}
}

func TestEmitWithoutRecorderIsNoop(t *testing.T) {
	var a Advisory
	a.Emit(nil, obj(1), corev1.EventTypeWarning, "InsecureAdminConnection", "plaintext admin bolt")
}
