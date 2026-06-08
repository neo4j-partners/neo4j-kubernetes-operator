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
	return ensurePVCSeedProxyResources(ctx, r.Client, r.Scheme, shardedDB, shardedDB.Name, backupPVCName)
}
