package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestIsUpgradeRequiredSingleStatefulSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = neo4jv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.27.0-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
		},
		Status: neo4jv1alpha1.Neo4jEnterpriseClusterStatus{
			Phase: "Ready",
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-server",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: pointer.Int32(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "neo4j",
							Image: "neo4j:5.26.0-enterprise",
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 3,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, sts).
		Build()

	reconciler := &Neo4jEnterpriseClusterReconciler{Client: fakeClient}

	if !reconciler.isUpgradeRequired(context.Background(), cluster) {
		t.Fatalf("expected upgrade to be required when StatefulSet image differs")
	}

	existing := &appsv1.StatefulSet{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "demo-server",
		Namespace: "default",
	}, existing); err != nil {
		t.Fatalf("failed to fetch StatefulSet: %v", err)
	}
	existing.Spec.Template.Spec.Containers[0].Image = "neo4j:5.27.0-enterprise"
	if err := fakeClient.Update(context.Background(), existing); err != nil {
		t.Fatalf("failed to update StatefulSet: %v", err)
	}

	if reconciler.isUpgradeRequired(context.Background(), cluster) {
		t.Fatalf("expected upgrade not to be required when images match")
	}
}
