/*
Copyright 2025.

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
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

// storageExpansionResult describes the outcome of a storage expansion check.
type storageExpansionResult struct {
	// needed is true when PVC sizes differ from the desired spec.
	needed bool
	// dataExpansion is true when data volume expansion is required.
	dataExpansion bool
	// backupExpansion is true when backup volume expansion is required.
	backupExpansion bool
	// shrinkDetected is true when the desired size is smaller than existing PVCs.
	shrinkDetected bool
	// shrinkMessage describes the shrink rejection.
	shrinkMessage string
}

// reconcileStorageExpansion checks whether PVC expansion is needed and performs it.
// It returns (requeue, error). If requeue is true the caller should requeue immediately.
// This method is non-disruptive: pods keep running throughout the expansion.
func (r *Neo4jEnterpriseClusterReconciler) reconcileStorageExpansion(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (bool, error) {
	logger := log.FromContext(ctx)

	result, err := r.checkStorageExpansionNeeded(ctx, cluster)
	if err != nil {
		return false, err
	}

	// Reject storage shrink — PVCs cannot be reduced in size
	if result.shrinkDetected {
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonStorageExpansionFailed,
			"Storage shrink rejected: %s", result.shrinkMessage)
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Storage shrink not supported: %s", result.shrinkMessage))
		return false, fmt.Errorf("storage shrink not supported: %s", result.shrinkMessage)
	}

	if !result.needed {
		return false, nil
	}

	logger.Info("Storage expansion required",
		"dataExpansion", result.dataExpansion,
		"backupExpansion", result.backupExpansion)

	// Phase: Expanding
	_ = r.updateClusterStatus(ctx, cluster, "Expanding", "Expanding storage volumes")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonStorageExpansionStarted,
		"Storage expansion started")

	// Expand data volumes
	if result.dataExpansion {
		desiredSize := resource.MustParse(cluster.Spec.Storage.Size)
		stsName := fmt.Sprintf("%s-server", cluster.Name)

		if err := r.expandVolumes(ctx, cluster, stsName, "data", desiredSize); err != nil {
			r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonStorageExpansionFailed,
				"Failed to expand data storage: %v", err)
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Storage expansion failed: %v", err))
			return false, err
		}
	}

	// Expand backup volumes
	if result.backupExpansion {
		backupSize := cluster.Spec.Storage.Size
		if cluster.Spec.Storage.BackupStorage != nil && cluster.Spec.Storage.BackupStorage.Size != "" {
			backupSize = cluster.Spec.Storage.BackupStorage.Size
		}
		desiredSize := resource.MustParse(backupSize)
		stsName := fmt.Sprintf("%s-backup", cluster.Name)

		if err := r.expandVolumes(ctx, cluster, stsName, "backup-storage", desiredSize); err != nil {
			r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonStorageExpansionFailed,
				"Failed to expand backup storage: %v", err)
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Backup storage expansion failed: %v", err))
			return false, err
		}
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonStorageExpansionCompleted,
		"Storage expansion completed successfully")

	// Requeue so the next reconcile recreates the StatefulSet(s) with updated VolumeClaimTemplates
	return true, nil
}

// checkStorageExpansionNeeded compares desired storage sizes against existing PVCs.
// It also detects shrink attempts (desired < current) and sets shrinkDetected.
func (r *Neo4jEnterpriseClusterReconciler) checkStorageExpansionNeeded(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (storageExpansionResult, error) {
	result := storageExpansionResult{}

	// Check data volumes
	desiredDataSize := resource.MustParse(cluster.Spec.Storage.Size)
	stsName := fmt.Sprintf("%s-server", cluster.Name)
	state, err := r.comparePVCSizes(ctx, cluster.Namespace, stsName, "data", desiredDataSize)
	if err != nil {
		return result, err
	}
	if state == pvcSizeShrink {
		result.shrinkDetected = true
		result.shrinkMessage = fmt.Sprintf("spec.storage.size (%s) is smaller than existing data PVCs; PVC shrink is not supported by Kubernetes", cluster.Spec.Storage.Size)
		return result, nil
	}
	result.dataExpansion = (state == pvcSizeExpand)

	// Check backup volumes
	if cluster.Spec.Backups != nil {
		backupSize := cluster.Spec.Storage.Size
		if cluster.Spec.Storage.BackupStorage != nil && cluster.Spec.Storage.BackupStorage.Size != "" {
			backupSize = cluster.Spec.Storage.BackupStorage.Size
		}
		desiredBackupSize := resource.MustParse(backupSize)
		backupStsName := fmt.Sprintf("%s-backup", cluster.Name)
		state, err := r.comparePVCSizes(ctx, cluster.Namespace, backupStsName, "backup-storage", desiredBackupSize)
		if err != nil {
			return result, err
		}
		if state == pvcSizeShrink {
			result.shrinkDetected = true
			result.shrinkMessage = fmt.Sprintf("backup storage size (%s) is smaller than existing backup PVCs; PVC shrink is not supported by Kubernetes", backupSize)
			return result, nil
		}
		result.backupExpansion = (state == pvcSizeExpand)
	}

	result.needed = result.dataExpansion || result.backupExpansion
	return result, nil
}

// pvcSizeState represents the relationship between desired and actual PVC sizes.
type pvcSizeState int

const (
	pvcSizeMatch  pvcSizeState = iota // All PVCs match or no PVCs exist
	pvcSizeExpand                     // At least one PVC is smaller than desired
	pvcSizeShrink                     // Desired is smaller than at least one PVC
)

// comparePVCSizes checks whether PVCs need expansion, are at the right size, or would require a shrink.
func (r *Neo4jEnterpriseClusterReconciler) comparePVCSizes(ctx context.Context, namespace, stsName, volumeName string, desiredSize resource.Quantity) (pvcSizeState, error) {
	pvcs, err := r.findPVCsForStatefulSet(ctx, namespace, stsName, volumeName)
	if err != nil {
		return pvcSizeMatch, err
	}

	for _, pvc := range pvcs {
		currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		cmp := desiredSize.Cmp(currentSize)
		if cmp < 0 {
			return pvcSizeShrink, nil
		}
		if cmp > 0 {
			return pvcSizeExpand, nil
		}
	}
	return pvcSizeMatch, nil
}

// expandVolumes performs PVC expansion and orphan-deletes the StatefulSet.
func (r *Neo4jEnterpriseClusterReconciler) expandVolumes(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, stsName, volumeName string, desiredSize resource.Quantity) error {
	logger := log.FromContext(ctx)

	// 1. Validate the StorageClass supports volume expansion
	pvcs, err := r.findPVCsForStatefulSet(ctx, cluster.Namespace, stsName, volumeName)
	if err != nil {
		return fmt.Errorf("failed to find PVCs for %s/%s: %w", stsName, volumeName, err)
	}
	if len(pvcs) == 0 {
		logger.Info("No PVCs found for expansion", "statefulSet", stsName, "volume", volumeName)
		return nil
	}

	// Check StorageClass expandability using the first PVC's StorageClass
	if err := r.validateStorageClassExpandable(ctx, pvcs[0]); err != nil {
		return err
	}

	// 2. Patch each PVC with the new size
	for i := range pvcs {
		pvc := &pvcs[i]
		currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if currentSize.Cmp(desiredSize) >= 0 {
			continue // Already at or above desired size
		}

		logger.Info("Expanding PVC",
			"pvc", pvc.Name,
			"currentSize", currentSize.String(),
			"desiredSize", desiredSize.String())

		if err := r.patchPVCSize(ctx, pvc, desiredSize); err != nil {
			return fmt.Errorf("failed to patch PVC %s: %w", pvc.Name, err)
		}
	}

	// 3. Orphan-delete the StatefulSet so pods keep running
	if err := r.orphanDeleteStatefulSet(ctx, cluster.Namespace, stsName); err != nil {
		return fmt.Errorf("failed to orphan-delete StatefulSet %s: %w", stsName, err)
	}

	return nil
}

// findPVCsForStatefulSet discovers PVCs belonging to a StatefulSet.
// Uses two-tier strategy: label selector first, name-prefix + ordinal fallback for legacy clusters.
func (r *Neo4jEnterpriseClusterReconciler) findPVCsForStatefulSet(ctx context.Context, namespace, stsName, volumeName string) ([]corev1.PersistentVolumeClaim, error) {
	logger := log.FromContext(ctx)

	// Tier 1: Label-based discovery
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := client.MatchingLabels{
		"app.kubernetes.io/name":     "neo4j",
		"app.kubernetes.io/instance": strings.TrimSuffix(stsName, "-server"),
	}
	if err := r.List(ctx, pvcList, client.InNamespace(namespace), labelSelector); err != nil {
		return nil, fmt.Errorf("failed to list PVCs by label: %w", err)
	}

	// Filter by volume name prefix (PVC name format: {volumeName}-{stsName}-{ordinal})
	var matched []corev1.PersistentVolumeClaim
	prefix := fmt.Sprintf("%s-%s-", volumeName, stsName)
	for _, pvc := range pvcList.Items {
		if strings.HasPrefix(pvc.Name, prefix) {
			suffix := strings.TrimPrefix(pvc.Name, prefix)
			if _, err := strconv.Atoi(suffix); err == nil {
				matched = append(matched, pvc)
			}
		}
	}

	if len(matched) > 0 {
		return matched, nil
	}

	// Tier 2: Name-prefix fallback for legacy clusters without labels
	logger.V(1).Info("No PVCs found by label, trying name-prefix fallback",
		"statefulSet", stsName, "volume", volumeName)

	allPVCs := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, allPVCs, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list all PVCs in namespace: %w", err)
	}

	for _, pvc := range allPVCs.Items {
		if strings.HasPrefix(pvc.Name, prefix) {
			suffix := strings.TrimPrefix(pvc.Name, prefix)
			// Validate ordinal to prevent prefix collision (e.g., "my-cluster" vs "my-cluster-extended")
			if _, err := strconv.Atoi(suffix); err == nil {
				matched = append(matched, pvc)
			}
		}
	}

	return matched, nil
}

// validateStorageClassExpandable checks that the StorageClass allows volume expansion.
func (r *Neo4jEnterpriseClusterReconciler) validateStorageClassExpandable(ctx context.Context, pvc corev1.PersistentVolumeClaim) error {
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName == "" {
		return fmt.Errorf("PVC %s has no StorageClass set; cannot determine if expansion is supported", pvc.Name)
	}

	sc := &storagev1.StorageClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: *pvc.Spec.StorageClassName}, sc); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("StorageClass %q not found; cannot validate volume expansion support", *pvc.Spec.StorageClassName)
		}
		return fmt.Errorf("failed to get StorageClass %q: %w", *pvc.Spec.StorageClassName, err)
	}

	if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
		return fmt.Errorf("StorageClass %q does not allow volume expansion (allowVolumeExpansion is not true); update the StorageClass or use a different one", sc.Name)
	}

	return nil
}

// patchPVCSize patches a PVC's storage request with retry on conflict.
func (r *Neo4jEnterpriseClusterReconciler) patchPVCSize(ctx context.Context, pvc *corev1.PersistentVolumeClaim, desiredSize resource.Quantity) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch to get latest resourceVersion
		latest := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, latest); err != nil {
			return err
		}

		latest.Spec.Resources.Requests[corev1.ResourceStorage] = desiredSize
		return r.Update(ctx, latest)
	})
}

// orphanDeleteStatefulSet deletes a StatefulSet with the Orphan propagation policy,
// leaving pods running. The StatefulSet will be recreated on the next reconcile
// with updated VolumeClaimTemplates.
func (r *Neo4jEnterpriseClusterReconciler) orphanDeleteStatefulSet(ctx context.Context, namespace, name string) error {
	logger := log.FromContext(ctx)

	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, sts); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("StatefulSet already deleted, skipping orphan-delete", "statefulSet", name)
			return nil
		}
		return err
	}

	logger.Info("Orphan-deleting StatefulSet for storage expansion",
		"statefulSet", name, "namespace", namespace)

	propagation := metav1.DeletePropagationOrphan
	return r.Delete(ctx, sts, &client.DeleteOptions{
		PropagationPolicy: &propagation,
	})
}
