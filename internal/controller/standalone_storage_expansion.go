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

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

// reconcileStandaloneStorageExpansion checks whether PVC expansion is needed for a standalone
// deployment and performs it. Returns (requeue, error).
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStandaloneStorageExpansion(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) (bool, error) {
	logger := log.FromContext(ctx)

	desiredSize := resource.MustParse(standalone.Spec.Storage.Size)
	stsName := standalone.Name

	state, err := r.compareStandalonePVCSizes(ctx, standalone.Namespace, stsName, "neo4j-data", desiredSize)
	if err != nil {
		return false, err
	}

	switch state {
	case pvcSizeMatch:
		return false, nil

	case pvcSizeShrink:
		msg := fmt.Sprintf("spec.storage.size (%s) is smaller than existing PVC; PVC shrink is not supported by Kubernetes", standalone.Spec.Storage.Size)
		if r.Recorder != nil {
			r.Recorder.Eventf(standalone, corev1.EventTypeWarning, EventReasonStorageExpansionFailed,
				"Storage shrink rejected: %s", msg)
		}
		return false, fmt.Errorf("storage shrink not supported: %s", msg)

	case pvcSizeExpand:
		logger.Info("Storage expansion required for standalone", "desiredSize", desiredSize.String())

		if r.Recorder != nil {
			r.Recorder.Event(standalone, corev1.EventTypeNormal, EventReasonStorageExpansionStarted,
				"Storage expansion started")
		}

		if err := r.expandStandaloneVolumes(ctx, standalone, stsName, "neo4j-data", desiredSize); err != nil {
			if r.Recorder != nil {
				r.Recorder.Eventf(standalone, corev1.EventTypeWarning, EventReasonStorageExpansionFailed,
					"Failed to expand storage: %v", err)
			}
			return false, err
		}

		if r.Recorder != nil {
			r.Recorder.Event(standalone, corev1.EventTypeNormal, EventReasonStorageExpansionCompleted,
				"Storage expansion completed successfully")
		}

		return true, nil
	}

	return false, nil
}

// compareStandalonePVCSizes checks PVC sizes against the desired size for a standalone deployment.
func (r *Neo4jEnterpriseStandaloneReconciler) compareStandalonePVCSizes(ctx context.Context, namespace, stsName, volumeName string, desiredSize resource.Quantity) (pvcSizeState, error) {
	pvcs, err := r.findStandalonePVCs(ctx, namespace, stsName, volumeName)
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

// findStandalonePVCs discovers PVCs belonging to a standalone StatefulSet.
// Uses two-tier strategy: label selector first, name-prefix fallback for legacy deployments.
func (r *Neo4jEnterpriseStandaloneReconciler) findStandalonePVCs(ctx context.Context, namespace, stsName, volumeName string) ([]corev1.PersistentVolumeClaim, error) {
	logger := log.FromContext(ctx)

	// Tier 1: Label-based discovery using stable PVC labels
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcList, client.InNamespace(namespace), client.MatchingLabels{
		"neo4j.com/cluster": stsName,
		"neo4j.com/role":    "data",
	}); err != nil {
		return nil, fmt.Errorf("failed to list PVCs by label: %w", err)
	}

	// Filter by volume name prefix: {volumeName}-{stsName}-{ordinal}
	prefix := fmt.Sprintf("%s-%s-", volumeName, stsName)
	var matched []corev1.PersistentVolumeClaim
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

	// Tier 2: Name-prefix fallback for legacy deployments without labels
	logger.V(1).Info("No PVCs found by label, trying name-prefix fallback",
		"standalone", stsName, "volume", volumeName)

	allPVCs := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, allPVCs, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list all PVCs in namespace: %w", err)
	}

	for _, pvc := range allPVCs.Items {
		if strings.HasPrefix(pvc.Name, prefix) {
			suffix := strings.TrimPrefix(pvc.Name, prefix)
			if _, err := strconv.Atoi(suffix); err == nil {
				matched = append(matched, pvc)
			}
		}
	}

	return matched, nil
}

// expandStandaloneVolumes patches PVCs and orphan-deletes the StatefulSet for a standalone deployment.
func (r *Neo4jEnterpriseStandaloneReconciler) expandStandaloneVolumes(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone, stsName, volumeName string, desiredSize resource.Quantity) error {
	logger := log.FromContext(ctx)

	pvcs, err := r.findStandalonePVCs(ctx, standalone.Namespace, stsName, volumeName)
	if err != nil {
		return fmt.Errorf("failed to find PVCs: %w", err)
	}
	if len(pvcs) == 0 {
		logger.Info("No PVCs found for expansion", "standalone", stsName)
		return nil
	}

	// Validate StorageClass supports expansion
	if err := r.validateStandaloneStorageClassExpandable(ctx, pvcs[0]); err != nil {
		return err
	}

	// Patch each PVC
	for i := range pvcs {
		pvc := &pvcs[i]
		currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if currentSize.Cmp(desiredSize) >= 0 {
			continue
		}

		logger.Info("Expanding PVC",
			"pvc", pvc.Name,
			"currentSize", currentSize.String(),
			"desiredSize", desiredSize.String())

		if err := r.patchStandalonePVCSize(ctx, pvc, desiredSize); err != nil {
			return fmt.Errorf("failed to patch PVC %s: %w", pvc.Name, err)
		}
	}

	// Orphan-delete the StatefulSet
	if err := r.orphanDeleteStandaloneStatefulSet(ctx, standalone.Namespace, stsName); err != nil {
		return fmt.Errorf("failed to orphan-delete StatefulSet %s: %w", stsName, err)
	}

	return nil
}

// validateStandaloneStorageClassExpandable checks that the StorageClass allows volume expansion.
func (r *Neo4jEnterpriseStandaloneReconciler) validateStandaloneStorageClassExpandable(ctx context.Context, pvc corev1.PersistentVolumeClaim) error {
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

// patchStandalonePVCSize patches a PVC's storage request with retry on conflict.
func (r *Neo4jEnterpriseStandaloneReconciler) patchStandalonePVCSize(ctx context.Context, pvc *corev1.PersistentVolumeClaim, desiredSize resource.Quantity) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, latest); err != nil {
			return err
		}
		latest.Spec.Resources.Requests[corev1.ResourceStorage] = desiredSize
		return r.Update(ctx, latest)
	})
}

// orphanDeleteStandaloneStatefulSet deletes a StatefulSet with the Orphan propagation policy.
func (r *Neo4jEnterpriseStandaloneReconciler) orphanDeleteStandaloneStatefulSet(ctx context.Context, namespace, name string) error {
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
