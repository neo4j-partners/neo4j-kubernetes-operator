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
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jRoleFinalizer is the finalizer that ensures the controller gets a
// chance to drop the underlying role (or release the finalizer when policy
// is Retain) before the CR is removed.
const Neo4jRoleFinalizer = "neo4j.com/role-finalizer"

// Neo4jRoleReconciler reconciles a Neo4jRole resource.
type Neo4jRoleReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
	Validator               *validation.RoleValidator
}

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jroles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jroles/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives a Neo4jRole resource toward its desired state.
func (r *Neo4jRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("neo4jrole", req.NamespacedName)

	role := &neo4jv1beta1.Neo4jRole{}
	if err := r.Get(ctx, req.NamespacedName, role); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	roleName := effectiveRoleName(role)
	requeue := r.requeueAfter()

	// Deletion path
	if role.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, role, roleName)
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(role, Neo4jRoleFinalizer) {
		controllerutil.AddFinalizer(role, Neo4jRoleFinalizer)
		if err := r.Update(ctx, role); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate
	if r.Validator != nil {
		res := r.Validator.Validate(ctx, role)
		for _, w := range res.Warnings {
			r.Recorder.Eventf(role, corev1.EventTypeWarning, EventReasonValidationWarning, "%s", w)
		}
		if len(res.Errors) > 0 {
			msg := fmt.Sprintf("validation failed: %s", res.Errors.ToAggregate().Error())
			r.setStatus(ctx, role, "Failed", metav1.ConditionFalse, EventReasonValidationFailed, msg, nil, false)
			r.Recorder.Event(role, corev1.EventTypeWarning, EventReasonValidationFailed, msg)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
	}

	// Resolve cluster
	target, err := ResolveClusterRef(ctx, r.Client, role.Namespace, role.Spec.ClusterRef)
	if err != nil {
		logger.Error(err, "failed to resolve clusterRef")
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		msg := fmt.Sprintf("clusterRef %q not found", role.Spec.ClusterRef)
		r.setStatus(ctx, role, "Pending", metav1.ConditionFalse, EventReasonClusterNotFound, msg, nil, false)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	if !target.IsReady() {
		msg := fmt.Sprintf("clusterRef %q is not Ready", role.Spec.ClusterRef)
		r.setNamedCondition(ctx, role, ConditionTypeClusterNotReady, metav1.ConditionTrue, ConditionReasonClusterNotReady, msg)
		r.setStatus(ctx, role, "Pending", metav1.ConditionFalse, ConditionReasonClusterNotReady, msg, nil, false)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, role, ConditionTypeClusterNotReady, metav1.ConditionFalse, "ClusterReady", "")

	// Connect
	nc, err := target.NewClient(r.Client)
	if err != nil {
		msg := fmt.Sprintf("failed to connect to Neo4j: %v", err)
		r.setStatus(ctx, role, "Failed", metav1.ConditionFalse, EventReasonConnectionFailed, msg, nil, false)
		r.Recorder.Event(role, corev1.EventTypeWarning, EventReasonConnectionFailed, msg)
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	defer func() {
		if err := nc.Close(); err != nil {
			logger.Error(err, "failed to close Neo4j client")
		}
	}()

	// Ensure role exists
	info, err := nc.ShowRole(ctx, roleName)
	if err != nil {
		return r.fail(ctx, role, "lookup failed", err, requeue)
	}
	if info == nil {
		if validation.IsBuiltInRole(roleName) {
			return r.fail(ctx, role, fmt.Sprintf("built-in role %q not found in cluster", roleName), nil, requeue)
		}
		if err := nc.CreateRoleAdvanced(ctx, roleName, role.Spec.CopyOf, true); err != nil {
			return r.fail(ctx, role, "create role failed", err, requeue)
		}
		r.Recorder.Eventf(role, corev1.EventTypeNormal, EventReasonRoleCreated, "Role %q created", roleName)
	}

	// Privilege diff
	desired, desiredByCanonical, errs := r.canonicaliseDesired(role.Spec.Privileges)
	if errs != nil {
		return r.fail(ctx, role, "privilege canonicalisation failed", errs, requeue)
	}
	current, immutableSet, currentByCanonical, err := r.fetchCurrentPrivileges(ctx, nc, roleName)
	if err != nil {
		return r.fail(ctx, role, "fetch current privileges failed", err, requeue)
	}

	toAdd := setDifference(desired, current)
	toRemove := []string{}
	if role.Spec.EnforcePrivileges {
		toRemove = setDifference(current, desired)
	}

	for _, canon := range toAdd {
		// Execute the original spec text, never the canonical form: canonical
		// upper-cases bare tokens including unquoted identifiers, so a role/
		// graph/database named e.g. `users` written unquoted would canonicalise
		// to `... TO USERS` and target the wrong (case-sensitive) role. The diff
		// is keyed on canonical; the statement run is the user's original.
		stmt := desiredByCanonical[canon]
		if stmt == "" {
			stmt = canon // defensive fallback (should not happen)
		}
		if err := nc.ExecutePrivilegeStatement(ctx, stmt); err != nil {
			return r.fail(ctx, role, fmt.Sprintf("apply privilege %q failed", stmt), err, requeue)
		}
	}

	drift := false
	for _, canon := range toRemove {
		if _, immutable := immutableSet[canon]; immutable {
			drift = true
			r.Recorder.Eventf(role, corev1.EventTypeWarning, EventReasonPrivilegesDriftKept,
				"cannot revoke immutable privilege %q", canon)
			continue
		}
		original := currentByCanonical[canon]
		if original == "" {
			original = canon
		}
		revoke, err := neo4jclient.DerivePrivilegeRevoke(original)
		if err != nil {
			drift = true
			r.Recorder.Eventf(role, corev1.EventTypeWarning, EventReasonPrivilegesDriftKept,
				"cannot derive REVOKE for %q: %v", original, err)
			continue
		}
		if err := nc.ExecutePrivilegeStatement(ctx, revoke); err != nil {
			return r.fail(ctx, role, fmt.Sprintf("revoke privilege %q failed", revoke), err, requeue)
		}
	}

	// Re-read for the AppliedPrivileges status field.
	final, _, _, err := r.fetchCurrentPrivileges(ctx, nc, roleName)
	if err != nil {
		return r.fail(ctx, role, "post-apply read failed", err, requeue)
	}

	// PrivilegesSynced condition
	if drift {
		r.setNamedCondition(ctx, role, ConditionTypePrivilegesSynced, metav1.ConditionFalse, ConditionReasonPrivilegesDrifted,
			"some privileges could not be reconciled (e.g. immutable). See events.")
	} else {
		r.setNamedCondition(ctx, role, ConditionTypePrivilegesSynced, metav1.ConditionTrue, ConditionReasonPrivilegesSynced,
			"privileges match spec")
	}

	if len(toAdd)+len(toRemove) > 0 {
		r.Recorder.Eventf(role, corev1.EventTypeNormal, EventReasonPrivilegesApplied,
			"applied %d added / %d revoked privileges", len(toAdd), len(toRemove))
	}

	// Emit RoleReady on the first transition to Ready (avoids spamming the
	// event stream on every reconcile after the role is already Ready).
	if role.Status.Phase != "Ready" {
		r.Recorder.Eventf(role, corev1.EventTypeNormal, EventReasonRoleReady,
			"Role %q is ready (%d privileges in sync)", roleName, len(final))
	}

	r.setStatus(ctx, role, "Ready", metav1.ConditionTrue, ConditionReasonRoleReady,
		fmt.Sprintf("role %q is in sync (%d privileges)", roleName, len(final)), final, drift)

	return ctrl.Result{RequeueAfter: requeue}, nil
}

func (r *Neo4jRoleReconciler) handleDeletion(ctx context.Context, role *neo4jv1beta1.Neo4jRole, roleName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(role, Neo4jRoleFinalizer) {
		return ctrl.Result{}, nil
	}

	requeue := r.requeueAfter()

	// Built-in roles are never dropped; just release the finalizer.
	if validation.IsBuiltInRole(roleName) {
		controllerutil.RemoveFinalizer(role, Neo4jRoleFinalizer)
		return ctrl.Result{}, r.Update(ctx, role)
	}

	if strings.EqualFold(role.Spec.DeletionPolicy, "Retain") {
		controllerutil.RemoveFinalizer(role, Neo4jRoleFinalizer)
		return ctrl.Result{}, r.Update(ctx, role)
	}

	target, err := ResolveClusterRef(ctx, r.Client, role.Namespace, role.Spec.ClusterRef)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		// Cluster gone — release the finalizer.
		controllerutil.RemoveFinalizer(role, Neo4jRoleFinalizer)
		return ctrl.Result{}, r.Update(ctx, role)
	}

	if !target.IsReady() {
		// Wait for the cluster to be ready so we can DROP cleanly.
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	nc, err := target.NewClient(r.Client)
	if err != nil {
		// If we cannot connect, surface the error but do not block deletion
		// indefinitely.
		logger.Error(err, "cannot connect during role deletion; releasing finalizer")
		controllerutil.RemoveFinalizer(role, Neo4jRoleFinalizer)
		return ctrl.Result{}, r.Update(ctx, role)
	}
	defer func() { _ = nc.Close() }()

	if err := nc.DropRoleIfExists(ctx, roleName); err != nil {
		if classifyFinalizerCleanup(role, err) == retryCleanup {
			r.Recorder.Eventf(role, corev1.EventTypeWarning, EventReasonRoleDeletionFailed,
				"DROP ROLE %q failed, will retry: %v", roleName, err)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
		r.Recorder.Eventf(role, corev1.EventTypeWarning, EventReasonRoleDeletionFailed,
			"DROP ROLE %q failed; releasing finalizer to avoid wedging deletion: %v", roleName, err)
		controllerutil.RemoveFinalizer(role, Neo4jRoleFinalizer)
		return ctrl.Result{}, r.Update(ctx, role)
	}

	r.Recorder.Eventf(role, corev1.EventTypeNormal, EventReasonRoleDeleted, "Role %q dropped", roleName)
	controllerutil.RemoveFinalizer(role, Neo4jRoleFinalizer)
	return ctrl.Result{}, r.Update(ctx, role)
}

// canonicaliseDesired turns the spec privileges into a deduplicated, sorted
// slice of canonical statements plus a map from each canonical form back to the
// original spec text. The diff is keyed on the canonical form, but callers
// execute the original text — the canonical form upper-cases bare tokens
// (including unquoted identifiers) and is explicitly not safe to feed to Neo4j.
func (r *Neo4jRoleReconciler) canonicaliseDesired(stmts []string) ([]string, map[string]string, error) {
	set := map[string]struct{}{}
	byCanonical := map[string]string{}
	for _, s := range stmts {
		canon := neo4jclient.CanonicalisePrivilegeStatement(s)
		if canon == "" {
			continue
		}
		set[canon] = struct{}{}
		if _, exists := byCanonical[canon]; !exists {
			byCanonical[canon] = s // first original wins for a given canonical form
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, byCanonical, nil
}

// fetchCurrentPrivileges reads SHOW ROLE PRIVILEGES AS COMMANDS and returns:
//   - canonical: sorted, deduplicated set of canonical statements
//   - immutable: set (by canonical form) of statements flagged immutable
//   - byCanonical: map from canonical form back to the original command,
//     used to derive REVOKE statements verbatim.
func (r *Neo4jRoleReconciler) fetchCurrentPrivileges(ctx context.Context, nc *neo4jclient.Client, roleName string) ([]string, map[string]struct{}, map[string]string, error) {
	rows, err := nc.ShowRolePrivileges(ctx, roleName)
	if err != nil {
		return nil, nil, nil, err
	}
	set := map[string]struct{}{}
	immutable := map[string]struct{}{}
	byCanonical := map[string]string{}
	for _, row := range rows {
		canon := neo4jclient.CanonicalisePrivilegeStatement(row.Command)
		if canon == "" {
			continue
		}
		set[canon] = struct{}{}
		byCanonical[canon] = row.Command
		if row.Immutable {
			immutable[canon] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, immutable, byCanonical, nil
}

// setDifference returns the elements in a not present in b. Both inputs must
// be sorted; the result is also sorted.
func setDifference(a, b []string) []string {
	bset := make(map[string]struct{}, len(b))
	for _, x := range b {
		bset[x] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		if _, ok := bset[x]; !ok {
			out = append(out, x)
		}
	}
	return out
}

func (r *Neo4jRoleReconciler) requeueAfter() time.Duration {
	if r.RequeueAfter > 0 {
		return r.RequeueAfter
	}
	return 30 * time.Second
}

func (r *Neo4jRoleReconciler) fail(ctx context.Context, role *neo4jv1beta1.Neo4jRole, label string, err error, requeue time.Duration) (ctrl.Result, error) {
	msg := label
	if err != nil {
		msg = fmt.Sprintf("%s: %v", label, err)
	}
	r.setStatus(ctx, role, "Failed", metav1.ConditionFalse, EventReasonRoleSyncFailed, msg, nil, false)
	r.Recorder.Event(role, corev1.EventTypeWarning, EventReasonRoleSyncFailed, msg)
	return ctrl.Result{RequeueAfter: requeue}, err
}

func (r *Neo4jRoleReconciler) setStatus(
	ctx context.Context,
	role *neo4jv1beta1.Neo4jRole,
	phase string,
	readyStatus metav1.ConditionStatus,
	readyReason, message string,
	appliedPrivileges []string,
	drift bool,
) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jRole{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(role), latest); err != nil {
			return err
		}
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, readyStatus, readyReason, message)
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		if appliedPrivileges != nil {
			latest.Status.AppliedPrivileges = appliedPrivileges
		}
		latest.Status.PrivilegeDrift = drift
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to update Neo4jRole status")
	}
}

func (r *Neo4jRoleReconciler) setNamedCondition(ctx context.Context, role *neo4jv1beta1.Neo4jRole, condType string, status metav1.ConditionStatus, reason, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jRole{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(role), latest); err != nil {
			return err
		}
		SetNamedCondition(&latest.Status.Conditions, condType, latest.Generation, status, reason, message)
		latest.Status.ObservedGeneration = latest.Generation
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to set condition on Neo4jRole", "condition", condType)
	}
}

// effectiveRoleName returns spec.name if non-empty, otherwise metadata.name.
func effectiveRoleName(role *neo4jv1beta1.Neo4jRole) string {
	if role.Spec.Name != "" {
		return role.Spec.Name
	}
	return role.Name
}

// SetupWithManager registers the controller and watches the cluster /
// standalone CRs whose state transitions should re-trigger role
// reconciliation (Pending → Ready). Without this watch, roles only
// notice cluster status changes on their next 30-second requeue, which
// can starve reconciles in CI when clusters bootstrap quickly.
func (r *Neo4jRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueRolesForCluster := EnqueueDependentsForClusterChange(
		mgr.GetClient(),
		func() client.ObjectList { return &neo4jv1beta1.Neo4jRoleList{} },
		func(list client.ObjectList, emit func(name, namespace, clusterRef string)) {
			roles, ok := list.(*neo4jv1beta1.Neo4jRoleList)
			if !ok {
				return
			}
			for i := range roles.Items {
				role := &roles.Items[i]
				emit(role.Name, role.Namespace, role.Spec.ClusterRef)
			}
		},
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jRole{}).
		Watches(&neo4jv1beta1.Neo4jEnterpriseCluster{}, enqueueRolesForCluster).
		Watches(&neo4jv1beta1.Neo4jEnterpriseStandalone{}, enqueueRolesForCluster).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}
