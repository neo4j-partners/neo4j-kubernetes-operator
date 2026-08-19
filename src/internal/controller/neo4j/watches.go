package neo4j

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

// mapSecretToNeo4j enqueues CRs that mount this Secret. cert-manager TLS Secrets
// are owned by the Certificate, not the Neo4j CR, so Owns(Secret) never sees them
// (ADR-009). The mountable label keeps ServiceAccount tokens out of the queue.
func (r *Neo4jReconciler) mapSecretToNeo4j(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	if secret.Labels == nil || secret.Labels[rendersecrets.MountableLabel] != rendersecrets.MountableLabelValue {
		return nil
	}
	var list neo4jv1beta1.Neo4jList
	if err := r.List(ctx, &list, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range list.Items {
		n := &list.Items[i]
		if !crMountsSecret(n, secret.Name) {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: n.Name, Namespace: n.Namespace},
		})
	}
	return reqs
}

func crMountsSecret(neo4j *neo4jv1beta1.Neo4j, name string) bool {
	if rendertrust.ReferencesSecret(neo4j, name) {
		return true
	}
	for _, n := range rendersecrets.ReferencedMountSecrets(neo4j) {
		if n == name {
			return true
		}
	}
	return false
}
