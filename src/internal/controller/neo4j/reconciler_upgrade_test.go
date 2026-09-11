package neo4j

import (
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/connectivity"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/formation"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/persistence"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/serverconfig"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/trust"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/upgrade"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/workload"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/status"
)

// pipelineFor wires the real domain steps against a fake API server. The reconciler holds concrete
// types rather than the shared.Reconciler interface, so the steps cannot be swapped for spies —
// which is fine here: whether the workload step ran is observable from its output, and a real step
// is the stronger thing to assert against anyway.
func pipelineFor(c client.Client, s *runtime.Scheme) *Neo4jReconciler {
	return &Neo4jReconciler{
		Client:       c,
		Scheme:       s,
		Persistence:  persistence.New(c, nil),
		Trust:        trust.New(c, s),
		ServerConfig: serverconfig.New(c, s),
		Workload:     workload.New(c, s),
		Connectivity: connectivity.New(c, s),
		Formation:    formation.New(c, s, nil),
		StatusWriter: status.NewWriter(c),
	}
}

// stsExists reports whether the Standalone pool's StatefulSet was created — the observable proof
// that the workload step ran.
func stsExists(t *testing.T, c client.Client, n *neo4jv1beta1.Neo4j) bool {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	err := c.Get(t.Context(), client.ObjectKey{Name: n.Name + "-server", Namespace: n.Namespace}, sts)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get statefulset: %v", err)
	}
	return err == nil
}

func upgradeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("core scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("apps scheme: %v", err)
	}
	return s
}

func refusedCR() *neo4jv1beta1.Neo4j {
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2025.01.0", // older than what is running
			License: &neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeStandalone,
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
	n.Status.Version = "2026.05.0"
	return n
}

// The placement of the Preflight call is the whole point of it: domain/workload replaces the pod
// template, so a refusal that ran after it would arrive with the roll already started. Every other
// upgrade test calls Preflight directly and would still pass if someone moved the call below the
// step list — this is the one that would not.
func TestRefusedVersionChangeStopsBeforeAnyDomainStep(t *testing.T) {
	s := upgradeScheme(t)
	n := refusedCR()
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).WithStatusSubresource(n).Build()

	_, err := pipelineFor(c, s).runPipeline(t.Context(), n)
	if err == nil {
		t.Fatal("a downgrade must fail the pipeline, not be reconciled")
	}
	if !errors.Is(err, upgrade.ErrVersionDowngrade) {
		t.Fatalf("pipeline error = %v, want a version-downgrade refusal", err)
	}
	if stsExists(t, c, n) {
		t.Error("the workload step created a StatefulSet despite the refusal — Preflight must run " +
			"before the pipeline, not inside it, or the pod template moves before anything is checked")
	}
}

// The refusal has to be one the user can see and act on, not a bare error in the log.
func TestRefusedVersionChangeIsReportedAsACataloguedReason(t *testing.T) {
	s := upgradeScheme(t)
	n := refusedCR()
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).WithStatusSubresource(n).Build()
	w := status.NewWriter(c)

	_, err := pipelineFor(c, s).runPipeline(t.Context(), n)
	if err == nil {
		t.Fatal("expected the downgrade to be refused")
	}

	w.MarkPipelineError(n, err)
	var found *metav1.Condition
	for i := range n.Status.Conditions {
		if n.Status.Conditions[i].Type == "Error" {
			found = &n.Status.Conditions[i]
		}
	}
	if found == nil {
		t.Fatal("no Error condition after a refused version change")
	}
	if found.Reason != "VersionDowngradeRefused" {
		t.Errorf("Error reason = %q, want VersionDowngradeRefused", found.Reason)
	}
	if found.Message == "" {
		t.Error("the refusal must carry a message telling the user what to do")
	}
}

// A version change the operator accepts must reach the pipeline, or the guard would be a wall.
func TestAcceptedVersionChangeRunsThePipeline(t *testing.T) {
	s := upgradeScheme(t)
	n := refusedCR()
	n.Spec.Version = "2026.07.1" // forward, and no LTS checkpoint in between
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(n).WithStatusSubresource(n).Build()

	if _, err := pipelineFor(c, s).runPipeline(t.Context(), n); err != nil {
		t.Fatalf("a supported upgrade must not be refused: %v", err)
	}
	if !stsExists(t, c, n) {
		t.Error("no StatefulSet after an accepted version change — the workload step never ran")
	}
}
