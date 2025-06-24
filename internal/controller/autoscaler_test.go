/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	controller "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("AutoScaler", func() {
	var (
		autoScaler *controller.AutoScaler
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		ctx        context.Context
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()

		scheme := runtime.NewScheme()
		Expect(neo4jv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(appsv1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		autoScaler = controller.NewAutoScaler(fakeClient)

		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1alpha1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Primaries:   3,
					Secondaries: 2,
				},
				Storage: neo4jv1alpha1.StorageSpec{
					ClassName: "standard",
					Size:      "10Gi",
				},
				AutoScaling: &neo4jv1alpha1.AutoScalingSpec{
					Enabled: true,
					Primaries: &neo4jv1alpha1.PrimaryAutoScalingConfig{
						Enabled:     true,
						MinReplicas: 1,
						MaxReplicas: 7,
						Metrics: []neo4jv1alpha1.AutoScalingMetric{
							{
								Type:   "cpu",
								Target: "70%",
								Weight: "1.0",
							},
						},
					},
					Secondaries: &neo4jv1alpha1.SecondaryAutoScalingConfig{
						Enabled:     true,
						MinReplicas: 0,
						MaxReplicas: 10,
						Metrics: []neo4jv1alpha1.AutoScalingMetric{
							{
								Type:   "cpu",
								Target: "80%",
								Weight: "1.0",
							},
						},
					},
				},
			},
		}
	})

	Context("When auto-scaling is enabled", func() {
		It("Should reconcile auto-scaling successfully", func() {
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			// Create the primary StatefulSet that the auto-scaler expects
			primaryStatefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-primary",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &[]int32{3}[0],
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: 3,
					Replicas:      3,
				},
			}
			Expect(fakeClient.Create(ctx, primaryStatefulSet)).Should(Succeed())

			// Create the secondary StatefulSet
			secondaryStatefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-secondary",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &[]int32{2}[0],
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: 2,
					Replicas:      2,
				},
			}
			Expect(fakeClient.Create(ctx, secondaryStatefulSet)).Should(Succeed())

			err := autoScaler.ReconcileAutoScaling(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should skip when auto-scaling is disabled", func() {
			cluster.Spec.AutoScaling.Enabled = false
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := autoScaler.ReconcileAutoScaling(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("MetricsCollector", func() {
		var metricsCollector *controller.MetricsCollector

		BeforeEach(func() {
			metricsCollector = controller.NewMetricsCollector(fakeClient, log.Log.WithName("test"))
		})

		It("Should collect cluster metrics", func() {
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			// Create the primary StatefulSet that the metrics collector expects
			primaryStatefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-primary",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &[]int32{3}[0],
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: 3,
					Replicas:      3,
				},
			}
			Expect(fakeClient.Create(ctx, primaryStatefulSet)).Should(Succeed())

			// Create the secondary StatefulSet
			secondaryStatefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-secondary",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &[]int32{2}[0],
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas: 2,
					Replicas:      2,
				},
			}
			Expect(fakeClient.Create(ctx, secondaryStatefulSet)).Should(Succeed())

			metrics, err := metricsCollector.CollectMetrics(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(metrics).NotTo(BeNil())
			Expect(metrics.Timestamp).NotTo(BeZero())
		})
	})

	Context("ScaleDecisionEngine", func() {
		var decisionEngine *controller.ScaleDecisionEngine

		BeforeEach(func() {
			decisionEngine = controller.NewScaleDecisionEngine(log.Log.WithName("test"))
		})

		It("Should calculate primary scaling decisions", func() {
			metrics := &controller.ClusterMetrics{
				PrimaryNodes: controller.NodeMetrics{
					Total:   3,
					Healthy: 3,
				},
			}

			decision := decisionEngine.CalculatePrimaryScaling(cluster, metrics)
			Expect(decision).NotTo(BeNil())
		})
	})
})
