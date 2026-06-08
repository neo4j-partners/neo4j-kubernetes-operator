/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

const (
	// SeedFromURIProvidersConfigKey is the neo4j.conf setting that gates which
	// seed providers Neo4j will consult when resolving a `seedURI` /
	// `seedURIs` value in `CREATE DATABASE`. The shipped default is
	// "CloudSeedProvider" — sufficient for s3/gs/azb but not for http URLs
	// served by the operator-managed PVC seed proxy.
	SeedFromURIProvidersConfigKey = "dbms.databases.seed_from_uri_providers"

	// URLConnectionSeedProviderName is the class-name token that must be
	// present in SeedFromURIProvidersConfigKey for Neo4j to accept
	// http/https/ftp seed URIs (URLConnectionSeedProvider on the classpath
	// only registers if listed here).
	URLConnectionSeedProviderName = "URLConnectionSeedProvider"

	// CloudSeedProviderName is the default — kept when we auto-extend the
	// list so we don't silently drop the s3/gs/azb providers users may also
	// be relying on.
	CloudSeedProviderName = "CloudSeedProvider"

	// AutoEnableURLSeedProviderAnnotation is set on the cluster CR to
	// authorise the operator to append URLConnectionSeedProvider to
	// spec.config[SeedFromURIProvidersConfigKey] when a PVC-backed
	// sharded-DB seedBackupRef needs it. Mirrors the auto-inherit-seed-creds
	// pattern: the patch triggers a rolling restart of cluster pods (config
	// is mounted as a ConfigMap and Neo4j reads it at startup), so the
	// cluster owner — not the sharded-DB owner — is the right party to opt
	// in.
	AutoEnableURLSeedProviderAnnotation = "neo4j.com/auto-enable-url-seed-provider"
)

// EnsureClusterHasURLSeedProvider validates that the referenced cluster's
// spec.config[dbms.databases.seed_from_uri_providers] includes
// URLConnectionSeedProvider — required for `CREATE DATABASE … OPTIONS {
// seedURIs: { … http://… } }` to succeed when the operator is serving
// shard backups via the in-cluster HTTP proxy (PVC-backed seedBackupRef).
//
// Returns:
//   - (autoEnabled=false, nil)  when the provider is already configured
//     (no action taken).
//   - (autoEnabled=true, nil)   after the operator has appended the
//     provider to the cluster's spec.config under the auto-enable
//     annotation. The caller should treat this as transient — the
//     cluster controller now needs to roll out the StatefulSet with
//     the updated ConfigMap.
//   - (autoEnabled=false, actionableErr)  when the provider is absent and
//     the cluster lacks the auto-enable annotation. The error message is a
//     copy-pasteable snippet directing the user to update their cluster
//     CR.
//
// Without this check, the user gets the cryptic Neo4j error "No seed
// providers found to satisfy the provided uri 'http://…'" the first time
// they try a PVC-backed sharded restore — a Neo4j-side failure mode that
// happens long after the operator's reconcile loop says everything looks
// fine.
func EnsureClusterHasURLSeedProvider(
	ctx context.Context,
	c client.Client,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (autoEnabled bool, err error) {
	current := cluster.Spec.Config[SeedFromURIProvidersConfigKey]
	if providerListContains(current, URLConnectionSeedProviderName) {
		return false, nil
	}

	if cluster.Annotations[AutoEnableURLSeedProviderAnnotation] != "true" {
		return false, fmt.Errorf(
			"cluster %q does not enable URLConnectionSeedProvider, which is required for PVC-backed seedBackupRef restores.\n"+
				"Either:\n"+
				"  1. Add this to the cluster's spec.config:\n"+
				"     %s: %q\n"+
				"  2. Or set annotation `%s: \"true\"` on the cluster to let the operator add it automatically (triggers a rolling restart of cluster pods).",
			cluster.Name,
			SeedFromURIProvidersConfigKey,
			joinProviderList(current, URLConnectionSeedProviderName),
			AutoEnableURLSeedProviderAnnotation,
		)
	}

	if cluster.Spec.Config == nil {
		cluster.Spec.Config = map[string]string{}
	}
	cluster.Spec.Config[SeedFromURIProvidersConfigKey] = joinProviderList(current, URLConnectionSeedProviderName)
	if err := c.Update(ctx, cluster); err != nil {
		return false, fmt.Errorf("auto-enable URLConnectionSeedProvider on cluster %q: %w", cluster.Name, err)
	}
	return true, nil
}

func providerListContains(list, name string) bool {
	for _, p := range strings.Split(list, ",") {
		if strings.TrimSpace(p) == name {
			return true
		}
	}
	return false
}

// joinProviderList preserves existing entries (deduped) and appends the
// new name. Empty list defaults to "CloudSeedProvider,<name>" so we don't
// silently strip the shipped default when extending.
func joinProviderList(existing, add string) string {
	seen := map[string]bool{}
	out := []string{}
	push := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	if existing == "" {
		push(CloudSeedProviderName)
	} else {
		for _, p := range strings.Split(existing, ",") {
			push(p)
		}
	}
	push(add)
	return strings.Join(out, ",")
}
