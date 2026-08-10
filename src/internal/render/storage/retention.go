package storage

import (
	appsv1 "k8s.io/api/apps/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// RetentionPolicy maps CR storage.volumeClaimRetention onto the StatefulSet field.
// Defaults are Retain/Retain (OP-2-005-UNINST-01). whenDeleted follows the pinned
// status value once set (ADD-06).
func RetentionPolicy(neo4j *neo4jv1beta1.Neo4j) *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy {
	whenDeleted := appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	whenScaled := appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	if EffectiveWhenDeleted(neo4j) == neo4jv1beta1.VolumeClaimRetentionDelete {
		whenDeleted = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	}
	if neo4j != nil && neo4j.Spec.Storage != nil && neo4j.Spec.Storage.VolumeClaimRetention != nil {
		r := neo4j.Spec.Storage.VolumeClaimRetention
		if r.WhenScaled == neo4jv1beta1.VolumeClaimRetentionDelete {
			whenScaled = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
		}
	}
	return &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
		WhenDeleted: whenDeleted,
		WhenScaled:  whenScaled,
	}
}

// SpecWhenDeleted is the live spec value (default Retain).
func SpecWhenDeleted(neo4j *neo4jv1beta1.Neo4j) neo4jv1beta1.VolumeClaimRetentionPolicyType {
	if neo4j != nil &&
		neo4j.Spec.Storage != nil &&
		neo4j.Spec.Storage.VolumeClaimRetention != nil &&
		neo4j.Spec.Storage.VolumeClaimRetention.WhenDeleted == neo4jv1beta1.VolumeClaimRetentionDelete {
		return neo4jv1beta1.VolumeClaimRetentionDelete
	}
	return neo4jv1beta1.VolumeClaimRetentionRetain
}

// EffectiveWhenDeleted is the whenDeleted policy for STS / uninstall planning:
// status pin if present, otherwise live spec (pre-pin first reconcile).
func EffectiveWhenDeleted(neo4j *neo4jv1beta1.Neo4j) neo4jv1beta1.VolumeClaimRetentionPolicyType {
	if neo4j != nil && neo4j.Status.VolumeClaimRetentionWhenDeleted != nil {
		return *neo4j.Status.VolumeClaimRetentionWhenDeleted
	}
	return SpecWhenDeleted(neo4j)
}

// PinWhenDeleted snapshots SpecWhenDeleted into status once (ADD-06).
// Returns true when status was mutated and needs a status write.
func PinWhenDeleted(neo4j *neo4jv1beta1.Neo4j) bool {
	if neo4j == nil || neo4j.Status.VolumeClaimRetentionWhenDeleted != nil {
		return false
	}
	v := SpecWhenDeleted(neo4j)
	neo4j.Status.VolumeClaimRetentionWhenDeleted = &v
	return true
}

// DeleteDataOnUninstall reports whether CR deletion should wipe Dynamic PVCs (UNINST-02).
// Honors the pinned status value only — fail-safe Preserve when unset (ADD-06).
func DeleteDataOnUninstall(neo4j *neo4jv1beta1.Neo4j) bool {
	if neo4j == nil || neo4j.Status.VolumeClaimRetentionWhenDeleted == nil {
		return false
	}
	return *neo4j.Status.VolumeClaimRetentionWhenDeleted == neo4jv1beta1.VolumeClaimRetentionDelete
}

// DeleteDataOnScale reports whether the operator may delete Dynamic member PVCs on
// scale-in / Dropped-store recycle (NEO-007). Default is Retain — STS retention and
// operator wipe must agree; otherwise whenScaled:Retain is a false promise.
func DeleteDataOnScale(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j != nil &&
		neo4j.Spec.Storage != nil &&
		neo4j.Spec.Storage.VolumeClaimRetention != nil &&
		neo4j.Spec.Storage.VolumeClaimRetention.WhenScaled == neo4jv1beta1.VolumeClaimRetentionDelete
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
