/*
Copyright 2025.

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

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jUserFinalizer is the finalizer that ensures the controller gets a
// chance to drop the underlying user (or release the finalizer when policy
// is Retain) before the CR is removed.
const Neo4jUserFinalizer = "neo4j.com/user-finalizer"

// Neo4jUserReconciler reconciles a Neo4jUser resource.
type Neo4jUserReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
	Validator               *validation.UserValidator
}

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jusers/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jroles,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives a Neo4jUser resource toward its desired state.
func (r *Neo4jUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("neo4juser", req.NamespacedName)

	user := &neo4jv1beta1.Neo4jUser{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	username := effectiveUsername(user)
	requeue := r.requeueAfter()

	if user.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, user, username)
	}

	if !controllerutil.ContainsFinalizer(user, Neo4jUserFinalizer) {
		controllerutil.AddFinalizer(user, Neo4jUserFinalizer)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate
	if r.Validator != nil {
		res := r.Validator.Validate(ctx, user)
		for _, w := range res.Warnings {
			r.Recorder.Eventf(user, corev1.EventTypeWarning, EventReasonValidationWarning, "%s", w)
		}
		if len(res.Errors) > 0 {
			msg := fmt.Sprintf("validation failed: %s", res.Errors.ToAggregate().Error())
			r.setStatus(ctx, user, "Failed", metav1.ConditionFalse, EventReasonValidationFailed, msg, nil, "", nil)
			r.Recorder.Event(user, corev1.EventTypeWarning, EventReasonValidationFailed, msg)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
		if len(res.Pending) > 0 {
			// Transient dependency gaps (e.g. password Secret not applied
			// yet): Pending + requeue, like missing roles — apply order is
			// documented as irrelevant (#259).
			msg := strings.Join(res.Pending, "; ")
			r.setStatus(ctx, user, "Pending", metav1.ConditionFalse, "SecretPending", msg, nil, "", nil)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
	}

	// Resolve cluster
	target, err := ResolveClusterRef(ctx, r.Client, user.Namespace, user.Spec.ClusterRef)
	if err != nil {
		logger.Error(err, "failed to resolve clusterRef")
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		msg := fmt.Sprintf("clusterRef %q not found", user.Spec.ClusterRef)
		r.setStatus(ctx, user, "Pending", metav1.ConditionFalse, EventReasonClusterNotFound, msg, nil, "", nil)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	if !target.IsReady() {
		msg := fmt.Sprintf("clusterRef %q is not Ready", user.Spec.ClusterRef)
		r.setNamedCondition(ctx, user, ConditionTypeClusterNotReady, metav1.ConditionTrue, ConditionReasonClusterNotReady, msg)
		r.setStatus(ctx, user, "Pending", metav1.ConditionFalse, ConditionReasonClusterNotReady, msg, nil, "", nil)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, user, ConditionTypeClusterNotReady, metav1.ConditionFalse, "ClusterReady", "")

	// Read password Secret if configured
	var passwordValue string
	var passwordHash string
	if user.Spec.PasswordSecretRef != nil {
		pw, err := r.readPasswordSecret(ctx, user)
		if err != nil {
			msg := fmt.Sprintf("password secret read failed: %v", err)
			r.setStatus(ctx, user, "Failed", metav1.ConditionFalse, EventReasonUserSyncFailed, msg, nil, "", nil)
			return ctrl.Result{RequeueAfter: requeue}, err
		}
		passwordValue = pw
		passwordHash = sha256Hex([]byte(pw))
	}

	// Connect
	nc, err := target.NewClient(r.Client)
	if err != nil {
		msg := fmt.Sprintf("failed to connect to Neo4j: %v", err)
		r.setStatus(ctx, user, "Failed", metav1.ConditionFalse, EventReasonConnectionFailed, msg, nil, "", nil)
		r.Recorder.Event(user, corev1.EventTypeWarning, EventReasonConnectionFailed, msg)
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	defer func() {
		if err := nc.Close(); err != nil {
			logger.Error(err, "failed to close Neo4j client")
		}
	}()

	// Existence
	info, err := nc.ShowUser(ctx, username)
	if err != nil {
		return r.fail(ctx, user, "lookup failed", err, requeue)
	}

	desiredSuspended := user.Spec.AccountStatus == "suspended"

	if info == nil {
		// Create user
		opts := neo4jclient.AlterUserOptions{}
		if passwordValue != "" {
			opts.WithPassword(passwordValue)
			opts.WithPasswordChangeRequired(user.Spec.RequirePasswordChange)
		}
		opts.WithSuspended(desiredSuspended)
		if user.Spec.HomeDatabase != "" {
			opts.WithHomeDatabase(user.Spec.HomeDatabase)
		}
		for _, ap := range user.Spec.ExternalAuth {
			opts.WithSetAuthProviders(neo4jclient.AuthProviderInfo{Provider: ap.Provider, ID: ap.ID})
		}
		if err := nc.CreateUserAdvanced(ctx, username, &opts, true); err != nil {
			return r.fail(ctx, user, "create user failed", err, requeue)
		}
		r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonUserCreated, "User %q created", username)
		// Re-read so we have fresh state for the rest of the loop.
		info, err = nc.ShowUser(ctx, username)
		if err != nil {
			return r.fail(ctx, user, "post-create read failed", err, requeue)
		}
		if info == nil {
			// Should be impossible — we just created it. Treat as transient.
			return r.fail(ctx, user, "user not visible after create (eventual consistency?)", nil, requeue)
		}
	} else {
		// Diff and ALTER existing user.
		opts := r.computeAlter(user, info, passwordHash, passwordValue, desiredSuspended)
		if !opts.IsEmpty() {
			if err := nc.AlterUser(ctx, username, &opts); err != nil {
				return r.fail(ctx, user, "alter user failed", err, requeue)
			}
			r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonUserUpdated, "User %q updated", username)
		}
	}

	// Role binding diff. Resolve same-namespace Neo4jRole CR names in
	// spec.roles to their effective Neo4j role name first (#260) — literal
	// Neo4j names (built-ins, externally-created roles) pass through unchanged.
	desiredRoles := normaliseRoles(user.Spec.Roles)
	desiredRoles, resolvedRoles := resolveRoleNames(ctx, r.Client, user.Namespace, user.Spec.ClusterRef, desiredRoles)
	desiredRoles = normaliseRoles(desiredRoles) // dedupe any CR-name/spec.name collisions
	if len(resolvedRoles) > 0 {
		r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonRolesResolved,
			"resolved Neo4jRole CR names to Neo4j role names: %v", resolvedRoles)
	}
	currentRoles := normaliseRoles(info.Roles) // Note: may be slightly stale after CREATE; re-read just below.
	// info is non-nil here: the create branch above re-reads it (and bails if
	// still nil) and the else branch only runs when it was already non-nil.
	if len(info.Roles) == 0 {
		// fresh-create path: fetch live roles since create-time info has none granted.
		if live, err := nc.ListUserRoles(ctx, username); err == nil {
			currentRoles = normaliseRoles(live)
		}
	}

	addRoles, dropRoles, missing := r.diffRoles(ctx, user, desiredRoles, currentRoles)
	for _, role := range addRoles {
		if err := nc.GrantRoleToUser(ctx, role, username); err != nil {
			return r.fail(ctx, user, fmt.Sprintf("grant %q failed", role), err, requeue)
		}
	}
	for _, role := range dropRoles {
		if err := nc.RevokeRoleFromUser(ctx, role, username); err != nil {
			return r.fail(ctx, user, fmt.Sprintf("revoke %q failed", role), err, requeue)
		}
	}
	if len(addRoles) > 0 {
		r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonRolesGranted, "granted %v", addRoles)
	}
	if len(dropRoles) > 0 {
		r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonRolesRevoked, "revoked %v", dropRoles)
	}

	// Re-read final role list for status.
	finalRoles, err := nc.ListUserRoles(ctx, username)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to read final role list (non-fatal)")
		finalRoles = currentRoles
	}
	finalRoles = normaliseRoles(finalRoles)

	// Conditions
	if len(missing) > 0 {
		r.setNamedCondition(ctx, user, ConditionTypePendingDependencies, metav1.ConditionTrue, ConditionReasonRolesPending,
			fmt.Sprintf("waiting for roles to exist: %v", missing))
		r.Recorder.Eventf(user, corev1.EventTypeWarning, EventReasonRolePending,
			"waiting for roles to exist: %v", missing)
	} else {
		r.setNamedCondition(ctx, user, ConditionTypePendingDependencies, metav1.ConditionFalse, "AllDependenciesPresent", "")
	}

	rolesEqual := stringSlicesEqualSorted(desiredRoles, finalRoles)
	if rolesEqual {
		r.setNamedCondition(ctx, user, ConditionTypeRolesSynced, metav1.ConditionTrue, ConditionReasonRolesSynced, "granted roles match spec")
	} else {
		r.setNamedCondition(ctx, user, ConditionTypeRolesSynced, metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("granted roles %v differ from spec %v", finalRoles, desiredRoles))
	}

	if user.Spec.PasswordSecretRef != nil {
		r.setNamedCondition(ctx, user, ConditionTypePasswordSynced, metav1.ConditionTrue, ConditionReasonPasswordSynced,
			"password secret hash applied")
	}

	// Phase / Ready
	overallReady := rolesEqual && len(missing) == 0
	var rotated *metav1.Time
	if user.Status.PasswordSecretHash != passwordHash && passwordHash != "" {
		now := metav1.Now()
		rotated = &now
		r.Recorder.Event(user, corev1.EventTypeNormal, EventReasonPasswordRotated, "password updated from Secret")
	}

	if overallReady {
		// Emit UserReady once when the CR first transitions into Ready,
		// not on every subsequent reconcile.
		if user.Status.Phase != "Ready" {
			r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonUserReady,
				"User %q is ready (%d roles in sync)", username, len(finalRoles))
		}
		r.setStatus(ctx, user, "Ready", metav1.ConditionTrue, ConditionReasonUserReady,
			fmt.Sprintf("user %q is in sync (%d roles)", username, len(finalRoles)),
			finalRoles, passwordHash, rotated)
	} else {
		r.setStatus(ctx, user, "Pending", metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("user %q reconciled with pending dependencies", username),
			finalRoles, passwordHash, rotated)
	}

	return ctrl.Result{RequeueAfter: requeue}, nil
}

func (r *Neo4jUserReconciler) handleDeletion(ctx context.Context, user *neo4jv1beta1.Neo4jUser, username string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(user, Neo4jUserFinalizer) {
		return ctrl.Result{}, nil
	}
	requeue := r.requeueAfter()

	if strings.EqualFold(user.Spec.DeletionPolicy, "Retain") {
		controllerutil.RemoveFinalizer(user, Neo4jUserFinalizer)
		return ctrl.Result{}, r.Update(ctx, user)
	}

	target, err := ResolveClusterRef(ctx, r.Client, user.Namespace, user.Spec.ClusterRef)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		controllerutil.RemoveFinalizer(user, Neo4jUserFinalizer)
		return ctrl.Result{}, r.Update(ctx, user)
	}
	if !target.IsReady() {
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	nc, err := target.NewClient(r.Client)
	if err != nil {
		logger.Error(err, "cannot connect during user deletion; releasing finalizer")
		controllerutil.RemoveFinalizer(user, Neo4jUserFinalizer)
		return ctrl.Result{}, r.Update(ctx, user)
	}
	defer func() { _ = nc.Close() }()

	if err := nc.DropUserIfExists(ctx, username); err != nil {
		if classifyFinalizerCleanup(user, err) == retryCleanup {
			r.Recorder.Eventf(user, corev1.EventTypeWarning, EventReasonUserDeletionFailed,
				"DROP USER %q failed, will retry: %v", username, err)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
		r.Recorder.Eventf(user, corev1.EventTypeWarning, EventReasonUserDeletionFailed,
			"DROP USER %q failed; releasing finalizer to avoid wedging deletion: %v", username, err)
		controllerutil.RemoveFinalizer(user, Neo4jUserFinalizer)
		return ctrl.Result{}, r.Update(ctx, user)
	}
	r.Recorder.Eventf(user, corev1.EventTypeNormal, EventReasonUserDeleted, "User %q dropped", username)
	controllerutil.RemoveFinalizer(user, Neo4jUserFinalizer)
	return ctrl.Result{}, r.Update(ctx, user)
}

// computeAlter builds the set of ALTER USER clauses needed to bring `info`
// (current state) into agreement with `user.Spec` (desired state).
func (r *Neo4jUserReconciler) computeAlter(
	user *neo4jv1beta1.Neo4jUser,
	info *neo4jclient.UserInfo,
	passwordHash, passwordValue string,
	desiredSuspended bool,
) neo4jclient.AlterUserOptions {
	opts := neo4jclient.AlterUserOptions{}

	// Password rotation: triggered when the Secret value SHA-256 differs
	// from the last-applied hash recorded in status.
	if user.Spec.PasswordSecretRef != nil && passwordHash != "" && passwordHash != user.Status.PasswordSecretHash {
		opts.WithPassword(passwordValue)
	}

	// Password change-required toggle: only emit a clause when the bit needs
	// to change.
	if info.PasswordChangeRequired != user.Spec.RequirePasswordChange {
		opts.WithPasswordChangeRequired(user.Spec.RequirePasswordChange)
	}

	// Status
	if info.Suspended != desiredSuspended {
		opts.WithSuspended(desiredSuspended)
	}

	// Home database
	switch {
	case user.Spec.HomeDatabase == "" && info.HomeDatabase != "":
		opts.WithoutHomeDatabase()
	case user.Spec.HomeDatabase != "" && user.Spec.HomeDatabase != info.HomeDatabase:
		opts.WithHomeDatabase(user.Spec.HomeDatabase)
	}

	// External auth providers
	desired := map[string]string{}
	for _, ap := range user.Spec.ExternalAuth {
		desired[ap.Provider] = ap.ID
	}
	current := map[string]string{}
	for _, ap := range info.AuthProviders {
		// Skip the implicit `native` provider — it's managed via SET PASSWORD.
		if strings.EqualFold(ap.Provider, "native") {
			continue
		}
		current[ap.Provider] = ap.ID
	}

	// Providers to remove or update
	for prov, curID := range current {
		desiredID, ok := desired[prov]
		if !ok {
			opts.WithRemovedAuthProviders(prov)
			continue
		}
		if desiredID != curID {
			// Re-set in place: a SET AUTH overwrites the existing entry's ID.
			opts.WithSetAuthProviders(neo4jclient.AuthProviderInfo{Provider: prov, ID: desiredID})
		}
	}
	// Providers to add
	for prov, id := range desired {
		if _, ok := current[prov]; ok {
			continue
		}
		opts.WithSetAuthProviders(neo4jclient.AuthProviderInfo{Provider: prov, ID: id})
	}

	return opts
}

// diffRoles computes role grants/revokes and reports any desired roles that
// do not yet exist (built-in or as a Neo4jRole CR). PUBLIC is implicit and
// is never granted/revoked here.
func (r *Neo4jUserReconciler) diffRoles(ctx context.Context, user *neo4jv1beta1.Neo4jUser, desired, current []string) (add []string, drop []string, missing []string) {
	currentSet := map[string]struct{}{}
	for _, c := range current {
		currentSet[c] = struct{}{}
	}
	desiredSet := map[string]struct{}{}
	for _, d := range desired {
		desiredSet[d] = struct{}{}
	}

	// Existence pre-flight: built-ins always exist, otherwise check for a
	// Neo4jRole CR with that name in the same namespace and matching cluster.
	for _, role := range desired {
		if validation.IsBuiltInRole(role) {
			continue
		}
		if _, ok := currentSet[role]; ok {
			continue
		}
		if r.roleResourceExists(ctx, user.Namespace, user.Spec.ClusterRef, role) {
			continue
		}
		missing = append(missing, role)
	}

	for _, d := range desired {
		if _, ok := currentSet[d]; ok {
			continue
		}
		if containsString(missing, d) {
			continue
		}
		add = append(add, d)
	}
	for _, c := range current {
		// Never revoke PUBLIC — it is implicit and Neo4j refuses removal.
		if strings.EqualFold(c, "PUBLIC") {
			continue
		}
		if _, ok := desiredSet[c]; ok {
			continue
		}
		drop = append(drop, c)
	}
	sort.Strings(add)
	sort.Strings(drop)
	sort.Strings(missing)
	return add, drop, missing
}

// roleResourceExists reports whether a Neo4jRole CR with .spec.name (or
// metadata.name when spec.name is empty) equals roleName, in the same
// namespace and pointing at the same clusterRef. Delegates to the shared
// roleNameExists (see role_resolution.go) — same index used by #260
// CR-name resolution.
func (r *Neo4jUserReconciler) roleResourceExists(ctx context.Context, namespace, clusterRef, roleName string) bool {
	return roleNameExists(ctx, r.Client, namespace, clusterRef, roleName)
}

func (r *Neo4jUserReconciler) readPasswordSecret(ctx context.Context, user *neo4jv1beta1.Neo4jUser) (string, error) {
	ref := user.Spec.PasswordSecretRef
	key := ref.Key
	if key == "" {
		key = "password"
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: user.Namespace, Name: ref.Name}, secret); err != nil {
		return "", err
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %q has no key %q", ref.Name, key)
	}
	if len(val) == 0 {
		return "", fmt.Errorf("secret %q key %q is empty", ref.Name, key)
	}
	return string(val), nil
}

func (r *Neo4jUserReconciler) requeueAfter() time.Duration {
	if r.RequeueAfter > 0 {
		return r.RequeueAfter
	}
	return 30 * time.Second
}

func (r *Neo4jUserReconciler) fail(ctx context.Context, user *neo4jv1beta1.Neo4jUser, label string, err error, requeue time.Duration) (ctrl.Result, error) {
	msg := label
	if err != nil {
		msg = fmt.Sprintf("%s: %v", label, err)
	}
	r.setStatus(ctx, user, "Failed", metav1.ConditionFalse, EventReasonUserSyncFailed, msg, nil, "", nil)
	r.Recorder.Event(user, corev1.EventTypeWarning, EventReasonUserSyncFailed, msg)
	return ctrl.Result{RequeueAfter: requeue}, err
}

func (r *Neo4jUserReconciler) setStatus(
	ctx context.Context,
	user *neo4jv1beta1.Neo4jUser,
	phase string,
	readyStatus metav1.ConditionStatus,
	readyReason, message string,
	currentRoles []string,
	passwordHash string,
	rotated *metav1.Time,
) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jUser{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(user), latest); err != nil {
			return err
		}
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, readyStatus, readyReason, message)
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		if currentRoles != nil {
			latest.Status.CurrentRoles = currentRoles
		}
		if passwordHash != "" {
			latest.Status.PasswordSecretHash = passwordHash
		}
		if rotated != nil {
			latest.Status.PasswordLastRotated = rotated
		}
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to update Neo4jUser status")
	}
}

func (r *Neo4jUserReconciler) setNamedCondition(ctx context.Context, user *neo4jv1beta1.Neo4jUser, condType string, status metav1.ConditionStatus, reason, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jUser{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(user), latest); err != nil {
			return err
		}
		SetNamedCondition(&latest.Status.Conditions, condType, latest.Generation, status, reason, message)
		latest.Status.ObservedGeneration = latest.Generation
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to set condition on Neo4jUser", "condition", condType)
	}
}

// SetupWithManager registers the controller and wires up watches:
//   - Neo4jRole: re-reconciles users with PendingDependencies when a referenced
//     role is created.
//   - Neo4jEnterpriseCluster / Neo4jEnterpriseStandalone: re-reconciles every
//     user pointing at a changed cluster, so users react immediately to the
//     cluster's Ready condition flipping (otherwise we wait up to 30s for the
//     next requeue, which compounds across multiple cluster status updates
//     during formation and can starve the user reconcile in CI).
func (r *Neo4jUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c := mgr.GetClient()
	enqueueUsersForCluster := EnqueueDependentsForClusterChange(
		c,
		func() client.ObjectList { return &neo4jv1beta1.Neo4jUserList{} },
		func(list client.ObjectList, emit func(name, namespace, clusterRef string)) {
			users, ok := list.(*neo4jv1beta1.Neo4jUserList)
			if !ok {
				return
			}
			for i := range users.Items {
				u := &users.Items[i]
				emit(u.Name, u.Namespace, u.Spec.ClusterRef)
			}
		},
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jUser{}).
		Watches(&neo4jv1beta1.Neo4jEnterpriseCluster{}, enqueueUsersForCluster).
		Watches(&neo4jv1beta1.Neo4jEnterpriseStandalone{}, enqueueUsersForCluster).
		Watches(&neo4jv1beta1.Neo4jRole{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			role, ok := obj.(*neo4jv1beta1.Neo4jRole)
			if !ok {
				return nil
			}
			// Match a user's spec.roles against BOTH the role's effective Neo4j
			// name AND its CR metadata.name, so a user that referenced the role
			// by either form re-reconciles when the role lands (#260).
			roleName := role.Spec.Name
			if roleName == "" {
				roleName = role.Name
			}
			users := &neo4jv1beta1.Neo4jUserList{}
			if err := c.List(ctx, users, client.InNamespace(role.Namespace)); err != nil {
				return nil
			}
			var reqs []reconcile.Request
			for i := range users.Items {
				u := &users.Items[i]
				if u.Spec.ClusterRef != role.Spec.ClusterRef {
					continue
				}
				for _, rname := range u.Spec.Roles {
					if rname == roleName || rname == role.Name {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: u.Namespace, Name: u.Name}})
						break
					}
				}
			}
			return reqs
		})).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}

// effectiveUsername returns spec.username if non-empty, otherwise metadata.name.
func effectiveUsername(user *neo4jv1beta1.Neo4jUser) string {
	if user.Spec.Username != "" {
		return user.Spec.Username
	}
	return user.Name
}

// normaliseRoles trims, deduplicates, sorts, and filters out the implicit
// PUBLIC role from a role-name slice.
//
// PUBLIC is auto-assigned to every Neo4j user — it always shows up in
// `SHOW USERS YIELD roles`, but listing it in spec.roles has no effect
// (the validator warns). Filtering here means downstream `rolesEqual`
// comparisons match cleanly: a fresh user with `roles: [reader]` has live
// roles `[PUBLIC, reader]`, which after this filter becomes `[reader]` on
// both sides of the diff. Without this filter the user would sit in
// `Pending` forever — observed empirically as a 9+ min reconcile loop in
// CI before the spec timeout fires.
func normaliseRoles(in []string) []string {
	set := map[string]struct{}{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if strings.EqualFold(r, "PUBLIC") {
			continue
		}
		set[r] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stringSlicesEqualSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

// sha256Hex returns the hex-encoded SHA-256 of b.
//
// IMPORTANT: this is NOT used as a password-storage hash and is NOT used in
// any authentication path. The operator computes a digest of the password
// Secret's value purely as a change-detection fingerprint — equality
// comparison only — so the controller can answer "did the Secret rotate
// since I last applied it?" on each reconcile loop. The actual password
// authentication is performed by Neo4j against its own (scrypt-based)
// hash in the `system` database; the password text itself lives in the
// Kubernetes Secret. For change detection what matters is collision
// resistance (SHA-256 is correct), not computational cost (slow hashes
// like bcrypt/Argon2/scrypt would actively hurt the loop for zero
// security benefit). The fingerprint sits in `status.passwordSecretHash`,
// which requires `get neo4jusers` RBAC; an attacker with that permission
// can already read the operator's Secret directly via `get secrets`, so
// the hash leaks no information not otherwise available.
//
// codeql[go/weak-sensitive-data-hashing]: Intentional — fingerprint for
// Secret-rotation detection, not a password-storage hash. See doc above.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
