package trust

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/domain/shared"
	rendertrust "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/trust"
)

// Reconciler validates TLS Secrets for BYO trust (BDR-006 / BDR-011).
// Volume mounts and conf keys are applied by workload/serverconfig render.
type Reconciler struct {
	Client client.Client
}

func New(c client.Client) *Reconciler {
	return &Reconciler{Client: c}
}

func (r *Reconciler) Reconcile(ctx context.Context, neo4j *neo4jv1beta1.Neo4j) shared.StepResult {
	if err := rendertrust.ValidateBYO(neo4j); err != nil {
		return shared.Failed(err)
	}
	if !rendertrust.TrustEnabled(neo4j) {
		return shared.Done()
	}
	for _, name := range rendertrust.BYOSecretNames(neo4j) {
		if err := r.requireSecret(ctx, neo4j.Namespace, name); err != nil {
			return shared.Failed(err)
		}
	}
	return shared.Done()
}

func (r *Reconciler) requireSecret(ctx context.Context, namespace, name string) error {
	var secret corev1.Secret
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("trust secret %q not found in namespace %q", name, namespace)
		}
		return fmt.Errorf("get trust secret %q: %w", name, err)
	}
	return nil
}
