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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

const (
	// AutoInheritSeedCredsAnnotation is the annotation users add to a
	// Neo4jEnterpriseCluster to authorise the operator to auto-patch
	// spec.extraEnvFrom when a Neo4jShardedDatabase or Neo4jDatabase needs
	// cloud-credentials access for seedURI / seedBackupRef. Without the
	// annotation, the operator emits an actionable error instead.
	//
	// Set to the string "true". The annotation is checked on the cluster CR
	// referenced by the seed-consuming CR, NOT on the seed-consuming CR
	// itself — clusters are owned by infrastructure operators, and they're
	// the right party to authorise the rolling restart this triggers.
	AutoInheritSeedCredsAnnotation = "neo4j.com/auto-inherit-seed-creds"

	// AutoInheritedFromAnnotation is set by the operator on the cluster CR
	// when it has auto-patched spec.extraEnvFrom from a seed source. Value
	// is the Secret name that was projected. Audit trail for users to
	// understand where the entry came from.
	AutoInheritedFromAnnotation = "neo4j.com/seed-creds-auto-inherited-from"
)

// EnsureClusterHasSeedCreds validates that a cluster's spec.extraEnvFrom
// includes the named Secret so the Neo4j JVM (running on cluster server
// pods) can authenticate via the AWS / GCP / Azure SDK default credential
// chain when fetching a seedURI during `CREATE DATABASE … OPTIONS { seedURI }`.
//
// Returns:
//   - (autoInherited=false, nil)  when the cluster already has the Secret
//     projected (no action taken) OR when credsSecretName is empty (no
//     Secret to validate — the user is presumably relying on IRSA / GKE
//     Workload Identity / Azure Workload Identity instead).
//   - (autoInherited=true, nil)  after the operator has appended the
//     missing entry to spec.extraEnvFrom under the auto-inherit
//     annotation. The caller should treat this as a transient state:
//     the cluster controller now needs to roll out the StatefulSet, and
//     a subsequent reconcile will find the entry already present.
//   - (autoInherited=false, actionableErr)  when the Secret is absent
//     and the cluster lacks the auto-inherit annotation. The error
//     message is a copy-pasteable snippet directing the user to add the
//     entry to their cluster CR.
//
// The caller is expected to:
//  1. Set the sharded/standard DB's status.phase to a transient value
//     (Pending / Waiting) when autoInherited=true and requeue, because
//     the cluster pods need time to restart with the new envFrom.
//  2. Set the DB's status.phase to Failed when an actionable error is
//     returned. The user must update their cluster CR before the seed
//     can be reattempted.
func EnsureClusterHasSeedCreds(
	ctx context.Context,
	c client.Client,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	credsSecretName string,
) (autoInherited bool, err error) {
	if credsSecretName == "" {
		// No Secret to validate. The user is relying on an external
		// credentials path (IAM role on the node, IRSA via SA annotations,
		// GKE Workload Identity, etc.). The operator can't verify these
		// from here, so we trust the user and let Neo4j fail later if the
		// creds aren't actually available.
		return false, nil
	}

	// Already projected? Walk extraEnvFrom looking for a SecretRef with the
	// matching Name. The user can have multiple Secrets projected; we just
	// need one to match.
	for _, ef := range cluster.Spec.ExtraEnvFrom {
		if ef.SecretRef != nil && ef.SecretRef.Name == credsSecretName {
			return false, nil
		}
	}

	// Not projected. Auto-inherit only if the cluster owner has opted in
	// via annotation — the patch triggers a rolling restart of cluster
	// pods, which a sharded-DB controller shouldn't be allowed to do
	// unsolicited.
	if cluster.GetAnnotations()[AutoInheritSeedCredsAnnotation] != "true" {
		return false, fmt.Errorf(
			"cluster %q is not configured to access seed credentials Secret %q.\n"+
				"Either:\n"+
				"  1. Add this entry to the cluster CR (no rolling restart needed if cluster is recreated):\n"+
				"     spec:\n"+
				"       extraEnvFrom:\n"+
				"       - secretRef:\n"+
				"           name: %s\n"+
				"  2. Or set annotation `%s: \"true\"` on the cluster CR to let the operator add it automatically (triggers a rolling restart of cluster pods).",
			cluster.Name, credsSecretName, credsSecretName, AutoInheritSeedCredsAnnotation,
		)
	}

	// Opt-in confirmed. Append the entry and Update. The cluster controller
	// will pick up the spec change on its next reconcile and roll out the
	// StatefulSet.
	cluster.Spec.ExtraEnvFrom = append(cluster.Spec.ExtraEnvFrom, corev1.EnvFromSource{
		SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: credsSecretName},
		},
	})
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations[AutoInheritedFromAnnotation] = credsSecretName
	if err := c.Update(ctx, cluster); err != nil {
		return false, fmt.Errorf("auto-inherit seed credentials Secret %q onto cluster %q: %w", credsSecretName, cluster.Name, err)
	}
	return true, nil
}
