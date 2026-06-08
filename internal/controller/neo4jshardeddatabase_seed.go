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

	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// ResolvedShardedSeed captures the outcome of resolving
// `spec.seedBackupRef` on a Neo4jShardedDatabase. Either URI (cloud) or
// PerShardURIs (PVC) is populated, never both.
type ResolvedShardedSeed struct {
	// URI is a directory seedURI (cloud-backed backups) consumed by
	// `OPTIONS { seedURI: '<URI>' }` in the CREATE DATABASE Cypher.
	URI string
	// PerShardURIs maps shard name (e.g. "products-g000") to an HTTP URL
	// served by the operator-managed PVC seed proxy. Populated when the
	// backup is PVC-storage; consumed by `OPTIONS { seedURIs: { … } }`.
	PerShardURIs map[string]string
	// CredsSecretName is the cloud creds Secret to project onto cluster
	// pods for cloud seedURIs. Empty for PVC (the proxy is in-cluster
	// HTTP, no creds needed) and for backups that use workload identity.
	CredsSecretName string
	// ProxyAvailable is meaningful only for PVC-backed restores: true
	// when the proxy Deployment reports ≥1 Ready replica. False means
	// the caller should route to Pending and requeue.
	ProxyAvailable bool
}

// resolveShardedSeed resolves spec.seedBackupRef on a Neo4jShardedDatabase
// into either a cloud directory URI or a per-shard HTTP-proxy URL map,
// depending on the backup's storage type.
//
// Returns:
//   - (nil, nil)  when SeedBackupRef is empty — the caller falls through
//     to the existing manual spec.SeedURI / spec.SeedURIs paths.
//   - (nil, wrapped ErrBackupNotReady)  when the backup CR exists but
//     has no Succeeded run yet. Caller should route to Pending + requeue.
//   - (nil, err)  for permanent failures (missing backup CR, missing
//     shard artifact metadata for PVC, unsupported storage type).
//   - (resolved, nil)  on success. Inspect resolved.URI vs
//     resolved.PerShardURIs to pick the right OPTIONS clause.
//
// For PVC-backed backups: the operator creates a per-shardedDB
// HTTP-proxy Deployment + Service mounting the backup PVC RO and serving
// shard `.backup` files over HTTP. Cluster pods use Neo4j's
// URLConnectionSeedProvider via per-shard URLs.
func (r *Neo4jShardedDatabaseReconciler) resolveShardedSeed(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) (*ResolvedShardedSeed, error) {
	if shardedDB.Spec.SeedBackupRef == "" {
		return nil, nil
	}

	storage, backupPath, err := ResolveBackupRef(ctx, r.Client, shardedDB.Spec.SeedBackupRef, shardedDB.Namespace)
	if err != nil {
		return nil, err
	}

	// Cloud path: existing CloudSeedProvider directory URI.
	if storage.Type == "s3" || storage.Type == "gcs" || storage.Type == "azure" {
		uri, err := buildSeedURIFromBackupStorage(storage, backupPath)
		if err != nil {
			return nil, err
		}
		credsSecretName := ""
		if storage.Cloud != nil {
			credsSecretName = storage.Cloud.CredentialsSecretRef
		}
		return &ResolvedShardedSeed{URI: uri, CredsSecretName: credsSecretName}, nil
	}

	// PVC path: build per-shard URLs against the operator-managed proxy.
	if storage.Type == "pvc" {
		return r.resolvePVCShardedSeed(ctx, shardedDB, storage, backupPath)
	}

	return nil, fmt.Errorf("seedBackupRef does not support storage type %q", storage.Type)
}

// resolvePVCShardedSeed handles the PVC-backed branch of resolveShardedSeed.
// Looks up the backup CR's recorded ShardArtifacts (populated by F3 when
// the backup ran) to construct per-shard URLs against the seed proxy.
//
// Returns an error if the backup didn't record per-shard filenames —
// the F3 Pod-log parsing must have captured them. If not, the user
// must re-run the backup with the F3-capable operator before they can
// restore from PVC.
func (r *Neo4jShardedDatabaseReconciler) resolvePVCShardedSeed(
	ctx context.Context,
	shardedDB *neo4jv1beta1.Neo4jShardedDatabase,
	storage neo4jv1beta1.StorageLocation,
	backupPath string,
) (*ResolvedShardedSeed, error) {
	if storage.PVC == nil || storage.PVC.Name == "" {
		return nil, fmt.Errorf("PVC-backed seedBackupRef requires the backup's storage.pvc.name to be set")
	}

	// Fetch the backup CR again to read its ShardArtifacts (the shared
	// ResolveBackupRef helper doesn't expose them — it returns only
	// StorageLocation + BackupsPath, which the restore controller and
	// the cloud sharded-seed path don't need).
	backup := &neo4jv1beta1.Neo4jBackup{}
	if err := r.Get(ctx, types.NamespacedName{Name: shardedDB.Spec.SeedBackupRef, Namespace: shardedDB.Namespace}, backup); err != nil {
		return nil, fmt.Errorf("PVC seed: re-fetch backup %q: %w", shardedDB.Spec.SeedBackupRef, err)
	}

	// Find the BackupRun that matches the BackupsPath we just resolved —
	// that's the Succeeded run whose artifacts we want to project.
	var run *neo4jv1beta1.BackupRun
	for i := range backup.Status.History {
		if backup.Status.History[i].BackupsPath == backupPath && backup.Status.History[i].Status == "Succeeded" {
			run = &backup.Status.History[i]
			break
		}
	}
	if run == nil {
		return nil, fmt.Errorf("PVC seed: no Succeeded run with BackupsPath %q on Neo4jBackup %q (internal: ResolveBackupRef returned a path that's now missing)", backupPath, shardedDB.Spec.SeedBackupRef)
	}
	if len(run.ShardArtifacts) == 0 {
		return nil, fmt.Errorf("PVC seed: Neo4jBackup %q run %q has no shardArtifacts metadata recorded — re-run the backup with a recent operator version (F3 wiring required to capture per-shard filenames)", backup.Name, run.RunID)
	}

	// Validate that filenames are captured (ShardName-only entries from
	// the F3-deferred era won't have per-shard filenames the proxy can
	// serve).
	missingFilenames := []string{}
	for _, a := range run.ShardArtifacts {
		if a.Filename == "" {
			missingFilenames = append(missingFilenames, a.ShardName)
		}
	}
	if len(missingFilenames) > 0 {
		return nil, fmt.Errorf("PVC seed: shards %v have empty Filename in backup status.history — F3 Pod-log parsing didn't capture them. Re-run the backup, or use a cloud-backed seedBackupRef instead", missingFilenames)
	}

	// Ensure the proxy exists + is Ready. If not Ready yet, route to
	// Pending so the next reconcile picks it up.
	available, err := r.ensurePVCSeedProxy(ctx, shardedDB, storage.PVC.Name)
	if err != nil {
		return nil, err
	}

	// Build per-shard URL map even if proxy isn't Ready yet — caller can
	// inspect ProxyAvailable to decide whether to proceed or wait.
	perShard := make(map[string]string, len(run.ShardArtifacts))
	for _, a := range run.ShardArtifacts {
		perShard[a.ShardName] = pvcSeedProxyURLForShard(shardedDB.Name, shardedDB.Namespace, backupPath, a.Filename)
	}

	return &ResolvedShardedSeed{
		PerShardURIs:   perShard,
		ProxyAvailable: available,
	}, nil
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
