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

func TestReconcileFailedJobSurfacesPodMessage(t *testing.T) {
	s := scheme(t)
	backup := backupCR()
	job, err := renderbackup.BackupJob(enterpriseNeo4j(), backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := controllerutil.SetControllerReference(backup, job, s); err != nil {
		t.Fatal(err)
	}
	// Job condition carries only the generic controller message.
	job.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
		Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit",
	}}
	// The failed pod carries the real neo4j-admin cause in its termination message.
	realMsg := "Differential backups require that a full backup of the same database exists"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-abc", Namespace: "ns", Labels: map[string]string{"job-name": job.Name}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "neo4j-admin",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Message: realMsg, FinishedAt: metav1.Now()}},
		}}},
	}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(enterpriseNeo4j(), backup, job, pod).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackup{}).Build()
	r := &BackupReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16)}

	if _, err := r.Reconcile(t.Context(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getBackup(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", got.Status.Phase)
	}
	if got.Status.Message != realMsg {
		t.Errorf("status.message = %q, want the pod's neo4j-admin cause %q", got.Status.Message, realMsg)
	}
}

func TestReconcilePVCBackupRecordsArtifactPath(t *testing.T) {
	s := scheme(t)
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"neo4j"},
			Destination: neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationPVC, PVC: &neo4jv1beta1.BackupPVC{ClaimName: "bk"}},
			Type:        neo4jv1beta1.BackupTypeFull,
		},
	}
	job, err := renderbackup.BackupJob(enterpriseNeo4j(), backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := controllerutil.SetControllerReference(backup, job, s); err != nil {
		t.Fatal(err)
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	// The Job's pod recorded the real artifact filename and size on success (/dev/termination-log).
	realName := "neo4j-2026-09-01T15-08-49.backup"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-xyz", Namespace: "ns", Labels: map[string]string{"job-name": job.Name}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "neo4j-admin",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: "neo4j=" + realName + "|4096\n", FinishedAt: metav1.Now()}},
		}}},
	}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(enterpriseNeo4j(), backup, job, pod).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackup{}).Build()
	r := &BackupReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16)}

	if _, err := r.Reconcile(t.Context(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getBackup(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		t.Fatalf("phase = %q, want Succeeded", got.Status.Phase)
	}
	if len(got.Status.Artifacts) != 1 || got.Status.Artifacts[0].Path != realName {
		t.Errorf("artifact Path = %+v, want the real filename %q", got.Status.Artifacts, realName)
	}
	if got.Status.Artifacts[0].SizeBytes != 4096 {
		t.Errorf("artifact SizeBytes = %d, want 4096 (parsed from the recorded |bytes suffix)", got.Status.Artifacts[0].SizeBytes)
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
