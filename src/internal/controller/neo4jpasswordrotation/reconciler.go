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

// Package neo4jpasswordrotation reconciles Neo4jPasswordRotation records (BDR-017 / ADR-019).
// The rotation owner stages the new admin password in its own Secret and leaves the instance's
// live auth Secret (the old value) untouched. The operator authenticates with the still-live old
// value, runs ALTER USER neo4j SET PASSWORD over the system database, then writes the new value
// into the live auth Secret. The old value therefore survives until the ALTER lands — no
// persistent shadow Secret. The run is immutable and roll-forward only: on a write-back failure
// after a successful ALTER the database already holds the new value, so a retry dials with the new
// credential, sees the change is applied, and finishes the write-back (no rollback).
package neo4jpasswordrotation

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/formation"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

// RotationReconciler drives a Neo4jPasswordRotation to Succeeded/Failed over admin Bolt.
type RotationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// Connect dials admin Bolt with explicit credentials; nil → formation.DialWith (tests inject a fake).
	Connect func(ctx context.Context, neo4j *neo4jv1.Neo4j, user, pass string) (intneo4j.Admin, error)
}

func NewReconciler(mgr ctrl.Manager) *RotationReconciler {
	return &RotationReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("neo4jpasswordrotation-controller"),
	}
}

// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jpasswordrotations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jpasswordrotations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4jpasswordrotations/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.com,resources=neo4js,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;update;delete

func (r *RotationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var rot neo4jv1.Neo4jPasswordRotation
	if err := r.Get(ctx, req.NamespacedName, &rot); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Immutable record: a terminal run never rotates again (GitOps re-apply safe).
	if rot.Status.Phase == neo4jv1.RunPhaseSucceeded || rot.Status.Phase == neo4jv1.RunPhaseFailed {
		return ctrl.Result{}, nil
	}

	var neo4j neo4jv1.Neo4j
	if err := r.Get(ctx, types.NamespacedName{Name: rot.Spec.Neo4jRef.Name, Namespace: rot.Namespace}, &neo4j); err != nil {
		if apierrors.IsNotFound(err) {
			return r.retryable(ctx, &rot, oracle.ReasonRotationTargetNotFound,
				"waiting for Neo4j "+rot.Spec.Neo4jRef.Name+" to exist")
		}
		return ctrl.Result{}, err
	}

	// Gate on the instance's own Ready: a live leader must answer before we open an admin session —
	// the same conservative gate restore uses. One exception keeps recovery homogeneous across
	// topologies: once the ALTER has landed and only the Secret write-back is outstanding
	// (RotationSecretWriteBackFailed), do NOT gate. In Cluster the stale live Secret is itself what
	// holds Ready down — the operator's formation loop dials that Secret every pass — so gating here
	// would deadlock the very write-back that repairs it. Standalone never hits that (formation skips
	// it); bypassing the gate only while recovering lets both converge the same way.
	recovering := rot.Status.Reason == oracle.ReasonRotationSecretWriteBackFailed.String()
	if !recovering && !meta.IsStatusConditionTrue(neo4j.Status.Conditions, oracle.ConditionReady.String()) {
		return r.retryable(ctx, &rot, oracle.ReasonRotationBeforeReady,
			"waiting for Neo4j "+neo4j.Name+" to be Ready before opening an admin session")
	}

	// Old credential = the instance's live auth Secret. "none" means auth is disabled: nothing to do.
	oldUser, oldPass, disabled, reason, msg := r.readLiveAuth(ctx, &neo4j)
	if reason != nil {
		return r.fail(ctx, &rot, *reason, msg)
	}
	if disabled {
		return r.succeed(ctx, &rot, oracle.ReasonRotationAuthDisabled,
			"authentication is disabled on the target (NEO4J_AUTH: none); nothing to rotate")
	}

	// New credential = the staged Secret named by the spec. Same admin user as the live Secret.
	newPass, reason, msg := r.readStagedAuth(ctx, &rot, &neo4j, oldUser)
	if reason != nil {
		return r.fail(ctx, &rot, *reason, msg)
	}

	connect := r.Connect
	if connect == nil {
		connect = func(ctx context.Context, n *neo4jv1.Neo4j, user, pass string) (intneo4j.Admin, error) {
			return formation.DialWith(ctx, r.Client, r.Recorder, n, user, pass)
		}
	}

	// Try the old (still-live) credential. If it fails, the ALTER may already have landed on a
	// previous pass: try the new credential and, if it works, just finish the write-back.
	admin, err := connect(ctx, &neo4j, oldUser, oldPass)
	if err != nil {
		admin, err2 := connect(ctx, &neo4j, oldUser, newPass)
		if err2 != nil {
			return r.retryable(ctx, &rot, oracle.ReasonRotationAuthFailed,
				"old credential failed ("+err.Error()+") and new credential failed ("+err2.Error()+")")
		}
		_ = admin.Close(ctx)
		if werr := r.writeLiveAuth(ctx, &neo4j, oldUser, newPass); werr != nil {
			return r.retryable(ctx, &rot, oracle.ReasonRotationSecretWriteBackFailed, werr.Error())
		}
		r.maybeDeleteStaged(ctx, &rot)
		return r.succeed(ctx, &rot, oracle.ReasonRotationSucceeded,
			"password already applied in the database; live auth Secret synced")
	}
	defer func() { _ = admin.Close(ctx) }()

	if err := admin.AlterUserPassword(ctx, oldUser, newPass); err != nil {
		return r.fail(ctx, &rot, oracle.ReasonRotationAlterFailed, err.Error())
	}
	// ALTER landed. From here the database holds the new value; a write-back failure diverges the
	// live Secret (old) from the database (new), and the next pass converges via the dial-new path.
	if err := r.writeLiveAuth(ctx, &neo4j, oldUser, newPass); err != nil {
		return r.retryable(ctx, &rot, oracle.ReasonRotationSecretWriteBackFailed, err.Error())
	}
	r.maybeDeleteStaged(ctx, &rot)
	return r.succeed(ctx, &rot, oracle.ReasonRotationSucceeded,
		"admin password rotated and live auth Secret synced")
}

// readLiveAuth reads the instance's live auth Secret (the OLD credential). disabled is true when
// authentication is off (NEO4J_AUTH: none). A non-nil reason is terminal.
func (r *RotationReconciler) readLiveAuth(ctx context.Context, neo4j *neo4jv1.Neo4j) (user, pass string, disabled bool, reason *oracle.Reason, msg string) {
	ctxRender := render.ClientServiceContext(neo4j)
	var secret corev1.Secret
	key := types.NamespacedName{Name: ctxRender.AuthSecretName(), Namespace: ctxRender.Namespace()}
	if err := r.Get(ctx, key, &secret); err != nil {
		rr := oracle.ReasonRotationNewSecretInvalid
		return "", "", false, &rr, "live auth secret " + key.Name + ": " + err.Error()
	}
	if err := rendersecrets.RequireAuthSecretDelegated(&secret, neo4j); err != nil {
		rr := oracle.ReasonRotationAuthUndelegated
		return "", "", false, &rr, "live auth secret: " + err.Error()
	}
	raw := strings.TrimSpace(string(secret.Data["NEO4J_AUTH"]))
	if strings.EqualFold(raw, "none") {
		return "", "", true, nil, ""
	}
	user, pass, err := intneo4j.ParseAuthSecret(raw)
	if err != nil {
		rr := oracle.ReasonRotationNewSecretInvalid
		return "", "", false, &rr, "live auth secret: " + err.Error()
	}
	return user, pass, false, nil, ""
}

// readStagedAuth reads the staged Secret named by the spec (the NEW credential) and checks it
// carries the same admin user as the live Secret. A non-nil reason is terminal.
func (r *RotationReconciler) readStagedAuth(ctx context.Context, rot *neo4jv1.Neo4jPasswordRotation, neo4j *neo4jv1.Neo4j, wantUser string) (pass string, reason *oracle.Reason, msg string) {
	var secret corev1.Secret
	key := types.NamespacedName{Name: rot.Spec.NewPasswordSecretRef.Name, Namespace: rot.Namespace}
	if err := r.Get(ctx, key, &secret); err != nil {
		rr := oracle.ReasonRotationNewSecretInvalid
		return "", &rr, "staged secret " + key.Name + ": " + err.Error()
	}
	if err := rendersecrets.RequireAuthSecretDelegated(&secret, neo4j); err != nil {
		rr := oracle.ReasonRotationAuthUndelegated
		return "", &rr, "staged secret: " + err.Error()
	}
	user, pass, err := intneo4j.ParseAuthSecret(string(secret.Data["NEO4J_AUTH"]))
	if err != nil {
		rr := oracle.ReasonRotationNewSecretInvalid
		return "", &rr, "staged secret: " + err.Error()
	}
	if user != wantUser {
		rr := oracle.ReasonRotationNewSecretInvalid
		return "", &rr, "staged secret user " + user + " does not match the live admin user " + wantUser + " (rotation changes the password, not the user)"
	}
	return pass, nil, ""
}

// writeLiveAuth writes the new credential into the instance's live auth Secret. The auth Secret is
// in no StatefulSet checksum, so this does not roll the pods (BDR-017).
func (r *RotationReconciler) writeLiveAuth(ctx context.Context, neo4j *neo4jv1.Neo4j, user, pass string) error {
	ctxRender := render.ClientServiceContext(neo4j)
	var secret corev1.Secret
	key := types.NamespacedName{Name: ctxRender.AuthSecretName(), Namespace: ctxRender.Namespace()}
	if err := r.Get(ctx, key, &secret); err != nil {
		return err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data["NEO4J_AUTH"] = []byte(user + "/" + pass)
	return r.Update(ctx, &secret)
}

// maybeDeleteStaged deletes the staged Secret when the spec opts in. Best-effort: a failure here
// does not fail an otherwise-successful rotation (the owner can still remove it).
func (r *RotationReconciler) maybeDeleteStaged(ctx context.Context, rot *neo4jv1.Neo4jPasswordRotation) {
	if !rot.Spec.DeleteNewSecretOnSuccess {
		return
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: rot.Spec.NewPasswordSecretRef.Name, Namespace: rot.Namespace}}
	if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		ctrllog.FromContext(ctx).Error(err, "failed to delete staged rotation secret", "secret", secret.Name)
	}
}

func (r *RotationReconciler) succeed(ctx context.Context, rot *neo4jv1.Neo4jPasswordRotation, reason oracle.Reason, msg string) (ctrl.Result, error) {
	rot.Status.Phase = neo4jv1.RunPhaseSucceeded
	rot.Status.Reason = reason.String()
	rot.Status.Message = msg
	setCondition(rot, oracle.ConditionRotationReady, metav1.ConditionTrue, reason, msg)
	if r.Recorder != nil {
		r.Recorder.Event(rot, corev1.EventTypeNormal, reason.String(), msg)
	}
	return ctrl.Result{}, r.writeStatus(ctx, rot)
}

func (r *RotationReconciler) fail(ctx context.Context, rot *neo4jv1.Neo4jPasswordRotation, reason oracle.Reason, msg string) (ctrl.Result, error) {
	rot.Status.Phase = neo4jv1.RunPhaseFailed
	rot.Status.Reason = reason.String()
	rot.Status.Message = msg
	setCondition(rot, oracle.ConditionRotationReady, metav1.ConditionFalse, reason, msg)
	if r.Recorder != nil {
		r.Recorder.Event(rot, corev1.EventTypeWarning, reason.String(), msg)
	}
	return ctrl.Result{}, r.writeStatus(ctx, rot)
}

func (r *RotationReconciler) retryable(ctx context.Context, rot *neo4jv1.Neo4jPasswordRotation, reason oracle.Reason, msg string) (ctrl.Result, error) {
	if rot.Status.Phase == "" {
		rot.Status.Phase = neo4jv1.RunPhasePending
	}
	rot.Status.Reason = reason.String()
	rot.Status.Message = msg
	setCondition(rot, oracle.ConditionRotationReady, metav1.ConditionFalse, reason, msg)
	if err := r.writeStatus(ctx, rot); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *RotationReconciler) writeStatus(ctx context.Context, rot *neo4jv1.Neo4jPasswordRotation) error {
	rot.Status.ObservedGeneration = rot.Generation
	if err := r.Status().Update(ctx, rot); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

func setCondition(rot *neo4jv1.Neo4jPasswordRotation, ctype oracle.Condition, status metav1.ConditionStatus, reason oracle.Reason, message string) {
	meta.SetStatusCondition(&rot.Status.Conditions, metav1.Condition{
		Type:               ctype.String(),
		Status:             status,
		Reason:             reason.String(),
		Message:            message,
		ObservedGeneration: rot.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *RotationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1.Neo4jPasswordRotation{}).
		Complete(r)
}
