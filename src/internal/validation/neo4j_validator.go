package validation

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/imagepolicy"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/plugins"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendercfg "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// Neo4jValidator is the validating admission webhook for Neo4j (ADR-001).
// Slice 1: NEO-001 security/hostPath; NEO-002 SA IAM; NEO-005 secrets; NEO-006 config injection.
type Neo4jValidator struct {
	Client client.Client // optional; when set, checks mountable Secret labels (NEO-005)
}

var _ admission.CustomValidator = &Neo4jValidator{}

func (v *Neo4jValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *Neo4jValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	// Finalizer removal is an UPDATE. Do not re-apply spec checks or a stale/invalid
	// CR cannot finish deleting (NEO-010).
	if neo4j, ok := newObj.(*neo4jv1beta1.Neo4j); ok && neo4j.DeletionTimestamp != nil && !neo4j.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	if err := validateMinimumMembersImmutable(oldObj, newObj); err != nil {
		return nil, err
	}
	return nil, v.validate(ctx, newObj)
}

// validateMinimumMembersImmutable rejects updates that change topology.minimumMembers
// (CEL also enforces this when the CRD is current; webhook covers enable-webhooks installs).
func validateMinimumMembersImmutable(oldObj, newObj runtime.Object) error {
	oldN, okOld := oldObj.(*neo4jv1beta1.Neo4j)
	newN, okNew := newObj.(*neo4jv1beta1.Neo4j)
	if !okOld || !okNew {
		return nil
	}
	oldM := oldN.Spec.Topology.MinimumMembers
	newM := newN.Spec.Topology.MinimumMembers
	switch {
	case oldM == nil && newM == nil:
		return nil
	case oldM == nil || newM == nil || *oldM != *newM:
		return fmt.Errorf("topology.minimumMembers cannot change after create (scale via topology.primaries.members)")
	default:
		return nil
	}
}

func (v *Neo4jValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *Neo4jValidator) validate(ctx context.Context, obj runtime.Object) error {
	if err := ValidateNeo4j(obj); err != nil {
		return err
	}
	if v.Client == nil {
		return nil
	}
	neo4j, ok := obj.(*neo4jv1beta1.Neo4j)
	if !ok {
		return fmt.Errorf("expected a Neo4j object, got %T", obj)
	}
	return rendersecrets.EnsureMountable(ctx, v.Client, neo4j)
}

// ValidateNeo4j runs CR-only admission checks (no API reads).
func ValidateNeo4j(obj runtime.Object) error {
	neo4j, ok := obj.(*neo4jv1beta1.Neo4j)
	if !ok {
		return fmt.Errorf("expected a Neo4j object, got %T", obj)
	}
	if err := renderwl.ValidateSecurity(neo4j); err != nil {
		return err
	}
	if err := renderwl.ValidateNetworkPolicy(neo4j); err != nil {
		return err
	}
	if err := renderwl.ValidatePDB(neo4j); err != nil {
		return err
	}
	if err := renderstorage.Validate(neo4j); err != nil {
		return err
	}
	if err := rendersecrets.ValidateSpec(neo4j); err != nil {
		return err
	}
	if err := rendercfg.ValidateConfig(neo4j); err != nil {
		return err
	}
	if err := imagepolicy.Validate(neo4j); err != nil {
		return err
	}
	if err := plugins.Validate(neo4j); err != nil {
		return err
	}
	return nil
}
