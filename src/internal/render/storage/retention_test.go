package storage

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestRetentionPolicyDefaultsRetain(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{}
	p := RetentionPolicy(neo4j)
	if p.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType ||
		p.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Fatalf("defaults = %#v", p)
	}
	if DeleteDataOnUninstall(neo4j) {
		t.Fatal("expected retain uninstall")
	}
}

func TestRetentionPolicyDelete(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				VolumeClaimRetention: &neo4jv1beta1.VolumeClaimRetentionPolicySpec{
					WhenDeleted: neo4jv1beta1.VolumeClaimRetentionDelete,
					WhenScaled:  neo4jv1beta1.VolumeClaimRetentionDelete,
				},
			},
		},
	}
	p := RetentionPolicy(neo4j)
	if p.WhenDeleted != appsv1.DeletePersistentVolumeClaimRetentionPolicyType ||
		p.WhenScaled != appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
		t.Fatalf("delete policy = %#v", p)
	}
	if !DeleteDataOnUninstall(neo4j) {
		t.Fatal("expected delete uninstall")
	}
}

func TestProtectedClaimNames(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:     neo4jv1beta1.VolumeModeExisting,
						Existing: &neo4jv1beta1.ExistingVolumeSpec{ClaimName: "keep-me"},
					},
					Logs: &neo4jv1beta1.AuxiliaryVolumeSpec{
						Mode:     neo4jv1beta1.VolumeModeExisting,
						Existing: &neo4jv1beta1.ExistingVolumeSpec{ClaimName: "keep-logs"},
					},
				},
			},
		},
	}
	got := ProtectedClaimNames(neo4j)
	if _, ok := got["keep-me"]; !ok {
		t.Fatal("missing keep-me")
	}
	if _, ok := got["keep-logs"]; !ok {
		t.Fatal("missing keep-logs")
	}
}
