package neo4jbackup

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	renderbackup "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/backup"
)

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func enterpriseNeo4j() *neo4jv1beta1.Neo4j {
	port := int32(6362)
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:      neo4jv1beta1.EditionEnterprise,
			Version:      "2025.01.0",
			Topology:     neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{Listeners: &neo4jv1beta1.ConnectivityListenersSpec{Backup: &port}},
		},
	}
}

func backupCR() *neo4jv1beta1.Neo4jBackup {
	return &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"*"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://b/p/"},
			Type:        neo4jv1beta1.BackupTypeAuto,
		},
	}
}

func newReconciler(t *testing.T, objs ...client.Object) (*BackupReconciler, client.Client) {
	t.Helper()
	s := scheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackup{}).Build()
	return &BackupReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16)}, c
}

func req() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "nb", Namespace: "ns"}}
}

func getBackup(t *testing.T, c client.Client) *neo4jv1beta1.Neo4jBackup {
	t.Helper()
	var b neo4jv1beta1.Neo4jBackup
	if err := c.Get(t.Context(), req().NamespacedName, &b); err != nil {
		t.Fatalf("get backup: %v", err)
	}
	return &b
}

func TestReconcileCreatesJobAndRuns(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), backupCR())
	res, err := r.Reconcile(t.Context(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected a requeue while the Job runs")
	}
	var job batchv1.Job
	if err := c.Get(t.Context(), types.NamespacedName{Name: renderbackup.JobName(backupCR()), Namespace: "ns"}, &job); err != nil {
		t.Fatalf("expected Job created: %v", err)
	}
	if p := getBackup(t, c).Status.Phase; p != neo4jv1beta1.RunPhaseRunning {
		t.Errorf("phase = %q, want Running", p)
	}
}

func TestReconcileTargetNotFoundIsRetryable(t *testing.T) {
	r, c := newReconciler(t, backupCR()) // no Neo4j
	res, err := r.Reconcile(t.Context(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue when target missing")
	}
	b := getBackup(t, c)
	if b.Status.Phase != neo4jv1beta1.RunPhasePending {
		t.Errorf("phase = %q, want Pending", b.Status.Phase)
	}
	if b.Status.Reason != "BackupTargetNotFound" {
		t.Errorf("reason = %q, want BackupTargetNotFound", b.Status.Reason)
	}
}

func TestReconcileCommunityFailsTerminally(t *testing.T) {
	n := enterpriseNeo4j()
	n.Spec.Edition = neo4jv1beta1.EditionCommunity
	r, c := newReconciler(t, n, backupCR())
	if _, err := r.Reconcile(t.Context(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	b := getBackup(t, c)
	if b.Status.Phase != neo4jv1beta1.RunPhaseFailed {
		t.Errorf("phase = %q, want Failed", b.Status.Phase)
	}
	if b.Status.Reason != "BackupEditionUnsupported" {
		t.Errorf("reason = %q, want BackupEditionUnsupported", b.Status.Reason)
	}
	// A terminal record does not spawn a Job.
	var job batchv1.Job
	if err := c.Get(t.Context(), types.NamespacedName{Name: renderbackup.JobName(backupCR()), Namespace: "ns"}, &job); err == nil {
		t.Error("no Job should be created for a community target")
	}
}

func TestReconcileMirrorsJobCompletion(t *testing.T) {
	s := scheme(t)
	backup := backupCR()
	// Pre-create the owned Job in a Complete state so the reconciler mirrors Succeeded.
	job, err := renderbackup.BackupJob(enterpriseNeo4j(), backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := controllerutil.SetControllerReference(backup, job, s); err != nil {
		t.Fatal(err)
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(enterpriseNeo4j(), backup, job).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackup{}).Build()
	r := &BackupReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16)}

	if _, err := r.Reconcile(t.Context(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getBackup(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", got.Status.Phase)
	}
	if got.Status.Chain == "" {
		t.Error("expected status.chain to be set on success")
	}
	if len(got.Status.Artifacts) != 1 || got.Status.Artifacts[0].URI != "s3://b/p/" {
		t.Errorf("expected one artifact pointing at the destination; got %+v", got.Status.Artifacts)
	}
}
