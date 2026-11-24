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

package neo4jpasswordrotation

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

// fakeAdmin records the password set by ALTER and can be made to fail it.
type fakeAdmin struct {
	altered []string
	altErr  error
}

func (f *fakeAdmin) AlterUserPassword(_ context.Context, _, pass string) error {
	if f.altErr != nil {
		return f.altErr
	}
	f.altered = append(f.altered, pass)
	return nil
}
func (f *fakeAdmin) Close(context.Context) error { return nil }

// Rest of the intneo4j.Admin surface — unused here, present to satisfy the interface.
func (f *fakeAdmin) ShowServers(context.Context) ([]intneo4j.Server, error) { return nil, nil }
func (f *fakeAdmin) ShowDatabaseTopologies(context.Context) ([]intneo4j.DatabaseTopology, error) {
	return nil, nil
}
func (f *fakeAdmin) ShowDatabases(context.Context) ([]intneo4j.DatabaseState, error) { return nil, nil }
func (f *fakeAdmin) SetDatabaseTopology(context.Context, string, int64, int64) error { return nil }
func (f *fakeAdmin) SetDefaultAllocationNumbers(context.Context, int64, int64) error { return nil }
func (f *fakeAdmin) EnableServer(context.Context, string, string) error              { return nil }
func (f *fakeAdmin) DeallocateDatabases(context.Context, string) error               { return nil }
func (f *fakeAdmin) DropServer(context.Context, string) error                        { return nil }
func (f *fakeAdmin) ClusterStable(context.Context) (bool, string, error)             { return true, "", nil }
func (f *fakeAdmin) CreateDatabaseWithSeed(context.Context, string, string, int64, int64) error {
	return nil
}
func (f *fakeAdmin) CreateOrReplaceDatabaseWithSeed(context.Context, string, string, int64, int64) error {
	return nil
}
func (f *fakeAdmin) StopDatabase(context.Context, string) error               { return nil }
func (f *fakeAdmin) StartDatabase(context.Context, string) error              { return nil }
func (f *fakeAdmin) CreateDatabaseIfNotExists(context.Context, string) error  { return nil }
func (f *fakeAdmin) CreateUserIfNotExists(context.Context, string, string) error { return nil }
func (f *fakeAdmin) GrantRole(context.Context, string, string) error          { return nil }

func scheme(t *testing.T) *runtime.Scheme {
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

func readyNeo4j() *neo4jv1.Neo4j {
	n := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "ns"},
		Spec: neo4jv1.Neo4jSpec{
			Edition:  neo4jv1.EditionEnterprise,
			Version:  "2025.01.0",
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
		},
	}
	meta.SetStatusCondition(&n.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllMembersReady", Message: "ready",
	})
	return n
}

// authSecret builds a delegated auth Secret (NEO-005) carrying NEO4J_AUTH=value.
func authSecret(name, value string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "ns",
			Labels: map[string]string{rendersecrets.AllowedForLabel: "g"},
		},
		Data: map[string][]byte{"NEO4J_AUTH": []byte(value)},
	}
}

func rotationCR(mut func(*neo4jv1.Neo4jPasswordRotation)) *neo4jv1.Neo4jPasswordRotation {
	r := &neo4jv1.Neo4jPasswordRotation{
		ObjectMeta: metav1.ObjectMeta{Name: "rot", Namespace: "ns"},
		Spec: neo4jv1.Neo4jPasswordRotationSpec{
			Neo4jRef:             neo4jv1.Neo4jRef{Name: "g"},
			NewPasswordSecretRef: neo4jv1.PasswordSecretRef{Name: "staged"},
		},
	}
	if mut != nil {
		mut(r)
	}
	return r
}

// newReconciler wires a fake client and a Connect seam that accepts only the listed passwords
// (modelling which credential the database currently honours).
func newReconciler(t *testing.T, admin intneo4j.Admin, accept []string, objs ...client.Object) (*RotationReconciler, client.Client) {
	t.Helper()
	s := scheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).
		WithStatusSubresource(&neo4jv1.Neo4jPasswordRotation{}).Build()
	ok := map[string]bool{}
	for _, p := range accept {
		ok[p] = true
	}
	r := &RotationReconciler{
		Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16),
		Connect: func(_ context.Context, _ *neo4jv1.Neo4j, _, pass string) (intneo4j.Admin, error) {
			if !ok[pass] {
				return nil, fmt.Errorf("authentication failure")
			}
			return admin, nil
		},
	}
	return r, c
}

func req() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "rot", Namespace: "ns"}}
}

func getRotation(t *testing.T, c client.Client) *neo4jv1.Neo4jPasswordRotation {
	t.Helper()
	var r neo4jv1.Neo4jPasswordRotation
	if err := c.Get(context.Background(), req().NamespacedName, &r); err != nil {
		t.Fatalf("get rotation: %v", err)
	}
	return &r
}

func liveAuth(t *testing.T, c client.Client) string {
	t.Helper()
	var s corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "g-auth", Namespace: "ns"}, &s); err != nil {
		t.Fatalf("get live auth secret: %v", err)
	}
	return string(s.Data["NEO4J_AUTH"])
}

// Nominal path: database honours the old password; operator ALTERs to new, then syncs the Secret.
func TestRotationAltersThenWritesBack(t *testing.T) {
	admin := &fakeAdmin{}
	r, c := newReconciler(t, admin, []string{"old"},
		readyNeo4j(), authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"), rotationCR(nil))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(admin.altered) != 1 || admin.altered[0] != "new" {
		t.Errorf("expected ALTER to new; altered=%v", admin.altered)
	}
	if got := liveAuth(t, c); got != "neo4j/new" {
		t.Errorf("live auth = %q, want neo4j/new", got)
	}
	if p := getRotation(t, c).Status.Phase; p != neo4jv1.RunPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", p)
	}
}

// Crash recovery: a prior pass already ALTERed (old fails, new works). The operator must not
// re-ALTER; it just finishes the write-back and Succeeds (roll-forward, no rollback).
func TestRotationRecoversWhenOldAlreadyChanged(t *testing.T) {
	admin := &fakeAdmin{}
	r, c := newReconciler(t, admin, []string{"new"},
		readyNeo4j(), authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"), rotationCR(nil))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(admin.altered) != 0 {
		t.Errorf("already applied: should not ALTER again; altered=%v", admin.altered)
	}
	if got := liveAuth(t, c); got != "neo4j/new" {
		t.Errorf("live auth = %q, want neo4j/new", got)
	}
	if p := getRotation(t, c).Status.Phase; p != neo4jv1.RunPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", p)
	}
}

// Recovery is homogeneous across topologies: when the ALTER already landed and only the Secret
// write-back is outstanding (RotationSecretWriteBackFailed), the Ready gate is bypassed — otherwise
// a Cluster whose Ready dropped because the live Secret is stale would deadlock the write-back that
// fixes it. Target NOT Ready + old fails + new works ⇒ write-back still completes and Succeeds.
func TestRotationRecoveryBypassesReadyGate(t *testing.T) {
	n := readyNeo4j()
	meta.SetStatusCondition(&n.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: metav1.ConditionFalse, Reason: "ClusterFormed", Message: "stale live auth secret",
	})
	admin := &fakeAdmin{}
	r, c := newReconciler(t, admin, []string{"new"}, // DB already honours the new password
		n, authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"),
		rotationCR(func(rot *neo4jv1.Neo4jPasswordRotation) {
			rot.Status.Phase = neo4jv1.RunPhasePending
			rot.Status.Reason = "RotationSecretWriteBackFailed"
		}))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(admin.altered) != 0 {
		t.Errorf("already applied: should not ALTER again; altered=%v", admin.altered)
	}
	if got := liveAuth(t, c); got != "neo4j/new" {
		t.Errorf("recovery did not sync live auth under NotReady: got %q want neo4j/new", got)
	}
	if got := getRotation(t, c); got.Status.Phase != neo4jv1.RunPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", got.Status.Phase)
	}
}

// A FRESH rotation (no prior write-back failure) still waits for Ready — the bypass is recovery-only.
// Neither credential authenticates (changed out of band): retry, do not fail terminally.
func TestRotationBothCredentialsFailRetries(t *testing.T) {
	admin := &fakeAdmin{}
	r, c := newReconciler(t, admin, nil,
		readyNeo4j(), authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"), rotationCR(nil))

	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue when both credentials fail")
	}
	got := getRotation(t, c)
	if got.Status.Phase != neo4jv1.RunPhasePending {
		t.Errorf("phase = %q, want Pending", got.Status.Phase)
	}
	if got.Status.Reason != "RotationAuthFailed" {
		t.Errorf("reason = %q, want RotationAuthFailed", got.Status.Reason)
	}
}

// ALTER fails terminally: Failed, and the live Secret is left on the old value.
func TestRotationAlterFailureIsTerminal(t *testing.T) {
	admin := &fakeAdmin{altErr: fmt.Errorf("permission denied")}
	r, c := newReconciler(t, admin, []string{"old"},
		readyNeo4j(), authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"), rotationCR(nil))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getRotation(t, c)
	if got.Status.Phase != neo4jv1.RunPhaseFailed {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	if got.Status.Reason != "RotationAlterFailed" {
		t.Errorf("reason = %q, want RotationAlterFailed", got.Status.Reason)
	}
	if got := liveAuth(t, c); got != "neo4j/old" {
		t.Errorf("live auth = %q, want unchanged neo4j/old", got)
	}
}

// Target not Ready: wait, do not dial.
func TestRotationWaitsForReady(t *testing.T) {
	n := readyNeo4j()
	meta.SetStatusCondition(&n.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: metav1.ConditionFalse, Reason: "Forming", Message: "forming",
	})
	r, c := newReconciler(t, &fakeAdmin{}, []string{"old"},
		n, authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"), rotationCR(nil))

	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue while target is not Ready")
	}
	if got := getRotation(t, c).Status.Reason; got != "RotationBeforeReady" {
		t.Errorf("reason = %q, want RotationBeforeReady", got)
	}
}

// Auth disabled on the target: nothing to rotate, Succeed as a no-op.
func TestRotationNoOpWhenAuthDisabled(t *testing.T) {
	admin := &fakeAdmin{}
	r, c := newReconciler(t, admin, []string{"old"},
		readyNeo4j(), authSecret("g-auth", "none"), authSecret("staged", "neo4j/new"), rotationCR(nil))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(admin.altered) != 0 {
		t.Errorf("auth disabled: nothing to ALTER; altered=%v", admin.altered)
	}
	got := getRotation(t, c)
	if got.Status.Phase != neo4jv1.RunPhaseSucceeded || got.Status.Reason != "RotationAuthDisabled" {
		t.Errorf("phase/reason = %q/%q, want Succeeded/RotationAuthDisabled", got.Status.Phase, got.Status.Reason)
	}
}

// Undelegated staged Secret is refused terminally (NEO-005).
func TestRotationRefusesUndelegatedStagedSecret(t *testing.T) {
	staged := authSecret("staged", "neo4j/new")
	staged.Labels = nil // strip delegation
	r, c := newReconciler(t, &fakeAdmin{}, []string{"old"},
		readyNeo4j(), authSecret("g-auth", "neo4j/old"), staged, rotationCR(nil))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getRotation(t, c)
	if got.Status.Phase != neo4jv1.RunPhaseFailed || got.Status.Reason != "RotationAuthUndelegated" {
		t.Errorf("phase/reason = %q/%q, want Failed/RotationAuthUndelegated", got.Status.Phase, got.Status.Reason)
	}
}

// DeleteNewSecretOnSuccess removes the staged Secret once the rotation Succeeds.
func TestRotationDeletesStagedSecretOnSuccess(t *testing.T) {
	admin := &fakeAdmin{}
	r, c := newReconciler(t, admin, []string{"old"},
		readyNeo4j(), authSecret("g-auth", "neo4j/old"), authSecret("staged", "neo4j/new"),
		rotationCR(func(r *neo4jv1.Neo4jPasswordRotation) { r.Spec.DeleteNewSecretOnSuccess = true }))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var s corev1.Secret
	err := c.Get(context.Background(), types.NamespacedName{Name: "staged", Namespace: "ns"}, &s)
	if !apierrors.IsNotFound(err) {
		t.Errorf("staged secret should be deleted; get err = %v", err)
	}
}
