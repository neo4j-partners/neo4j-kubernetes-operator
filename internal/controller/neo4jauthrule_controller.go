/*
Copyright 2026.

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
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jAuthRuleFinalizer guards the controller's chance to drop the rule (or
// release the finalizer when DeletionPolicy is Retain) before the CR is
// removed.
const Neo4jAuthRuleFinalizer = "neo4j.com/authrule-finalizer"

// abacAuthorizationProvidersKey is the Neo4j configuration key that names
// the OIDC provider(s) wired up for ABAC. Auth rules don't function without
// at least one provider listed here.
const abacAuthorizationProvidersKey = "dbms.security.abac.authorization_providers"

// ConditionTypeAuthRule* extends the shared condition vocabulary with
// rule-specific conditions. Defined locally rather than in events.go because
// they are only meaningful on Neo4jAuthRule status.
const (
	ConditionTypeOIDCProviderConfigured = "OIDCProviderConfigured"
	ConditionTypeAuthRuleVersionTooOld  = "AuthRuleVersionTooOld"
	ConditionReasonAuthRuleReady        = "AuthRuleReady"
	ConditionReasonAuthRuleVersionGate  = "ClusterTooOld"
	ConditionReasonOIDCNotConfigured    = "OIDCProviderNotConfigured"
)

// Neo4jAuthRuleReconciler reconciles a Neo4jAuthRule resource.
type Neo4jAuthRuleReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	MaxConcurrentReconciles int
	RequeueAfter            time.Duration
	Validator               *validation.AuthRuleValidator
}

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jauthrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jauthrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jauthrules/finalizers,verbs=update

// Reconcile drives a Neo4jAuthRule toward its desired state.
func (r *Neo4jAuthRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("neo4jauthrule", req.NamespacedName)

	rule := &neo4jv1beta1.Neo4jAuthRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	requeue := r.requeueAfter()

	if rule.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, rule)
	}

	if !controllerutil.ContainsFinalizer(rule, Neo4jAuthRuleFinalizer) {
		controllerutil.AddFinalizer(rule, Neo4jAuthRuleFinalizer)
		if err := r.Update(ctx, rule); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate
	if r.Validator != nil {
		res := r.Validator.Validate(ctx, rule)
		for _, w := range res.Warnings {
			r.Recorder.Eventf(rule, corev1.EventTypeWarning, EventReasonValidationWarning, "%s", w)
		}
		if len(res.Errors) > 0 {
			msg := fmt.Sprintf("validation failed: %s", res.Errors.ToAggregate().Error())
			r.setStatus(ctx, rule, "Failed", metav1.ConditionFalse, EventReasonValidationFailed, msg, nil, nil)
			r.Recorder.Event(rule, corev1.EventTypeWarning, EventReasonValidationFailed, msg)
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
	}

	// Resolve cluster
	target, err := ResolveClusterRef(ctx, r.Client, rule.Namespace, rule.Spec.ClusterRef)
	if err != nil {
		logger.Error(err, "failed to resolve clusterRef")
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found {
		msg := fmt.Sprintf("clusterRef %q not found", rule.Spec.ClusterRef)
		r.setStatus(ctx, rule, "Pending", metav1.ConditionFalse, EventReasonClusterNotFound, msg, nil, nil)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	if !target.IsReady() {
		msg := fmt.Sprintf("clusterRef %q is not Ready", rule.Spec.ClusterRef)
		r.setNamedCondition(ctx, rule, ConditionTypeClusterNotReady, metav1.ConditionTrue, ConditionReasonClusterNotReady, msg)
		r.setStatus(ctx, rule, "Pending", metav1.ConditionFalse, ConditionReasonClusterNotReady, msg, nil, nil)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, rule, ConditionTypeClusterNotReady, metav1.ConditionFalse, "ClusterReady", "")

	// Version gate — auth rules require Neo4j 2026.03+.
	if !targetSupportsAuthRules(target) {
		msg := fmt.Sprintf("Neo4jAuthRule requires Neo4j 2026.03 or later; cluster %q runs %s. See https://neo4j.com/docs/operations-manual/current/authentication-authorization/attribute-based-access-control/.",
			rule.Spec.ClusterRef, targetVersionString(target))
		r.setNamedCondition(ctx, rule, ConditionTypeAuthRuleVersionTooOld, metav1.ConditionTrue, ConditionReasonAuthRuleVersionGate, msg)
		r.setStatus(ctx, rule, "Failed", metav1.ConditionFalse, EventReasonAuthRuleVersionTooOld, msg, nil, nil)
		r.Recorder.Event(rule, corev1.EventTypeWarning, EventReasonAuthRuleVersionTooOld, msg)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, rule, ConditionTypeAuthRuleVersionTooOld, metav1.ConditionFalse, "VersionSupported", "")

	// OIDC provider configuration check (Option A from the design proposal:
	// document as prerequisite, surface a clear status condition rather
	// than mutating the cluster's spec.config).
	if !targetHasABACProvider(target) {
		msg := fmt.Sprintf(
			"clusterRef %q does not configure %s in spec.config. ABAC requires at least one OIDC provider name listed there. See docs/api_reference/neo4jauthrule.md for setup.",
			rule.Spec.ClusterRef, abacAuthorizationProvidersKey,
		)
		r.setNamedCondition(ctx, rule, ConditionTypeOIDCProviderConfigured, metav1.ConditionFalse, ConditionReasonOIDCNotConfigured, msg)
		r.setStatus(ctx, rule, "Pending", metav1.ConditionFalse, EventReasonOIDCProviderNotConfigured, msg, nil, nil)
		r.Recorder.Event(rule, corev1.EventTypeWarning, EventReasonOIDCProviderNotConfigured, msg)
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	r.setNamedCondition(ctx, rule, ConditionTypeOIDCProviderConfigured, metav1.ConditionTrue, "OIDCConfigured", "")

	// Connect.
	nc, err := target.NewClient(r.Client)
	if err != nil {
		msg := fmt.Sprintf("failed to connect to Neo4j: %v", err)
		r.setStatus(ctx, rule, "Failed", metav1.ConditionFalse, EventReasonConnectionFailed, msg, nil, nil)
		r.Recorder.Event(rule, corev1.EventTypeWarning, EventReasonConnectionFailed, msg)
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	defer func() { _ = nc.Close() }()

	ruleName := rule.Spec.Name
	if ruleName == "" {
		ruleName = rule.Name
	}
	desiredCondition := strings.TrimSpace(rule.Spec.Condition)
	desiredEnabled := true
	if rule.Spec.Enabled != nil {
		desiredEnabled = *rule.Spec.Enabled
	}
	desiredRoles := normaliseRoles(rule.Spec.GrantedRoles)

	// Verify granted roles exist (either as Neo4jRole CRs in this namespace
	// or as live roles in Neo4j). Roles missing from both are reported via
	// PendingDependencies; we still proceed with whatever roles are present.
	missingRoles, err := r.findMissingRoles(ctx, nc, rule, desiredRoles)
	if err != nil {
		return r.fail(ctx, rule, "role lookup failed", err, requeue)
	}
	if len(missingRoles) > 0 {
		msg := fmt.Sprintf("waiting for roles to exist: %v", missingRoles)
		r.setNamedCondition(ctx, rule, ConditionTypePendingDependencies, metav1.ConditionTrue, ConditionReasonRolesPending, msg)
		r.Recorder.Eventf(rule, corev1.EventTypeWarning, EventReasonRolePending, "%s", msg)
	} else {
		r.setNamedCondition(ctx, rule, ConditionTypePendingDependencies, metav1.ConditionFalse, "AllDependenciesPresent", "")
	}

	// Read live state.
	live, err := nc.ShowAuthRule(ctx, ruleName)
	if err != nil {
		return r.fail(ctx, rule, "SHOW AUTH RULES failed", err, requeue)
	}

	// Create-or-replace (which subsumes "alter" semantics) is the simplest
	// path when the live condition or enabled flag drift from spec. The
	// condition string is interpolated as-is; the validator has already
	// rejected DDL keywords so the surface is bounded to expression syntax.
	if live == nil || live.Condition != desiredCondition || live.Enabled != desiredEnabled {
		if err := nc.CreateOrReplaceAuthRule(ctx, ruleName, desiredCondition, desiredEnabled); err != nil {
			return r.fail(ctx, rule, "CREATE OR REPLACE AUTH RULE failed", err, requeue)
		}
		if live == nil {
			r.Recorder.Eventf(rule, corev1.EventTypeNormal, EventReasonAuthRuleCreated, "auth rule %q created", ruleName)
		} else {
			r.Recorder.Eventf(rule, corev1.EventTypeNormal, EventReasonAuthRuleUpdated, "auth rule %q condition or enabled flag updated", ruleName)
		}
	}

	// Diff role grants. CREATE OR REPLACE clears existing role grants on
	// the rule, so when we just re-created the rule we always re-grant the
	// full desired set.
	currentRoles := []string{}
	if live != nil {
		currentRoles = live.Roles
	}
	if live == nil || live.Condition != desiredCondition || live.Enabled != desiredEnabled {
		// CREATE OR REPLACE wiped any prior grants — reset baseline.
		currentRoles = []string{}
	}

	add, drop := diffRolesSimple(desiredRoles, currentRoles, missingRoles, rule.Spec.EnforceRoles)
	if len(add) > 0 {
		if err := nc.GrantRolesToAuthRule(ctx, ruleName, add); err != nil {
			return r.fail(ctx, rule, fmt.Sprintf("grant %v failed", add), err, requeue)
		}
		r.Recorder.Eventf(rule, corev1.EventTypeNormal, EventReasonRolesGranted, "granted %v to auth rule %q", add, ruleName)
	}
	if len(drop) > 0 {
		if err := nc.RevokeRolesFromAuthRule(ctx, ruleName, drop); err != nil {
			return r.fail(ctx, rule, fmt.Sprintf("revoke %v failed", drop), err, requeue)
		}
		r.Recorder.Eventf(rule, corev1.EventTypeNormal, EventReasonRolesRevoked, "revoked %v from auth rule %q", drop, ruleName)
	}

	// Re-read for status truth.
	final, err := nc.ShowAuthRule(ctx, ruleName)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to re-read auth rule after reconcile (non-fatal)")
		final = &neo4j.AuthRuleInfo{Name: ruleName, Condition: desiredCondition, Enabled: desiredEnabled, Roles: desiredRoles}
	}

	appliedRoles := append([]string(nil), final.Roles...)
	sort.Strings(appliedRoles)
	allDesiredGranted := stringSlicesEqualSorted(desiredRoles, appliedRoles)

	if allDesiredGranted && len(missingRoles) == 0 {
		r.setNamedCondition(ctx, rule, ConditionTypeRolesSynced, metav1.ConditionTrue, ConditionReasonRolesSynced, "all granted roles in sync")
		r.setStatus(ctx, rule, "Ready", metav1.ConditionTrue, ConditionReasonAuthRuleReady,
			fmt.Sprintf("auth rule %q is in sync (enabled=%t, roles=%v)", ruleName, final.Enabled, appliedRoles),
			appliedRoles, &final.Enabled)
	} else if len(missingRoles) > 0 {
		r.setStatus(ctx, rule, "PendingDependencies", metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("auth rule %q applied; %d/%d granted, waiting on missing roles %v", ruleName, len(appliedRoles), len(desiredRoles), missingRoles),
			appliedRoles, &final.Enabled)
	} else {
		r.setNamedCondition(ctx, rule, ConditionTypeRolesSynced, metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("granted %v of desired %v", appliedRoles, desiredRoles))
		r.setStatus(ctx, rule, "Pending", metav1.ConditionFalse, ConditionReasonRolesPending,
			fmt.Sprintf("auth rule %q has %d of %d desired role grants", ruleName, len(appliedRoles), len(desiredRoles)),
			appliedRoles, &final.Enabled)
	}

	return ctrl.Result{RequeueAfter: requeue}, nil
}

func (r *Neo4jAuthRuleReconciler) handleDeletion(ctx context.Context, rule *neo4jv1beta1.Neo4jAuthRule) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(rule, Neo4jAuthRuleFinalizer) {
		return ctrl.Result{}, nil
	}
	requeue := r.requeueAfter()

	if strings.EqualFold(rule.Spec.DeletionPolicy, "Retain") {
		controllerutil.RemoveFinalizer(rule, Neo4jAuthRuleFinalizer)
		return ctrl.Result{}, r.Update(ctx, rule)
	}

	target, err := ResolveClusterRef(ctx, r.Client, rule.Namespace, rule.Spec.ClusterRef)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	if !target.Found || !target.IsReady() || !targetSupportsAuthRules(target) {
		// Cluster gone, not ready, or too old to host auth rules — nothing
		// for us to drop. Release the finalizer.
		controllerutil.RemoveFinalizer(rule, Neo4jAuthRuleFinalizer)
		return ctrl.Result{}, r.Update(ctx, rule)
	}

	nc, err := target.NewClient(r.Client)
	if err != nil {
		logger.Error(err, "cannot connect during auth rule deletion; releasing finalizer")
		controllerutil.RemoveFinalizer(rule, Neo4jAuthRuleFinalizer)
		return ctrl.Result{}, r.Update(ctx, rule)
	}
	defer func() { _ = nc.Close() }()

	ruleName := rule.Spec.Name
	if ruleName == "" {
		ruleName = rule.Name
	}
	if err := nc.DropAuthRuleIfExists(ctx, ruleName); err != nil {
		r.Recorder.Eventf(rule, corev1.EventTypeWarning, EventReasonAuthRuleFailed,
			"DROP AUTH RULE %q failed: %v", ruleName, err)
		return ctrl.Result{RequeueAfter: requeue}, err
	}
	r.Recorder.Eventf(rule, corev1.EventTypeNormal, EventReasonAuthRuleDeleted, "auth rule %q dropped", ruleName)

	controllerutil.RemoveFinalizer(rule, Neo4jAuthRuleFinalizer)
	return ctrl.Result{}, r.Update(ctx, rule)
}

// findMissingRoles returns the subset of desired role names that exist
// neither as a Neo4jRole CR in the same namespace nor as a live role in
// Neo4j. Built-in roles are always considered to exist.
func (r *Neo4jAuthRuleReconciler) findMissingRoles(ctx context.Context, nc *neo4j.Client, rule *neo4jv1beta1.Neo4jAuthRule, desired []string) ([]string, error) {
	if len(desired) == 0 {
		return nil, nil
	}
	liveRoles, err := nc.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	livePresent := make(map[string]struct{}, len(liveRoles))
	for _, lr := range liveRoles {
		livePresent[lr.Role] = struct{}{}
	}
	var missing []string
	for _, name := range desired {
		if validation.IsBuiltInRole(name) {
			continue
		}
		if _, ok := livePresent[name]; ok {
			continue
		}
		if r.roleResourceExists(ctx, rule.Namespace, rule.Spec.ClusterRef, name) {
			continue
		}
		missing = append(missing, name)
	}
	return missing, nil
}

// roleResourceExists reports whether a Neo4jRole CR with the given Neo4j
// role name exists in the same namespace and points at the same cluster.
func (r *Neo4jAuthRuleReconciler) roleResourceExists(ctx context.Context, namespace, clusterRef, roleName string) bool {
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

// diffRolesSimple computes the add/drop sets given desired and current role
// slices. Roles in `missing` are excluded from the add set so we don't try
// (and fail) to grant a non-existent role. When enforceExclusive is false,
// the drop set is empty (we never revoke anything not in `desired`).
func diffRolesSimple(desired, current, missing []string, enforceExclusive bool) (add, drop []string) {
	missingSet := stringSet(missing)
	currentSet := stringSet(current)
	desiredSet := stringSet(desired)

	for _, d := range desired {
		if _, ok := missingSet[d]; ok {
			continue
		}
		if _, ok := currentSet[d]; ok {
			continue
		}
		add = append(add, d)
	}
	if enforceExclusive {
		for _, c := range current {
			if _, ok := desiredSet[c]; !ok {
				drop = append(drop, c)
			}
		}
	}
	sort.Strings(add)
	sort.Strings(drop)
	return add, drop
}

func (r *Neo4jAuthRuleReconciler) requeueAfter() time.Duration {
	if r.RequeueAfter > 0 {
		return r.RequeueAfter
	}
	return 30 * time.Second
}

func (r *Neo4jAuthRuleReconciler) fail(ctx context.Context, rule *neo4jv1beta1.Neo4jAuthRule, label string, err error, requeue time.Duration) (ctrl.Result, error) {
	msg := label
	if err != nil {
		msg = fmt.Sprintf("%s: %v", label, err)
	}
	r.setStatus(ctx, rule, "Failed", metav1.ConditionFalse, EventReasonAuthRuleFailed, msg, nil, nil)
	r.Recorder.Event(rule, corev1.EventTypeWarning, EventReasonAuthRuleFailed, msg)
	return ctrl.Result{RequeueAfter: requeue}, err
}

func (r *Neo4jAuthRuleReconciler) setStatus(
	ctx context.Context,
	rule *neo4jv1beta1.Neo4jAuthRule,
	phase string,
	readyStatus metav1.ConditionStatus,
	readyReason, message string,
	appliedRoles []string,
	appliedEnabled *bool,
) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jAuthRule{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(rule), latest); err != nil {
			return err
		}
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, readyStatus, readyReason, message)
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		if appliedRoles != nil {
			latest.Status.AppliedRoles = appliedRoles
		}
		if appliedEnabled != nil {
			b := *appliedEnabled
			latest.Status.AppliedEnabled = &b
		}
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to update Neo4jAuthRule status")
	}
}

func (r *Neo4jAuthRuleReconciler) setNamedCondition(ctx context.Context, rule *neo4jv1beta1.Neo4jAuthRule, condType string, status metav1.ConditionStatus, reason, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jAuthRule{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(rule), latest); err != nil {
			return err
		}
		SetNamedCondition(&latest.Status.Conditions, condType, latest.Generation, status, reason, message)
		latest.Status.ObservedGeneration = latest.Generation
		return r.Status().Update(ctx, latest)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		log.FromContext(ctx).Error(err, "failed to set condition on Neo4jAuthRule", "condition", condType)
	}
}

// targetSupportsAuthRules parses the cluster's reported Neo4j version and
// returns true only when it satisfies the AUTH RULE feature gate.
func targetSupportsAuthRules(target ResolvedTarget) bool {
	v := targetVersionString(target)
	if v == "" {
		return false
	}
	parsed, err := neo4j.ParseVersion(v)
	if err != nil || parsed == nil {
		return false
	}
	return parsed.SupportsAuthRules()
}

// targetVersionString returns the Neo4j image tag (e.g. "2026.04.0-enterprise")
// from the resolved target's spec, or "" if not set.
func targetVersionString(target ResolvedTarget) string {
	if target.Cluster != nil {
		return target.Cluster.Spec.Image.Tag
	}
	if target.Standalone != nil {
		return target.Standalone.Spec.Image.Tag
	}
	return ""
}

// targetHasABACProvider reports whether the cluster's spec.config sets the
// abac.authorization_providers key to a non-empty value.
func targetHasABACProvider(target ResolvedTarget) bool {
	var cfg map[string]string
	if target.Cluster != nil {
		cfg = target.Cluster.Spec.Config
	} else if target.Standalone != nil {
		cfg = target.Standalone.Spec.Config
	}
	if cfg == nil {
		return false
	}
	v, ok := cfg[abacAuthorizationProvidersKey]
	if !ok {
		return false
	}
	return strings.TrimSpace(v) != ""
}

// SetupWithManager registers the controller. We watch:
//   - Neo4jRole, so rules pending on a missing role re-reconcile when it lands.
//   - Neo4jEnterpriseCluster / Neo4jEnterpriseStandalone, so we react
//     immediately to the cluster's Ready transition or version bump.
func (r *Neo4jAuthRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c := mgr.GetClient()
	enqueueRulesForCluster := EnqueueDependentsForClusterChange(
		c,
		func() client.ObjectList { return &neo4jv1beta1.Neo4jAuthRuleList{} },
		func(list client.ObjectList, emit func(name, namespace, clusterRef string)) {
			rules, ok := list.(*neo4jv1beta1.Neo4jAuthRuleList)
			if !ok {
				return
			}
			for i := range rules.Items {
				ar := &rules.Items[i]
				emit(ar.Name, ar.Namespace, ar.Spec.ClusterRef)
			}
		},
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jAuthRule{}).
		Watches(&neo4jv1beta1.Neo4jEnterpriseCluster{}, enqueueRulesForCluster).
		Watches(&neo4jv1beta1.Neo4jEnterpriseStandalone{}, enqueueRulesForCluster).
		Watches(&neo4jv1beta1.Neo4jRole{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			role, ok := obj.(*neo4jv1beta1.Neo4jRole)
			if !ok {
				return nil
			}
			roleName := role.Spec.Name
			if roleName == "" {
				roleName = role.Name
			}
			rules := &neo4jv1beta1.Neo4jAuthRuleList{}
			if err := c.List(ctx, rules, client.InNamespace(role.Namespace)); err != nil {
				return nil
			}
			var reqs []reconcile.Request
			for i := range rules.Items {
				ar := &rules.Items[i]
				if ar.Spec.ClusterRef != role.Spec.ClusterRef {
					continue
				}
				for _, n := range ar.Spec.GrantedRoles {
					if n == roleName {
						reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ar.Namespace, Name: ar.Name}})
						break
					}
				}
			}
			return reqs
		})).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}
