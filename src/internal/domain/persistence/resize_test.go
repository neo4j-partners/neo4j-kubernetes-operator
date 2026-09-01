package persistence

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func storageLabels() map[string]string {
	return map[string]string{
		render.LabelInstance:  "dev",
		render.LabelComponent: "storage",
		render.LabelName:      render.AppNameValue,
		render.LabelManagedBy: render.ManagedByValue,
	}
}

func sizedNeo4j(size string) *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: size},
					},
				},
			},
		},
	}
}

func claim(name, request, capacity string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: storageLabels()},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(request)},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	if capacity != "" {
		pvc.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(capacity)}
	}
	return pvc
}

func statefulSet(name string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
}

func requestOf(t *testing.T, r *Reconciler, name string) string {
	t.Helper()
	var pvc corev1.PersistentVolumeClaim
	key := types.NamespacedName{Name: name, Namespace: "default"}
	if err := r.Client.Get(t.Context(), key, &pvc); err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	return q.String()
}

func newReconciler(t *testing.T, objs ...runtime.Object) (*Reconciler, *record.FakeRecorder) {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)
	rec := record.NewFakeRecorder(20)
	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	return &Reconciler{Client: c, Recorder: rec}, rec
}

// A grow must reach every ordinal, not just the one the status writer used to look at: the
// StatefulSet template keeps the old size, so nothing else would ever raise the others.
func TestExpandVolumesGrowsEveryOrdinal(t *testing.T) {
	neo4j := sizedNeo4j("10Gi")
	r, _ := newReconciler(t,
		statefulSet("dev-server", 3),
		claim("data-dev-server-0", "5Gi", "5Gi"),
		claim("data-dev-server-1", "5Gi", "5Gi"),
		claim("data-dev-server-2", "5Gi", "5Gi"),
	)

	r.expandVolumes(t.Context(), neo4j)

	for _, name := range []string{"data-dev-server-0", "data-dev-server-1", "data-dev-server-2"} {
		if got := requestOf(t, r, name); got != "10Gi" {
			t.Errorf("%s request = %s, want 10Gi", name, got)
		}
	}
}

// Claims past the live replica count are retained leftovers of a scale-in. Growing a volume no pod
// mounts would bill the user for nothing.
func TestExpandVolumesLeavesRetainedClaimsAlone(t *testing.T) {
	neo4j := sizedNeo4j("10Gi")
	r, _ := newReconciler(t,
		statefulSet("dev-server", 1),
		claim("data-dev-server-0", "5Gi", "5Gi"),
		claim("data-dev-server-1", "5Gi", "5Gi"),
	)

	r.expandVolumes(t.Context(), neo4j)

	if got := requestOf(t, r, "data-dev-server-0"); got != "10Gi" {
		t.Errorf("in-use claim = %s, want 10Gi", got)
	}
	if got := requestOf(t, r, "data-dev-server-1"); got != "5Gi" {
		t.Errorf("retained claim = %s, want it untouched at 5Gi", got)
	}
}

// CEL blocks a shrink at admission, but an older CRD would not. Never lower a request: Kubernetes
// rejects it outright, and a claim already larger is somebody's deliberate manual expansion.
func TestExpandVolumesNeverShrinks(t *testing.T) {
	neo4j := sizedNeo4j("5Gi")
	r, _ := newReconciler(t,
		statefulSet("dev-server", 1),
		claim("data-dev-server-0", "20Gi", "20Gi"),
	)

	r.expandVolumes(t.Context(), neo4j)

	if got := requestOf(t, r, "data-dev-server-0"); got != "20Gi" {
		t.Errorf("request = %s, want the claim left at 20Gi", got)
	}
}

// A PVC without operator provenance is not ours to touch (ADD-04).
func TestExpandVolumesSkipsForeignClaims(t *testing.T) {
	neo4j := sizedNeo4j("10Gi")
	foreign := claim("data-dev-server-0", "5Gi", "5Gi")
	foreign.Labels = map[string]string{"app": "someone-else"}
	r, _ := newReconciler(t, statefulSet("dev-server", 1), foreign)

	r.expandVolumes(t.Context(), neo4j)

	if got := requestOf(t, r, "data-dev-server-0"); got != "5Gi" {
		t.Errorf("request = %s, want the foreign claim untouched", got)
	}
}

// Before the StatefulSet exists there is nothing to grow — the template will carry the right size
// when the claims are created.
func TestExpandVolumesNoStatefulSet(t *testing.T) {
	neo4j := sizedNeo4j("10Gi")
	r, _ := newReconciler(t, claim("data-dev-server-0", "5Gi", "5Gi"))

	r.expandVolumes(t.Context(), neo4j)

	if got := requestOf(t, r, "data-dev-server-0"); got != "5Gi" {
		t.Errorf("request = %s, want 5Gi while no StatefulSet exists", got)
	}
}

func TestClaimsBehindCapacity(t *testing.T) {
	neo4j := sizedNeo4j("10Gi")
	r, _ := newReconciler(t,
		claim("data-dev-server-0", "10Gi", "10Gi"),
		claim("data-dev-server-1", "10Gi", "5Gi"),
	)

	behind := r.claimsBehindCapacity(t.Context(), neo4j)

	if len(behind) != 1 || behind[0] != "data-dev-server-1" {
		t.Errorf("behind = %v, want only data-dev-server-1", behind)
	}
}

// The completion Event is edge-triggered off the condition the previous pass published. Emitting on
// level would fire every pass and spend the object's Event budget.
func TestReportResizeCompleted(t *testing.T) {
	cases := []struct {
		name        string
		wasResizing bool
		behind      []string
		wantEvent   bool
	}{
		{name: "a grow that just finished", wasResizing: true, wantEvent: true},
		{name: "a grow still in flight", wasResizing: true, behind: []string{"data-dev-server-0"}},
		{name: "steady state, nothing was resizing", wasResizing: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			neo4j := sizedNeo4j("10Gi")
			r, rec := newReconciler(t)

			r.reportResizeCompleted(neo4j, tc.wasResizing, tc.behind)

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
