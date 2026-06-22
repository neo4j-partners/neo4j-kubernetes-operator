/*
Copyright 2025 Priyo Lahiri.

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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// ErrBackupNotReady is the sentinel error returned by ResolveBackupRef when
// the referenced Neo4jBackup exists but has no Succeeded run in
// status.history yet. Transient by definition — the backup may complete on a
// future reconcile. Callers should detect with errors.Is and route to a
// Pending phase + requeue (NOT terminal Failed).
//
// Exported so any controller that resolves a backupRef can use the same
// sentinel for consistent Pending-vs-Failed routing. Pinned by CLAUDE.md
// rule 43.
var ErrBackupNotReady = fmt.Errorf("backup has no Succeeded run yet")

// ResolveBackupRef dereferences a Neo4jBackup CR name into a concrete
// StorageLocation (with cloud creds folded in) and the per-run subfolder
// (BackupsPath) of its most-recent Succeeded run.
//
// Free-function form so both the Neo4jRestore controller (which historically
// owned this logic) and the Neo4jShardedDatabase controller (Phase 2
// seedBackupRef → seedURI resolution) can call it without inheriting any
// reconciler-specific state.
//
// Returns:
//   - ("", "", err)  if backupRef is empty or the Neo4jBackup is missing
//     (permanent error — caller should route to Failed).
//   - ("", "", wrapped ErrBackupNotReady)  if the backup exists but has no
//     Succeeded run yet (transient — caller should route to Pending +
//     requeue via errors.Is).
//   - (StorageLocation, backupPath, nil)  on success. The StorageLocation
//     is materialised with backup-level cloud creds folded into Storage.Cloud
//     so downstream consumers see the canonical shape.
func ResolveBackupRef(ctx context.Context, c client.Reader, backupRef, namespace string) (storage neo4jv1beta1.StorageLocation, backupPath string, err error) {
	if backupRef == "" {
		return neo4jv1beta1.StorageLocation{}, "", fmt.Errorf("backupRef is required")
	}

	backup := &neo4jv1beta1.Neo4jBackup{}
	if err := c.Get(ctx, types.NamespacedName{Name: backupRef, Namespace: namespace}, backup); err != nil {
		return neo4jv1beta1.StorageLocation{}, "", fmt.Errorf("failed to get Neo4jBackup %q: %w", backupRef, err)
	}

	// status.history is sorted newest-first by sortBackupRunsNewestFirst (see
	// neo4jbackup_controller.go). Walk forward until we find the first
	// Succeeded run — "the most recent successful backup" is the only
	// reasonable default for a backupRef.
	var succeeded *neo4jv1beta1.BackupRun
	for i := range backup.Status.History {
		if backup.Status.History[i].Status == "Succeeded" {
			succeeded = &backup.Status.History[i]
			break
		}
	}
	if succeeded == nil {
		return neo4jv1beta1.StorageLocation{}, "", fmt.Errorf(
			"Neo4jBackup %q: %w (status.history has no Succeeded run yet)",
			backupRef, ErrBackupNotReady,
		)
	}

	// Materialize a StorageLocation copy with the backup's cloud creds folded
	// in. Neo4jBackup historically allowed spec.cloud as an alternative to
	// spec.storage.cloud; cloudBlockForBackup (defined in
	// neo4jbackup_controller.go) picks whichever is populated, and we project
	// it onto the synthesized Storage so downstream consumers find it via the
	// canonical path.
	resolved := backup.Spec.Storage
	if resolved.Cloud == nil {
		if cb := cloudBlockForBackup(backup); cb != nil {
			resolved.Cloud = cb
		}
	}
	// Mirror the WRITE side's path defaulting (buildToPath): an empty
	// storage.path means the backup Job wrote to
	// "<scheme>://<bucket>/backups/<chain>/", so the resolved location must
	// carry "backups" too — otherwise every consumer (restore --from-path,
	// cluster seedURI, sharded seed) reads "<scheme>://<bucket>/<chain>/"
	// and the restore fails file-not-found (#218). PVC consumers ignore
	// Path, so unconditional defaulting is safe.
	if resolved.Path == "" {
		resolved.Path = defaultBackupStoragePath
	}
	return resolved, succeeded.BackupsPath, nil
}
