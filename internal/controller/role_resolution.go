/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Role-name resolution shared by the Neo4jUser and Neo4jRoleBinding
// controllers (#260). A Neo4jRole CR's metadata.name is Kubernetes-style
// (hyphens allowed) while its effective Neo4j role name (spec.name, or
// metadata.name when spec.name is empty) cannot contain hyphens — so for
// virtually every real custom role the two differ. Users naturally reference
// the CR (the GitOps object) in spec.roles, which previously left them stuck
// in RolesPending forever. These helpers let the CR name resolve to the Neo4j
// role name while keeping plain Neo4j names (built-ins, externally-created
// roles) working unchanged.

// roleCRIndex lists same-namespace Neo4jRole CRs whose spec.clusterRef matches
// clusterRef and returns two views used by role-name resolution:
//
//	effective:       the set of effective Neo4j role names — spec.name, or
//	                 metadata.name when spec.name is empty.
//	metaToEffective: metadata.name -> effective name, only for CRs where the
//	                 two differ (the GitOps CR-name -> Neo4j-name map).
//
// On a List error it returns empty maps, so callers degrade to literal
// (no-resolution) behaviour rather than failing.
func roleCRIndex(ctx context.Context, c client.Client, namespace, clusterRef string) (effective map[string]struct{}, metaToEffective map[string]string) {
	effective = map[string]struct{}{}
	metaToEffective = map[string]string{}
	list := &neo4jv1beta1.Neo4jRoleList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return effective, metaToEffective
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
		effective[name] = struct{}{}
		if name != role.Name {
			metaToEffective[role.Name] = name
		}
	}
	return effective, metaToEffective
}

// roleNameExists reports whether roleName matches a same-namespace Neo4jRole
// CR's effective Neo4j role name (spec.name, or metadata.name when spec.name
// is empty) pointing at the same clusterRef. This is the existence pre-flight
// shared by both controllers' diffRoles.
func roleNameExists(ctx context.Context, c client.Client, namespace, clusterRef, roleName string) bool {
	effective, _ := roleCRIndex(ctx, c, namespace, clusterRef)
	_, ok := effective[roleName]
	return ok
}

// resolveRoleNames maps each desired spec.roles entry to the Neo4j role name to
// grant. Literal-first: an entry that is already a built-in or an effective
// Neo4jRole name is kept as-is; only an entry that matches NO real role name but
// DOES match a same-namespace Neo4jRole CR's metadata.name is resolved to that
// CR's effective spec.name. Unknown entries pass through unchanged so diffRoles
// can report them as missing. resolved maps any rewritten input -> output for
// caller diagnostics (empty when nothing was rewritten).
func resolveRoleNames(ctx context.Context, c client.Client, namespace, clusterRef string, desired []string) (out []string, resolved map[string]string) {
	resolved = map[string]string{}
	if len(desired) == 0 {
		return desired, resolved
	}
	effective, metaToEffective := roleCRIndex(ctx, c, namespace, clusterRef)
	out = make([]string, 0, len(desired))
	for _, d := range desired {
		switch {
		case validation.IsBuiltInRole(d):
			out = append(out, d)
		default:
			if _, isEffective := effective[d]; isEffective {
				// d is already a real Neo4j role name — keep literal.
				out = append(out, d)
			} else if eff, ok := metaToEffective[d]; ok {
				// d is a CR metadata.name; resolve to its Neo4j role name.
				out = append(out, eff)
				resolved[d] = eff
			} else {
				out = append(out, d)
			}
		}
	}
	return out, resolved
}
