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

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// resolveShardedSeed resolves spec.seedBackupRef on a Neo4jShardedDatabase
// into a concrete cloud seedURI by dereferencing the referenced Neo4jBackup's
// most-recent Succeeded run.
//
// Returns:
//   - ("", "", nil)  when SeedBackupRef is empty — the caller falls through to
//     the existing manual spec.SeedURI / spec.SeedURIs paths.
//   - ("", "", wrapped ErrBackupNotReady)  when the backup CR exists but has no
//     Succeeded run yet. Caller should route to Pending + requeue.
//   - ("", "", err)  for permanent failures (missing backup CR, PVC storage,
//     unsupported storage type). Caller routes to Failed.
//   - (uri, credsSecretName, nil)  on success. The URI is a directory
//     (trailing slash) so neo4j-admin's CloudSeedProvider auto-discovers
//     per-shard files inside. credsSecretName is the Secret name from the
//     backup's resolved cloud block — empty when the backup uses workload
//     identity instead of an explicit Secret.
//
// PVC-storage backups are rejected here: the backup PVC is only mounted on
// the backup Job pod, not on the Neo4j cluster server pods that execute the
// CREATE DATABASE Cypher. Supporting PVC seeding would require mounting the
// backup PVC RO on every cluster pod, which is a server-statefulset-spec
// mutation outside the scope of the seedBackupRef field. Operators wanting
// cloudless restore should copy artifacts from the backup PVC to a cloud
// target first, or set spec.seedURI manually.
func (r *Neo4jShardedDatabaseReconciler) resolveShardedSeed(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) (string, string, error) {
	if shardedDB.Spec.SeedBackupRef == "" {
		return "", "", nil
	}

	storage, backupPath, err := ResolveBackupRef(ctx, r.Client, shardedDB.Spec.SeedBackupRef, shardedDB.Namespace)
	if err != nil {
		// ErrBackupNotReady is wrapped through here unchanged so callers can
		// use errors.Is to distinguish transient (Pending) from permanent
		// (Failed) failures.
		return "", "", err
	}

	uri, err := buildSeedURIFromBackupStorage(storage, backupPath)
	if err != nil {
		return "", "", err
	}
	credsSecretName := ""
	if storage.Cloud != nil {
		credsSecretName = storage.Cloud.CredentialsSecretRef
	}
	return uri, credsSecretName, nil
}

// buildSeedURIFromBackupStorage converts a backup's resolved StorageLocation
// + per-run backupPath into the directory URI that the Neo4j sharded
// CloudSeedProvider expects. The trailing slash is critical: without it,
// Neo4j's seed code treats the value as a single artifact path rather than
// a directory to scan for per-shard files.
//
// Currently supports s3:// (S3 / MinIO / R2 / etc.), gs:// (GCS), and azb://
// (Azure Blob). PVC and other storage types return an explanatory error.
func buildSeedURIFromBackupStorage(storage neo4jv1beta1.StorageLocation, backupPath string) (string, error) {
	basePath := storage.Path
	var fullPath string
	switch {
	case basePath != "" && backupPath != "":
		fullPath = fmt.Sprintf("%s/%s", strings.TrimRight(basePath, "/"), backupPath)
	case basePath != "":
		fullPath = basePath
	case backupPath != "":
		fullPath = backupPath
	}
	// Trailing slash → directory semantics for the CloudSeedProvider.
	if !strings.HasSuffix(fullPath, "/") {
		fullPath += "/"
	}

	switch storage.Type {
	case "s3":
		return fmt.Sprintf("s3://%s/%s", storage.Bucket, strings.TrimLeft(fullPath, "/")), nil
	case "gcs":
		return fmt.Sprintf("gs://%s/%s", storage.Bucket, strings.TrimLeft(fullPath, "/")), nil
	case "azure":
		return fmt.Sprintf("azb://%s/%s", storage.Bucket, strings.TrimLeft(fullPath, "/")), nil
	case "pvc", "":
		return "", fmt.Errorf("seedBackupRef requires cloud-backed backup storage (s3, gcs, azure); got %q — copy backup artifacts to a cloud bucket or set spec.seedURI manually", storage.Type)
	default:
		return "", fmt.Errorf("seedBackupRef does not support storage type %q", storage.Type)
	}
}
