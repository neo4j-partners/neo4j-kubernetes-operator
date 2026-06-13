/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func roleResolutionClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func roleCR(metaName, ns, clusterRef, specName string) *neo4jv1beta1.Neo4jRole {
	return &neo4jv1beta1.Neo4jRole{
		ObjectMeta: metav1.ObjectMeta{Name: metaName, Namespace: ns},
		Spec:       neo4jv1beta1.Neo4jRoleSpec{ClusterRef: clusterRef, Name: specName},
	}
}

// #260: a hyphenated CR metadata.name resolves to the role's underscore
// spec.name; literal Neo4j names (built-ins, the spec.name itself, unknowns)
// pass through unchanged.
func TestResolveRoleNames_CRNameResolvesToSpecName(t *testing.T) {
	c := roleResolutionClient(t, roleCR("analytics-reader", "prod", "prod-cluster", "analytics_reader"))

	out, resolved := resolveRoleNames(context.Background(), c, "prod", "prod-cluster",
		[]string{"analytics-reader", "editor", "externally_made"})

	assert.ElementsMatch(t, []string{"analytics_reader", "editor", "externally_made"}, out)
	assert.Equal(t, map[string]string{"analytics-reader": "analytics_reader"}, resolved,
		"only the CR-name entry should be reported as resolved")
}

// The spec.name itself (the real Neo4j role name) must pass through literally —
// literal-first means an entry that is already an effective role name is never
// rewritten, even though a CR with a different metadata.name exists.
func TestResolveRoleNames_SpecNamePassesThroughLiterally(t *testing.T) {
	c := roleResolutionClient(t, roleCR("analytics-reader", "prod", "prod-cluster", "analytics_reader"))

	out, resolved := resolveRoleNames(context.Background(), c, "prod", "prod-cluster",
		[]string{"analytics_reader"})

	assert.Equal(t, []string{"analytics_reader"}, out)
	assert.Empty(t, resolved)
}

// A CR with no spec.name uses its metadata.name as the effective name — there
// is nothing to resolve, so the name passes through and existence holds.
func TestResolveRoleNames_NoSpecNameIsIdentity(t *testing.T) {
	c := roleResolutionClient(t, roleCR("plainrole", "prod", "prod-cluster", ""))

	out, resolved := resolveRoleNames(context.Background(), c, "prod", "prod-cluster", []string{"plainrole"})
	assert.Equal(t, []string{"plainrole"}, out)
	assert.Empty(t, resolved)

	assert.True(t, roleNameExists(context.Background(), c, "prod", "prod-cluster", "plainrole"))
}

// Resolution is scoped: a CR in another namespace or pointing at another
// clusterRef must NOT resolve, and the name passes through (to be reported
// missing downstream).
func TestResolveRoleNames_ScopedByNamespaceAndClusterRef(t *testing.T) {
	c := roleResolutionClient(t,
		roleCR("analytics-reader", "other-ns", "prod-cluster", "analytics_reader"),
		roleCR("billing-reader", "prod", "other-cluster", "billing_reader"),
	)

	out, resolved := resolveRoleNames(context.Background(), c, "prod", "prod-cluster",
		[]string{"analytics-reader", "billing-reader"})

	assert.ElementsMatch(t, []string{"analytics-reader", "billing-reader"}, out,
		"out-of-scope CRs must not resolve")
	assert.Empty(t, resolved)
}

// roleNameExists matches the effective Neo4j name (spec.name) but NOT the CR
// metadata.name — existence is about the Neo4j role, while resolution bridges
// the CR name to it.
func TestRoleNameExists_MatchesEffectiveNameOnly(t *testing.T) {
	c := roleResolutionClient(t, roleCR("analytics-reader", "prod", "prod-cluster", "analytics_reader"))

	assert.True(t, roleNameExists(context.Background(), c, "prod", "prod-cluster", "analytics_reader"))
	assert.False(t, roleNameExists(context.Background(), c, "prod", "prod-cluster", "analytics-reader"),
		"the CR metadata.name is not itself a Neo4j role name")
}
