package neo4jrestore

import (
	"context"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	renderbackup "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/backup"
)

// fakeAdmin records restore verbs and answers SHOW DATABASES from an in-memory map.
type fakeAdmin struct {
	online       map[string]bool // db name -> online (existence = key present)
	created      []string
	replaced     []string
	stopped      []string
	seededWith   map[string]string
	onCreateFail error
}

func newFakeAdmin(existing map[string]bool) *fakeAdmin {
	if existing == nil {
		existing = map[string]bool{}
	}
	return &fakeAdmin{online: existing, seededWith: map[string]string{}}
}

func (f *fakeAdmin) ShowDatabases(context.Context) ([]intneo4j.DatabaseState, error) {
	out := make([]intneo4j.DatabaseState, 0, len(f.online))
	for n, on := range f.online {
		out = append(out, intneo4j.DatabaseState{Name: n, Online: on})
	}
	return out, nil
}

func (f *fakeAdmin) CreateDatabaseWithSeed(_ context.Context, name, seedURI string, _, _ int64) error {
	if f.onCreateFail != nil {
		return f.onCreateFail
	}
	f.created = append(f.created, name)
	f.seededWith[name] = seedURI
	f.online[name] = true
	return nil
}

func (f *fakeAdmin) CreateOrReplaceDatabaseWithSeed(_ context.Context, name, seedURI string, _, _ int64) error {
	if f.onCreateFail != nil {
		return f.onCreateFail
	}
	f.replaced = append(f.replaced, name)
	f.seededWith[name] = seedURI
	f.online[name] = true
	return nil
}

func (f *fakeAdmin) StopDatabase(_ context.Context, name string) error {
	f.stopped = append(f.stopped, name)
	f.online[name] = false
	return nil
}

func (f *fakeAdmin) StartDatabase(_ context.Context, name string) error {
	f.online[name] = true
	return nil
}
func (f *fakeAdmin) Close(context.Context) error { return nil }

// Formation surface — unused here; present to satisfy intneo4j.Admin.
func (f *fakeAdmin) ShowServers(context.Context) ([]intneo4j.Server, error) { return nil, nil }
func (f *fakeAdmin) ShowDatabaseTopologies(context.Context) ([]intneo4j.DatabaseTopology, error) {
	return nil, nil
}
func (f *fakeAdmin) SetDatabaseTopology(context.Context, string, int64, int64) error { return nil }
func (f *fakeAdmin) SetDefaultAllocationNumbers(context.Context, int64, int64) error { return nil }
func (f *fakeAdmin) EnableServer(context.Context, string, string) error              { return nil }
func (f *fakeAdmin) DeallocateDatabases(context.Context, string) error               { return nil }
func (f *fakeAdmin) DropServer(context.Context, string) error                        { return nil }

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

func readyNeo4j() *neo4jv1beta1.Neo4j {
	n := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2025.01.0",
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	meta.SetStatusCondition(&n.Status.Conditions, metav1.Condition{
		Type: oracle.ConditionReady.String(), Status: metav1.ConditionTrue,
		Reason: "AllMembersReady", Message: "ready",
	})
	return n
}

func restoreCR(mut func(*neo4jv1beta1.Neo4jRestore)) *neo4jv1beta1.Neo4jRestore {
	r := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "nr", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Neo4jRef:  neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases: []string{"neo4j"},
			Source:    neo4jv1beta1.RestoreSource{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://b/p/neo4j"},
		},
	}
	if mut != nil {
		mut(r)
	}
	return r
}

func newReconciler(t *testing.T, admin intneo4j.Admin, objs ...client.Object) (*RestoreReconciler, client.Client) {
	t.Helper()
	s := scheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jRestore{}).Build()
	r := &RestoreReconciler{
		Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16),
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) { return admin, nil },
	}
	return r, c
}

func req() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "nr", Namespace: "ns"}}
}

func getRestore(t *testing.T, c client.Client) *neo4jv1beta1.Neo4jRestore {
	t.Helper()
	var r neo4jv1beta1.Neo4jRestore
	if err := c.Get(context.Background(), req().NamespacedName, &r); err != nil {
		t.Fatalf("get restore: %v", err)
	}
	return &r
}

func TestRestoreSeedsFreshDatabaseThenSucceeds(t *testing.T) {
	admin := newFakeAdmin(nil) // no databases exist
	r, c := newReconciler(t, admin, readyNeo4j(), restoreCR(nil))

	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue while databases seed")
	}
	if len(admin.created) != 1 || admin.created[0] != "neo4j" {
		t.Errorf("expected CREATE DATABASE neo4j; created=%v", admin.created)
	}
	if got := admin.seededWith["neo4j"]; got != "s3://b/p/neo4j" {
		t.Errorf("seedURI = %q, want s3://b/p/neo4j", got)
	}
	if p := getRestore(t, c).Status.Phase; p != neo4jv1beta1.RunPhaseRunning {
		t.Errorf("phase = %q, want Running", p)
	}

	// Second pass polls; fake marks the db online at create, so it completes.
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if p := getRestore(t, c).Status.Phase; p != neo4jv1beta1.RunPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", p)
	}
}

func TestRestoreExistingWithoutOverwriteFails(t *testing.T) {
	admin := newFakeAdmin(map[string]bool{"neo4j": true})
	r, c := newReconciler(t, admin, readyNeo4j(), restoreCR(nil))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getRestore(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	if got.Status.Reason != "RestoreDatabaseExists" {
		t.Errorf("reason = %q, want RestoreDatabaseExists", got.Status.Reason)
	}
	if len(admin.created)+len(admin.replaced) != 0 {
		t.Error("nothing should be seeded when overwrite is refused")
	}
}

func TestRestoreOverwriteReplaces(t *testing.T) {
	admin := newFakeAdmin(map[string]bool{"neo4j": true})
	r, _ := newReconciler(t, admin, readyNeo4j(), restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
		r.Spec.Overwrite = true
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(admin.replaced) != 1 || admin.replaced[0] != "neo4j" {
		t.Errorf("expected CREATE OR REPLACE neo4j; replaced=%v", admin.replaced)
	}
	if len(admin.stopped) != 0 {
		t.Error("no forceOffline: should not STOP")
	}
}

func TestRestoreForceOfflineStopsBeforeReplace(t *testing.T) {
	admin := newFakeAdmin(map[string]bool{"neo4j": true})
	r, _ := newReconciler(t, admin, readyNeo4j(), restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
		r.Spec.Overwrite = true
		r.Spec.ForceOffline = true
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(admin.stopped) != 1 || admin.stopped[0] != "neo4j" {
		t.Errorf("expected STOP neo4j before replace; stopped=%v", admin.stopped)
	}
	if len(admin.replaced) != 1 {
		t.Errorf("expected replace after stop; replaced=%v", admin.replaced)
	}
}

func TestRestoreNotReadyIsRetryable(t *testing.T) {
	n := readyNeo4j()
	n.Status.Conditions = nil // not Ready
	r, c := newReconciler(t, newFakeAdmin(nil), n, restoreCR(nil))
	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue while target not Ready")
	}
	got := getRestore(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhasePending {
		t.Errorf("phase = %q, want Pending", got.Status.Phase)
	}
	if got.Status.Reason != "RestoreBeforeFormation" {
		t.Errorf("reason = %q, want RestoreBeforeFormation", got.Status.Reason)
	}
}

func TestRestoreCommunityFailsTerminally(t *testing.T) {
	n := readyNeo4j()
	n.Spec.Edition = neo4jv1beta1.EditionCommunity
	r, c := newReconciler(t, newFakeAdmin(nil), n, restoreCR(nil))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getRestore(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	if got.Status.Reason != "RestoreEditionUnsupported" {
		t.Errorf("reason = %q, want RestoreEditionUnsupported", got.Status.Reason)
	}
}

func TestRestoreBackupRefResolvesArtifactURI(t *testing.T) {
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			Phase: neo4jv1beta1.RunPhaseSucceeded,
			Artifacts: []neo4jv1beta1.BackupArtifact{
				{Database: "neo4j", URI: "s3://b/p/"},
			},
		},
	}
	admin := newFakeAdmin(nil)
	r, _ := newReconciler(t, admin, readyNeo4j(), backup, restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
		r.Spec.Source = neo4jv1beta1.RestoreSource{BackupRef: "nb"}
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := admin.seededWith["neo4j"]; got != "s3://b/p/" {
		t.Errorf("seedURI = %q, want s3://b/p/ (resolved from backupRef)", got)
	}
}

func backupsVolumeNeo4j(claim string) *neo4jv1beta1.Neo4j {
	n := readyNeo4j()
	n.Spec.Storage = &neo4jv1beta1.StorageSpec{
		Volumes: &neo4jv1beta1.VolumesSpec{
			Backups: &neo4jv1beta1.AuxiliaryVolumeSpec{
				Mode:     neo4jv1beta1.VolumeModeExisting,
				Existing: &neo4jv1beta1.ExistingVolumeSpec{ClaimName: claim},
			},
		},
	}
	return n
}

func pvcBackup(path string) *neo4jv1beta1.Neo4jBackup {
	return &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			Phase:     neo4jv1beta1.RunPhaseSucceeded,
			Artifacts: []neo4jv1beta1.BackupArtifact{{Database: "neo4j", URI: "pvc://bk", Path: path}},
		},
	}
}

func TestRestoreBackupRefPVCMountedSeedsFileURI(t *testing.T) {
	admin := newFakeAdmin(nil)
	r, _ := newReconciler(t, admin, backupsVolumeNeo4j("bk"), pvcBackup("neo4j.latest.backup"),
		restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
			r.Spec.Source = neo4jv1beta1.RestoreSource{BackupRef: "nb"}
		}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := admin.seededWith["neo4j"]; got != "file:/backups/neo4j.latest.backup" {
		t.Errorf("seedURI = %q, want file:/backups/neo4j.latest.backup", got)
	}
}

func TestRestoreBackupRefPVCNotMountedUnsupported(t *testing.T) {
	admin := newFakeAdmin(nil)
	// Target does NOT mount claim "bk" as its backups volume -> not server-readable.
	r, c := newReconciler(t, admin, readyNeo4j(), pvcBackup("neo4j.latest.backup"),
		restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
			r.Spec.Source = neo4jv1beta1.RestoreSource{BackupRef: "nb"}
		}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getRestore(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed || got.Status.Reason != "RestoreSourceUnsupported" {
		t.Errorf("want Failed/RestoreSourceUnsupported, got %q/%q", got.Status.Phase, got.Status.Reason)
	}
	if len(admin.created) != 0 {
		t.Error("must not seed when the backup PVC is not mounted on the target")
	}
}

func TestRestoreBackupRefPVCArtifactUnsupported(t *testing.T) {
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "nb", Namespace: "ns"},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			Phase:     neo4jv1beta1.RunPhaseSucceeded,
			Artifacts: []neo4jv1beta1.BackupArtifact{{Database: "neo4j", URI: "pvc://claim"}},
		},
	}
	admin := newFakeAdmin(nil)
	r, c := newReconciler(t, admin, readyNeo4j(), backup, restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
		r.Spec.Source = neo4jv1beta1.RestoreSource{BackupRef: "nb"}
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getRestore(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	if got.Status.Reason != "RestoreSourceUnsupported" {
		t.Errorf("reason = %q, want RestoreSourceUnsupported", got.Status.Reason)
	}
	if len(admin.created) != 0 {
		t.Error("pvc:// artifact must not be seeded in R1")
	}
}

// metadataJob builds the post-seed metadata Job controlled by restore, in the given terminal state
// (neither flag → still running).
func metadataJob(t *testing.T, restore *neo4jv1beta1.Neo4jRestore, complete, failed bool) *batchv1.Job {
	t.Helper()
	j := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: renderbackup.MetadataJobName(restore), Namespace: "ns"}}
	if err := controllerutil.SetControllerReference(restore, j, scheme(t)); err != nil {
		t.Fatal(err)
	}
	switch {
	case complete:
		j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	case failed:
		j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "backoff"}}
	}
	return j
}

func metadataPod(restore *neo4jv1beta1.Neo4jRestore, message string) *corev1.Pod {
	name := renderbackup.MetadataJobName(restore)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-x", Namespace: "ns", Labels: map[string]string{"job-name": name}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "neo4j-admin",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: message, FinishedAt: metav1.Now()}},
		}}},
	}
}

func metadataRestore() *neo4jv1beta1.Neo4jRestore {
	return restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
		r.Spec.Source = neo4jv1beta1.RestoreSource{BackupRef: "nb"}
		r.Spec.RestoreMetadata = true
	})
}

// sawEvent reports whether a Warning with the given reason substring was recorded.
func sawEvent(t *testing.T, r *RestoreReconciler, reason string) bool {
	t.Helper()
	fr, ok := r.Recorder.(*record.FakeRecorder)
	if !ok {
		t.Fatal("recorder is not a FakeRecorder")
	}
	for {
		select {
		case e := <-fr.Events:
			if strings.Contains(e, reason) {
				return true
			}
		default:
			return false
		}
	}
}

// drive runs Reconcile until the record is terminal or a step budget is exhausted (seed pass then
// poll/metadata passes). Returns the final record.
func drive(t *testing.T, r *RestoreReconciler, c client.Client) *neo4jv1beta1.Neo4jRestore {
	t.Helper()
	for i := 0; i < 5; i++ {
		if _, err := r.Reconcile(context.Background(), req()); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
		got := getRestore(t, c)
		if got.Status.Phase == neo4jv1beta1.RunPhaseSucceeded || got.Status.Phase == neo4jv1beta1.RunPhaseFailed {
			return got
		}
	}
	return getRestore(t, c)
}

func TestRestoreMetadataAppliesThenSucceeds(t *testing.T) {
	admin := newFakeAdmin(nil) // fresh db -> seed pass creates it online
	restore := metadataRestore()
	job := metadataJob(t, restore, true, false)
	pod := metadataPod(restore, "metadata-applied\n")
	r, c := newReconciler(t, admin, backupsVolumeNeo4j("bk"), pvcBackup("neo4j.latest.backup"), restore, job, pod)

	got := drive(t, r, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		t.Fatalf("phase = %q/%q, want Succeeded", got.Status.Phase, got.Status.Reason)
	}
	if sawEvent(t, r, "RestoreMetadataConflict") {
		t.Error("a clean metadata apply must not emit a conflict Warning")
	}
}

func TestRestoreMetadataConflictWarnsButSucceeds(t *testing.T) {
	admin := newFakeAdmin(nil)
	restore := metadataRestore()
	job := metadataJob(t, restore, true, false)
	pod := metadataPod(restore, "neo4j: role `reader` already exists\nmetadata-applied-with-warnings")
	r, c := newReconciler(t, admin, backupsVolumeNeo4j("bk"), pvcBackup("neo4j.latest.backup"), restore, job, pod)

	got := drive(t, r, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseSucceeded {
		t.Fatalf("phase = %q/%q, want Succeeded (conflicts warn, not fail)", got.Status.Phase, got.Status.Reason)
	}
	if !sawEvent(t, r, "RestoreMetadataConflict") {
		t.Error("expected a RestoreMetadataConflict Warning when statements were skipped")
	}
}

func TestRestoreMetadataJobFailedFails(t *testing.T) {
	admin := newFakeAdmin(nil)
	restore := metadataRestore()
	job := metadataJob(t, restore, false, true)
	pod := metadataPod(restore, "could not connect to system database")
	r, c := newReconciler(t, admin, backupsVolumeNeo4j("bk"), pvcBackup("neo4j.latest.backup"), restore, job, pod)

	got := drive(t, r, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed || got.Status.Reason != "RestoreMetadataFailed" {
		t.Fatalf("want Failed/RestoreMetadataFailed, got %q/%q", got.Status.Phase, got.Status.Reason)
	}
	if got.Status.Message != "could not connect to system database" {
		t.Errorf("message = %q, want the pod's failure cause", got.Status.Message)
	}
}

func TestRestoreMetadataJobRunningRequeues(t *testing.T) {
	admin := newFakeAdmin(nil)
	restore := metadataRestore()
	job := metadataJob(t, restore, false, false) // still running
	r, c := newReconciler(t, admin, backupsVolumeNeo4j("bk"), pvcBackup("neo4j.latest.backup"), restore, job)

	// seed pass, then poll reaches the metadata gate and waits on the Job.
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue while the metadata Job runs")
	}
	got := getRestore(t, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseRunning || got.Status.Reason != "RestoreMetadataApplying" {
		t.Errorf("want Running/RestoreMetadataApplying, got %q/%q", got.Status.Phase, got.Status.Reason)
	}
}

func TestRestoreMetadataRawURLUnsupported(t *testing.T) {
	admin := newFakeAdmin(nil)
	// A raw url source carries no metadata script; restoreMetadata is unsupported there.
	r, c := newReconciler(t, admin, readyNeo4j(), restoreCR(func(r *neo4jv1beta1.Neo4jRestore) {
		r.Spec.Source = neo4jv1beta1.RestoreSource{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://b/p/neo4j"}
		r.Spec.RestoreMetadata = true
	}))
	got := drive(t, r, c)
	if got.Status.Phase != neo4jv1beta1.RunPhaseFailed || got.Status.Reason != "RestoreMetadataFailed" {
		t.Errorf("want Failed/RestoreMetadataFailed, got %q/%q", got.Status.Phase, got.Status.Reason)
	}
}
