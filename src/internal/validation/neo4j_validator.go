package validation

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	renderstorage "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/storage"
	renderwl "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/workload"
)

// Neo4jValidator is the validating admission webhook for Neo4j (ADR-001).
// Slice 1: NEO-001 — privileged / hostPath / unsafe security context and storage shape.
type Neo4jValidator struct{}

var _ admission.CustomValidator = &Neo4jValidator{}

func (v *Neo4jValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, ValidateNeo4j(obj)
}

func (v *Neo4jValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return nil, ValidateNeo4j(newObj)
}

func (v *Neo4jValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateNeo4j runs admission checks that must reject the CR before reconcile.
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
	return nil
}
