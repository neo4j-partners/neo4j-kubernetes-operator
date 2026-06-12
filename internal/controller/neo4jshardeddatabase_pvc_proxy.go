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

	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// pvcSeedProxyURLForShard is the sharded-DB convenience wrapper around
// pvcSeedProxyURL. The proxy owner is the sharded DB CR, so its name is
// the URL suffix.
func pvcSeedProxyURLForShard(shardedDBName, namespace, backupsPath, filename string) string {
	return pvcSeedProxyURL(shardedDBName, namespace, backupsPath, filename)
}

// ensurePVCSeedProxy is the sharded-DB convenience wrapper around
// ensurePVCSeedProxyResources. Delegates to the generic helper with the
// sharded DB CR as the owner.
func (r *Neo4jShardedDatabaseReconciler) ensurePVCSeedProxy(
	ctx context.Context,
	shardedDB *neo4jv1beta1.Neo4jShardedDatabase,
	backupPVCName string,
) (proxyAvailable bool, err error) {
	available, err := ensurePVCSeedProxyResources(ctx, r.Client, r.Scheme, shardedDB, shardedDB.Name, backupPVCName)
	if err == nil {
		// Restrict the proxy to the target cluster's server pods (#219).
		if npErr := ensurePVCSeedProxyNetworkPolicy(ctx, r.Client, r.Scheme, shardedDB, shardedDB.Name, shardedDB.Spec.ClusterRef); npErr != nil {
			log.FromContext(ctx).Error(npErr, "Failed to ensure seed-proxy NetworkPolicy (non-fatal)")
		}
	}
	return available, err
}

// teardownPVCSeedProxy removes the proxy stack once the sharded database has
// finished seeding (#219) — the proxy otherwise serves the whole backup PVC
// for the lifetime of the (long-lived) sharded DB CR.
func (r *Neo4jShardedDatabaseReconciler) teardownPVCSeedProxy(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) error {
	return teardownPVCSeedProxyResources(ctx, r.Client, shardedDB.Namespace, shardedDB.Name)
}
