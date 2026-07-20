package workload

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// ServiceReaderRole allows Neo4j K8S discovery to list Services (Helm: neo4j-service-account.yaml).
func ServiceReaderRole(ctx render.Context) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.Name() + "-service-reader",
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("workload"),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints"},
				Verbs:     []string{"get", "watch", "list"},
			},
		},
	}
}

// ServiceReaderRoleBinding binds the operand ServiceAccount to ServiceReaderRole.
func ServiceReaderRoleBinding(ctx render.Context) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.Name() + "-service-binding",
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("workload"),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      ctx.OperandServiceAccountName(),
				Namespace: ctx.Namespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     ctx.Name() + "-service-reader",
		},
	}
}
