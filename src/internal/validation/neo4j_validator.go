package validation

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// Neo4jValidator is the validating admission webhook for Neo4j (ADR-001).
// Slice 1: NEO-001 security/hostPath; NEO-005 secret mount policy.
type Neo4jValidator struct {
	Client client.Client // optional; when set, checks mountable Secret labels (NEO-005)
}

var _ admission.CustomValidator = &Neo4jValidator{}

func (v *Neo4jValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *Neo4jValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return nil, v.validate(ctx, newObj)
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
	if err := renderstorage.Validate(neo4j); err != nil {
		return err
	}
	if err := rendersecrets.ValidateSpec(neo4j); err != nil {
		return err
	}
	return nil
}
