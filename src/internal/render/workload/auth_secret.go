package workload

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// AuthSecret builds the bootstrap auth Secret (NEO4J_AUTH).
func AuthSecret(ctx render.Context, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.AuthSecretName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("workload"),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"NEO4J_AUTH": "neo4j/" + password,
		},
	}
}
