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
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jRoleBindingFinalizer is the finalizer that ensures the controller
// gets a chance to revoke its grants (or release the finalizer when policy
// is Retain) before the CR is removed.
const Neo4jRoleBindingFinalizer = "neo4j.com/rolebinding-finalizer"

// Neo4jRoleBindingReconciler reconciles a Neo4jRoleBinding resource.
//
// Unlike Neo4jUser, this controller never creates or drops the underlying
// Neo4j user — it only manages role grants. The user is expected to exist
// in Neo4j (typically provisioned via SSO/LDAP first-login) by the time
// the cluster is Ready. If absent, the binding waits in a UserNotFound
// state and reconciles when the user appears.
type Neo4jRoleBindingReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
	Validator               *validation.RoleBindingValidator
}

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jrolebindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jrolebindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jroles,verbs=get;list;watch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jusers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives a Neo4jRoleBinding resource toward its desired state.
func (r *Neo4jRoleBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("neo4jrolebinding", req.NamespacedName)

	rb := &neo4jv1beta1.Neo4jRoleBinding{}
	if err := r.Get(ctx, req.NamespacedName, rb); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	requeue := r.requeueAfter()

	if rb.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, rb)
	}

	if !controllerutil.ContainsFinalizer(rb, Neo4jRoleBindingFinalizer) {
		controllerutil.AddFinalizer(rb, Neo4jRoleBindingFinalizer)
		if err := r.Update(ctx, rb); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate
	if r.Validator != nil {
		res := r.Validator.Validate(ctx, rb)
		for _, w := range res.Warnings {
			r.Recorder.Eventf(rb, corev1.EventTypeWarning, EventReasonValidationWarning, "%s", w)
		}
		if len(res.Errors) > 0 {
			msg := fmt.Sprintf("validation failed: %s", res.Errors.ToAggregate().Error())
			r.setStatus(ctx, rb, "Failed", metav1.ConditionFalse, EventReasonValidationFailed, msg, nil)
			r.Recorder.Event(rb, corev1.EventTypeWarning, EventReasonValidationFailed, msg)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
	}

	// Resolve cluster
	target, err := ResolveClusterRef(ctx, r.Client, rb.Namespace, rb.Spec.ClusterRef)
	if err != nil {
		logger.Error(err, "failed to resolve clusterRef")
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		msg := fmt.Sprintf("clusterRef %q not found", rb.Spec.ClusterRef)
		r.setStatus(ctx, rb, "Pending", metav1.ConditionFalse, EventReasonClusterNotFound, msg, nil)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	if !target.IsReady() {
		msg := fmt.Sprintf("clusterRef %q is not Ready", rb.Spec.ClusterRef)
		r.setNamedCondition(ctx, rb, ConditionTypeClusterNotReady, metav1.ConditionTrue, ConditionReasonClusterNotReady, msg)
		r.setStatus(ctx, rb, "Pending", metav1.ConditionFalse, ConditionReasonClusterNotReady, msg, nil)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, rb, ConditionTypeClusterNotReady, metav1.ConditionFalse, "ClusterReady", "")

	// Connect
	nc, err := target.NewClient(r.Client)
	if err != nil {
		msg := fmt.Sprintf("failed to connect to Neo4j: %v", err)
		r.setStatus(ctx, rb, "Failed", metav1.ConditionFalse, EventReasonConnectionFailed, msg, nil)
		r.Recorder.Event(rb, corev1.EventTypeWarning, EventReasonConnectionFailed, msg)
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	defer func() { _ = nc.Close() }()

	// Verify the user exists. We never create users from a binding.
	info, err := nc.ShowUser(ctx, rb.Spec.Username)
	if err != nil {
		return r.fail(ctx, rb, "lookup failed", err, requeue)
	}
	if info == nil {
		msg := fmt.Sprintf("user %q does not exist in Neo4j; bindings only manage grants for externally provisioned users", rb.Spec.Username)
		r.setNamedCondition(ctx, rb, ConditionTypeUserNotFound, metav1.ConditionTrue, EventReasonUserNotFound, msg)
		r.setStatus(ctx, rb, "Pending", metav1.ConditionFalse, EventReasonUserNotFound, msg, nil)
		r.Recorder.Eventf(rb, corev1.EventTypeWarning, EventReasonUserNotFound, "user %q does not exist", rb.Spec.Username)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, rb, ConditionTypeUserNotFound, metav1.ConditionFalse, "UserPresent", "")

	desiredRoles := normaliseRoles(rb.Spec.Roles)
	currentRoles := normaliseRoles(info.Roles)

	addRoles, dropRoles, missing := r.diffRoles(ctx, rb, desiredRoles, currentRoles)
	for _, role := range addRoles {
		if err := nc.GrantRoleToUser(ctx, role, rb.Spec.Username); err != nil {
			return r.fail(ctx, rb, fmt.Sprintf("grant %q failed", role), err, requeue)
		}
	}
	for _, role := range dropRoles {
		if err := nc.RevokeRoleFromUser(ctx, role, rb.Spec.Username); err != nil {
			return r.fail(ctx, rb, fmt.Sprintf("revoke %q failed", role), err, requeue)
		}
	}
	if len(addRoles) > 0 {
		r.Recorder.Eventf(rb, corev1.EventTypeNormal, EventReasonRolesGranted, "granted %v", addRoles)
	}
	if len(dropRoles) > 0 {
		r.Recorder.Eventf(rb, corev1.EventTypeNormal, EventReasonRolesRevoked, "revoked %v", dropRoles)
	}

	// Re-read for the GrantedRoles status field. This is the intersection of
	// desiredRoles and the user's actual current roles.
	finalRoles, err := nc.ListUserRoles(ctx, rb.Spec.Username)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to read final role list (non-fatal)")
		finalRoles = currentRoles
	}
	finalRoles = normaliseRoles(finalRoles)

	desiredSet := stringSet(desiredRoles)
	granted := make([]string, 0, len(desiredRoles))
	for _, role := range finalRoles {
		if _, ok := desiredSet[role]; ok {
			granted = append(granted, role)
		}
	}
	sort.Strings(granted)

	if len(missing) > 0 {
		r.setNamedCondition(ctx, rb, ConditionTypePendingDependencies, metav1.ConditionTrue, ConditionReasonRolesPending,
			fmt.Sprintf("waiting for roles to exist: %v", missing))
		r.Recorder.Eventf(rb, corev1.EventTypeWarning, EventReasonRolePending,
			"waiting for roles to exist: %v", missing)
	} else {
		r.setNamedCondition(ctx, rb, ConditionTypePendingDependencies, metav1.ConditionFalse, "AllDependenciesPresent", "")
	}

	allDesiredGranted := stringSlicesEqualSorted(desiredRoles, granted)
	if allDesiredGranted {
		r.setNamedCondition(ctx, rb, ConditionTypeRolesSynced, metav1.ConditionTrue, ConditionReasonRolesSynced, "all desired roles granted")
	} else {
		r.setNamedCondition(ctx, rb, ConditionTypeRolesSynced, metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("granted %v of desired %v", granted, desiredRoles))
	}

	if allDesiredGranted && len(missing) == 0 {
		// Distinguish first-time success ("Created") from a subsequent
		// successful reconcile after a spec change ("Updated"). Both
		// previously went unannounced.
		switch {
		case rb.Status.Phase != "Ready":
			// First time this binding has reached Ready (either fresh CR
			// or recovering from a previous failure). EventReasonBindingCreated
			// names the initial-grant transition.
			r.Recorder.Eventf(rb, corev1.EventTypeNormal, EventReasonBindingCreated,
				"RoleBinding for user %q ready with %d roles", rb.Spec.Username, len(granted))
		case !stringSlicesEqualSorted(rb.Status.GrantedRoles, granted):
			// Already Ready before, but the granted-role set has changed —
			// either spec.roles edited, or an externally granted/revoked role
			// has been reconciled.
			r.Recorder.Eventf(rb, corev1.EventTypeNormal, EventReasonBindingUpdated,
				"RoleBinding for user %q updated; granted roles now %v", rb.Spec.Username, granted)
		}
		r.setStatus(ctx, rb, "Ready", metav1.ConditionTrue, "BindingReady",
			fmt.Sprintf("user %q has all %d desired roles", rb.Spec.Username, len(granted)),
			granted)
	} else {
		r.setStatus(ctx, rb, "Pending", metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("user %q has %d of %d desired roles", rb.Spec.Username, len(granted), len(desiredRoles)),
			granted)
	}

	return ctrl.Result{RequeueAfter: requeue}, nil
}

func (r *Neo4jRoleBindingReconciler) handleDeletion(ctx context.Context, rb *neo4jv1beta1.Neo4jRoleBinding) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(rb, Neo4jRoleBindingFinalizer) {
		return ctrl.Result{}, nil
	}
	requeue := r.requeueAfter()

	if strings.EqualFold(rb.Spec.DeletionPolicy, "Retain") {
		controllerutil.RemoveFinalizer(rb, Neo4jRoleBindingFinalizer)
		return ctrl.Result{}, r.Update(ctx, rb)
	}

	target, err := ResolveClusterRef(ctx, r.Client, rb.Namespace, rb.Spec.ClusterRef)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		controllerutil.RemoveFinalizer(rb, Neo4jRoleBindingFinalizer)
		return ctrl.Result{}, r.Update(ctx, rb)
	}
	if !target.IsReady() {
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	nc, err := target.NewClient(r.Client)
	if err != nil {
		logger.Error(err, "cannot connect during binding deletion; releasing finalizer")
		controllerutil.RemoveFinalizer(rb, Neo4jRoleBindingFinalizer)
		return ctrl.Result{}, r.Update(ctx, rb)
	}
	defer func() { _ = nc.Close() }()

	// Revoke only the roles previously recorded as granted by this binding.
	// We never touch roles we did not add — that's the whole point of
	// non-exclusive bindings.
	for _, role := range rb.Status.GrantedRoles {
		if err := nc.RevokeRoleFromUser(ctx, role, rb.Spec.Username); err != nil {
			r.Recorder.Eventf(rb, corev1.EventTypeWarning, EventReasonBindingFailed,
				"revoke %q from %q failed: %v", role, rb.Spec.Username, err)
			return ctrl.Result{RequeueAfter: requeue}, err
		}
	}
	r.Recorder.Eventf(rb, corev1.EventTypeNormal, EventReasonBindingDeleted,
		"revoked %d role(s) from user %q", len(rb.Status.GrantedRoles), rb.Spec.Username)

	controllerutil.RemoveFinalizer(rb, Neo4jRoleBindingFinalizer)
	return ctrl.Result{}, r.Update(ctx, rb)
}

// diffRoles returns the role grants to add and revoke. When EnforceExclusive
// is false (default), only the desired roles are considered for revoke;
// extra roles granted by other means are left alone. PUBLIC is always
// excluded from both directions.
func (r *Neo4jRoleBindingReconciler) diffRoles(ctx context.Context, rb *neo4jv1beta1.Neo4jRoleBinding, desired, current []string) (add, drop, missing []string) {
	currentSet := stringSet(current)
	desiredSet := stringSet(desired)

	for _, role := range desired {
		if validation.IsBuiltInRole(role) {
			continue
		}
		if _, ok := currentSet[role]; ok {
			continue
		}
		if r.roleResourceExists(ctx, rb.Namespace, rb.Spec.ClusterRef, role) {
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

	if rb.Spec.EnforceExclusive {
		for _, c := range current {
			if strings.EqualFold(c, "PUBLIC") {
				continue
			}
			if _, ok := desiredSet[c]; !ok {
				drop = append(drop, c)
			}
		}
	}

	// Revoke roles previously granted by this binding that have since been
	// removed from .spec.roles, even when not exclusive.
	previouslyGranted := stringSet(rb.Status.GrantedRoles)
	for prev := range previouslyGranted {
		if _, ok := desiredSet[prev]; ok {
			continue
		}
		if strings.EqualFold(prev, "PUBLIC") {
			continue
		}
		if containsString(drop, prev) {
			continue
		}
		drop = append(drop, prev)
	}

	sort.Strings(add)
	sort.Strings(drop)
	sort.Strings(missing)
	return add, drop, missing
}

func (r *Neo4jRoleBindingReconciler) roleResourceExists(ctx context.Context, namespace, clusterRef, roleName string) bool {
	list := &neo4jv1beta1.Neo4jRoleList{}
	if err := r.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return false
	}
	for i := range list.Items {
		role := &list.Items[i]
		if role.Spec.ClusterRef != clusterRef {
			continue
		}
		name := role.Spec.Name
		if name == "" {
			name = role.Name
		}
		if name == roleName {
			return true
		}
	}
	return false
}

func (r *Neo4jRoleBindingReconciler) requeueAfter() time.Duration {
	if r.RequeueAfter > 0 {
		return r.RequeueAfter
	}
	return 30 * time.Second
}

func (r *Neo4jRoleBindingReconciler) fail(ctx context.Context, rb *neo4jv1beta1.Neo4jRoleBinding, label string, err error, requeue time.Duration) (ctrl.Result, error) {
	msg := label
	if err != nil {
		msg = fmt.Sprintf("%s: %v", label, err)
	}
	r.setStatus(ctx, rb, "Failed", metav1.ConditionFalse, EventReasonBindingFailed, msg, nil)
	r.Recorder.Event(rb, corev1.EventTypeWarning, EventReasonBindingFailed, msg)
	return ctrl.Result{RequeueAfter: requeue}, err
}

func (r *Neo4jRoleBindingReconciler) setStatus(
	ctx context.Context,
	rb *neo4jv1beta1.Neo4jRoleBinding,
	phase string,
	readyStatus metav1.ConditionStatus,
	readyReason, message string,
	grantedRoles []string,
) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jRoleBinding{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(rb), latest); err != nil {
			return err
		}
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, readyStatus, readyReason, message)
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		if grantedRoles != nil {
			latest.Status.GrantedRoles = grantedRoles
		}
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to update Neo4jRoleBinding status")
	}
}

func (r *Neo4jRoleBindingReconciler) setNamedCondition(ctx context.Context, rb *neo4jv1beta1.Neo4jRoleBinding, condType string, status metav1.ConditionStatus, reason, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jRoleBinding{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(rb), latest); err != nil {
			return err
		}
		SetNamedCondition(&latest.Status.Conditions, condType, latest.Generation, status, reason, message)
		latest.Status.ObservedGeneration = latest.Generation
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to set condition on Neo4jRoleBinding", "condition", condType)
	}
}

// SetupWithManager registers the controller and wires up watches:
//   - Neo4jRole: bindings pending on a missing role re-reconcile when the
//     role lands.
//   - Neo4jEnterpriseCluster / Neo4jEnterpriseStandalone: re-reconcile every
//     binding pointing at a changed cluster, so bindings react immediately
//     to the cluster's Ready condition flipping (otherwise we wait up to
//     30s for the next requeue).
func (r *Neo4jRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c := mgr.GetClient()
	enqueueBindingsForCluster := EnqueueDependentsForClusterChange(
		c,
		func() client.ObjectList { return &neo4jv1beta1.Neo4jRoleBindingList{} },
		func(list client.ObjectList, emit func(name, namespace, clusterRef string)) {
			bindings, ok := list.(*neo4jv1beta1.Neo4jRoleBindingList)
			if !ok {
				return
			}
			for i := range bindings.Items {
				b := &bindings.Items[i]
				emit(b.Name, b.Namespace, b.Spec.ClusterRef)
			}
		},
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jRoleBinding{}).
		Watches(&neo4jv1beta1.Neo4jEnterpriseCluster{}, enqueueBindingsForCluster).
		Watches(&neo4jv1beta1.Neo4jEnterpriseStandalone{}, enqueueBindingsForCluster).
		Watches(&neo4jv1beta1.Neo4jRole{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			role, ok := obj.(*neo4jv1beta1.Neo4jRole)
			if !ok {
				return nil
			}
			roleName := role.Spec.Name
			if roleName == "" {
				roleName = role.Name
			}
			bindings := &neo4jv1beta1.Neo4jRoleBindingList{}
			if err := c.List(ctx, bindings, client.InNamespace(role.Namespace)); err != nil {
				return nil
			}
			var reqs []reconcile.Request
			for i := range bindings.Items {
				b := &bindings.Items[i]
				if b.Spec.ClusterRef != role.Spec.ClusterRef {
					continue
				}
				for _, rname := range b.Spec.Roles {
					if rname == roleName {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: b.Namespace, Name: b.Name}})
						break
					}
				}
			}
			return reqs
		})).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}

// stringSet builds a presence map from a string slice.
func stringSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}
