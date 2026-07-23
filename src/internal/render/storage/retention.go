package storage

import (
	appsv1 "k8s.io/api/apps/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// RetentionPolicy maps CR storage.volumeClaimRetention onto the StatefulSet field.
// Defaults are Retain/Retain (OP-2-005-UNINST-01).
func RetentionPolicy(neo4j *neo4jv1beta1.Neo4j) *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy {
	whenDeleted := appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	whenScaled := appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	if neo4j != nil && neo4j.Spec.Storage != nil && neo4j.Spec.Storage.VolumeClaimRetention != nil {
		r := neo4j.Spec.Storage.VolumeClaimRetention
		if r.WhenDeleted == neo4jv1beta1.VolumeClaimRetentionDelete {
			whenDeleted = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
		}
		if r.WhenScaled == neo4jv1beta1.VolumeClaimRetentionDelete {
			whenScaled = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
		}
	}
	return &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
		WhenDeleted: whenDeleted,
		WhenScaled:  whenScaled,
	}
}

// DeleteDataOnUninstall reports whether CR deletion should wipe Dynamic PVCs (UNINST-02).
func DeleteDataOnUninstall(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j != nil &&
		neo4j.Spec.Storage != nil &&
		neo4j.Spec.Storage.VolumeClaimRetention != nil &&
		neo4j.Spec.Storage.VolumeClaimRetention.WhenDeleted == neo4jv1beta1.VolumeClaimRetentionDelete
}

// ProtectedClaimNames are Existing.claimName PVCs the operator must never delete.
func ProtectedClaimNames(neo4j *neo4jv1beta1.Neo4j) map[string]struct{} {
	out := map[string]struct{}{}
	if neo4j == nil || neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil {
		return out
	}
	vols := neo4j.Spec.Storage.Volumes
	addExistingClaim := func(ex *neo4jv1beta1.ExistingVolumeSpec) {
		if ex != nil && ex.ClaimName != "" {
			out[ex.ClaimName] = struct{}{}
		}
	}
	if vols.Data.Mode == neo4jv1beta1.VolumeModeExisting {
		addExistingClaim(vols.Data.Existing)
	}
	for _, aux := range []*neo4jv1beta1.AuxiliaryVolumeSpec{
		vols.Backups, vols.Logs, vols.Metrics, vols.Import, vols.Licenses, vols.Plugins,
	} {
		if aux != nil && aux.Mode == neo4jv1beta1.VolumeModeExisting {
			addExistingClaim(aux.Existing)
		}
	}
	return out
}
