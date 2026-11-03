package status

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func drainEvents(rec *record.FakeRecorder) int {
	n := 0
	for {
		select {
		case <-rec.Events:
			n++
		default:
			return n
		}
	}
}

// The completion Event is the StorageResizing→PVCBound edge, authored here from one capacity
// snapshot so it cannot be split across the pipeline — the kind/local-path flake where every claim
// catches up at once used to move the condition without the old persistence step ever seeing both
// halves, and the Event was lost.
func TestReportResizeCompletedEdge(t *testing.T) {
	cases := []struct {
		name      string
		prev      oracle.Reason
		now       oracle.Reason
		ready     bool
		wantEvent bool
	}{
		{name: "resizing settled to bound", prev: oracle.ReasonStorageResizing, now: oracle.ReasonPVCBound, ready: true, wantEvent: true},
		{name: "still resizing", prev: oracle.ReasonStorageResizing, now: oracle.ReasonStorageResizing, ready: false},
		{name: "bound all along, nothing was resizing", prev: oracle.ReasonPVCBound, now: oracle.ReasonPVCBound, ready: true},
		{name: "resize failed, not a completion", prev: oracle.ReasonStorageResizing, now: oracle.ReasonStorageResizeFailed, ready: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := record.NewFakeRecorder(20)
			w := &Writer{Recorder: rec}
			neo4j := standaloneWithDynamicSC("dev", "default", "standard")

			w.reportResizeCompleted(neo4j, tc.prev.String(), tc.now, tc.ready)

			select {
			case ev := <-rec.Events:
				if !tc.wantEvent {
					t.Fatalf("unexpected event %q", ev)
				}
				if !strings.Contains(ev, oracle.ReasonStorageResizeCompleted.String()) {
					t.Errorf("event = %q, want reason %s", ev, oracle.ReasonStorageResizeCompleted)
				}
			default:
				if tc.wantEvent {
					t.Fatal("expected a StorageResizeCompleted event")
				}
			}
		})
	}
}

// A racy requeue off a stale cache can read the previous reason as StorageResizing a second time, so
// the edge fires twice. The Advisory memo keeps the completion to one Event per generation, and a
// fresh grow (a generation bump) re-arms it.
func TestReportResizeCompletedOncePerGeneration(t *testing.T) {
	rec := record.NewFakeRecorder(20)
	w := &Writer{Recorder: rec}
	neo4j := standaloneWithDynamicSC("dev", "default", "standard")
	neo4j.UID = "uid-1"
	neo4j.Generation = 2

	w.reportResizeCompleted(neo4j, oracle.ReasonStorageResizing.String(), oracle.ReasonPVCBound, true)
	w.reportResizeCompleted(neo4j, oracle.ReasonStorageResizing.String(), oracle.ReasonPVCBound, true) // stale requeue
	if got := drainEvents(rec); got != 1 {
		t.Fatalf("events in one generation = %d, want exactly 1", got)
	}

	neo4j.Generation = 3 // a later grow bumps generation
	w.reportResizeCompleted(neo4j, oracle.ReasonStorageResizing.String(), oracle.ReasonPVCBound, true)
	if got := drainEvents(rec); got != 1 {
		t.Fatalf("events after generation bump = %d, want 1", got)
	}
}

// boundClaim is a claim as the API server really returns one: a request, and a capacity that may
// still be catching up with it.
func boundClaim(name, request, capacity string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(request)},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase:    corev1.ClaimBound,
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(capacity)},
		},
	}
	return pvc
}

func writerWith(t *testing.T, objs ...runtime.Object) *Writer {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = neo4jv1.AddToScheme(scheme)
	return NewWriter(fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build())
}

// Bound is not the same as ready: readiness used to short-circuit there, so a claim serving the old
// size after a grow reported healthy. These cases pin the three answers apart.
func TestObservePoolStorageReadySizes(t *testing.T) {
	cases := []struct {
		name       string
		specSize   string
		request    string
		capacity   string
		wantOK     bool
		wantReason oracle.Reason
		wantInMsg  string
	}{
		{
			name: "settled at the requested size", specSize: "10Gi", request: "10Gi", capacity: "10Gi",
			wantOK: true, wantReason: oracle.ReasonPVCBound,
		},
		{
			name: "request raised, capacity catching up", specSize: "10Gi", request: "10Gi", capacity: "5Gi",
			wantReason: oracle.ReasonStorageResizing, wantInMsg: "expansion in flight",
		},
		{
			name: "claim still behind the spec", specSize: "10Gi", request: "5Gi", capacity: "5Gi",
			wantReason: oracle.ReasonStorageResizeFailed, wantInMsg: "allowVolumeExpansion",
		},
		{
			// A claim someone grew by hand past the spec is not a problem to report.
			name: "claim larger than the spec", specSize: "10Gi", request: "20Gi", capacity: "20Gi",
			wantOK: true, wantReason: oracle.ReasonPVCBound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			neo4j := standaloneWithDynamicSC("dev", "default", "standard")
			neo4j.Spec.Storage.Volumes.Data.Dynamic.Size = tc.specSize
			w := writerWith(t, boundClaim("data-dev-server-0", tc.request, tc.capacity))

			ok, reason, msg := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 1)

			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (reason %q, message %q)", ok, tc.wantOK, reason, msg)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
			if tc.wantInMsg != "" && !strings.Contains(msg, tc.wantInMsg) {
				t.Errorf("message = %q, want it to mention %q", msg, tc.wantInMsg)
			}
		})
	}
}

// Only ordinal 0 used to be observed, so a member behind on a grow was invisible. Readiness has to
// span the pool.
func TestObservePoolStorageReadySpansOrdinals(t *testing.T) {
	neo4j := standaloneWithDynamicSC("dev", "default", "standard")
	neo4j.Spec.Storage.Volumes.Data.Dynamic.Size = "10Gi"
	w := writerWith(t,
		boundClaim("data-dev-server-0", "10Gi", "10Gi"),
		boundClaim("data-dev-server-1", "10Gi", "5Gi"),
		boundClaim("data-dev-server-2", "10Gi", "10Gi"),
	)

	ok, reason, msg := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 3)

	if ok || reason != oracle.ReasonStorageResizing {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	if !strings.Contains(msg, "data-dev-server-1") {
		t.Errorf("message = %q, want it to name the claim that is behind", msg)
	}
}

// A claim a scale-out has not created yet must not be held against the CR: the replica count passed
// in is the live StatefulSet's, so only claims that should already exist are judged.
func TestObservePoolStorageReadyIgnoresUncreatedOrdinals(t *testing.T) {
	neo4j := standaloneWithDynamicSC("dev", "default", "standard")
	w := writerWith(t, boundClaim("data-dev-server-0", "10Gi", "10Gi"))

	ok, reason, _ := w.observePoolStorageReady(t.Context(), render.StandaloneContext(neo4j), 1)

	if !ok || reason != oracle.ReasonPVCBound {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}
