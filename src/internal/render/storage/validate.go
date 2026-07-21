package storage

import (
	"fmt"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// Validate checks storage modes and Existing oneOf shapes (BDR-005).
func Validate(neo4j *neo4jv1beta1.Neo4j) error {
	if neo4j.Spec.Storage == nil || neo4j.Spec.Storage.Volumes == nil {
		return fmt.Errorf("spec.storage.volumes is required")
	}
	vols := neo4j.Spec.Storage.Volumes
	if err := validateData(&vols.Data); err != nil {
		return err
	}
	for _, role := range auxRoles(vols) {
		if role.spec == nil {
			continue
		}
		if err := validateAux(role.name, role.spec); err != nil {
			return err
		}
	}
	for _, m := range neo4j.Spec.Storage.AdditionalMounts {
		if m.Name == "" || m.MountPath == "" {
			return fmt.Errorf("storage.additionalMounts require name and mountPath")
		}
	}
	for name, sm := range neo4j.Spec.Storage.SecretMounts {
		if sm.MountPath == "" {
			return fmt.Errorf("storage.secretMounts[%q].mountPath is required", name)
		}
	}
	return nil
}

func validateData(data *neo4jv1beta1.DataVolumeSpec) error {
	switch data.Mode {
	case neo4jv1beta1.VolumeModeDynamic:
		if data.Dynamic == nil || data.Dynamic.Size == "" {
			return fmt.Errorf("storage.volumes.data.dynamic.size is required when mode is Dynamic")
		}
		return nil
	case neo4jv1beta1.VolumeModeExisting:
		return validateExisting("data", data.Existing)
	case neo4jv1beta1.VolumeModeShare:
		return fmt.Errorf("storage.volumes.data.mode cannot be Share")
	default:
		return fmt.Errorf("storage.volumes.data.mode %q is unsupported", data.Mode)
	}
}

func validateAux(name string, aux *neo4jv1beta1.AuxiliaryVolumeSpec) error {
	mode := aux.Mode
	if mode == "" {
		mode = neo4jv1beta1.VolumeModeShare
	}
	switch mode {
	case neo4jv1beta1.VolumeModeShare:
		if aux.ShareFrom != nil && *aux.ShareFrom != neo4jv1beta1.ShareFromData {
			return fmt.Errorf("storage.volumes.%s.shareFrom must be data", name)
		}
		return nil
	case neo4jv1beta1.VolumeModeDynamic:
		if aux.Dynamic == nil || aux.Dynamic.Size == "" {
			return fmt.Errorf("storage.volumes.%s.dynamic.size is required when mode is Dynamic", name)
		}
		return nil
	case neo4jv1beta1.VolumeModeExisting:
		return validateExisting(name, aux.Existing)
	default:
		return fmt.Errorf("storage.volumes.%s.mode %q is unsupported", name, mode)
	}
}

func validateExisting(role string, existing *neo4jv1beta1.ExistingVolumeSpec) error {
	if existing == nil {
		return fmt.Errorf("storage.volumes.%s.existing is required when mode is Existing", role)
	}
	n := 0
	if existing.ClaimName != "" {
		n++
	}
	if existing.Volume != nil {
		n++
	}
	if existing.VolumeClaimTemplate != nil {
		n++
	}
	if n != 1 {
		return fmt.Errorf("storage.volumes.%s.existing requires exactly one of claimName, volume, or volumeClaimTemplate", role)
	}
	return nil
}
