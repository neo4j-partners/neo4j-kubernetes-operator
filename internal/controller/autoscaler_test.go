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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("AutoScaler", func() {
	var (
		autoScaler *AutoScaler
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		ctx        context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()

		scheme := runtime.NewScheme()
		_ = neo4jv1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		autoScaler = NewAutoScaler(fakeClient)

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
			Expect(autoScaler.client.Create(ctx, cluster)).Should(Succeed())

			err := autoScaler.ReconcileAutoScaling(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should skip when auto-scaling is disabled", func() {
			cluster.Spec.AutoScaling.Enabled = false
			Expect(autoScaler.client.Create(ctx, cluster)).Should(Succeed())

			err := autoScaler.ReconcileAutoScaling(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should ensure odd replicas for primary nodes", func() {
			result := autoScaler.ensureOddReplicas(4, 1, 7)
			Expect(result).To(Equal(int32(5)))

			result = autoScaler.ensureOddReplicas(6, 1, 7)
			Expect(result).To(Equal(int32(7)))

			result = autoScaler.ensureOddReplicas(2, 1, 7)
			Expect(result).To(Equal(int32(3)))
		})

		It("Should handle quorum protection validation", func() {
			cluster.Spec.AutoScaling.Primaries.QuorumProtection = &neo4jv1alpha1.QuorumProtectionConfig{
				Enabled:             true,
				MinHealthyPrimaries: 2,
			}

			metrics := &ClusterMetrics{
				PrimaryNodes: NodeMetrics{
					Total:   3,
					Healthy: 3,
				},
			}

			err := autoScaler.validateQuorumProtection(ctx, cluster, metrics)
			Expect(err).NotTo(HaveOccurred())

			// Test insufficient healthy primaries
			metrics.PrimaryNodes.Healthy = 1
			err = autoScaler.validateQuorumProtection(ctx, cluster, metrics)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("MetricsCollector", func() {
		var metricsCollector *MetricsCollector

		BeforeEach(func() {
			metricsCollector = NewMetricsCollector(autoScaler.client, log.Log.WithName("test"))
		})

		It("Should collect cluster metrics", func() {
			Expect(autoScaler.client.Create(ctx, cluster)).Should(Succeed())

			metrics, err := metricsCollector.CollectMetrics(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(metrics).NotTo(BeNil())
			Expect(metrics.Timestamp).NotTo(BeZero())
		})

		It("Should determine pod health correctly", func() {
			healthyPod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			Expect(metricsCollector.isPodHealthy(healthyPod)).To(BeTrue())

			unhealthyPod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			}
			Expect(metricsCollector.isPodHealthy(unhealthyPod)).To(BeFalse())
		})
	})

	Context("ScaleDecisionEngine", func() {
		var decisionEngine *ScaleDecisionEngine

		BeforeEach(func() {
			decisionEngine = NewScaleDecisionEngine(log.Log.WithName("test"))
		})

		It("Should calculate primary scaling decisions", func() {
			metrics := &ClusterMetrics{
				PrimaryNodes: NodeMetrics{
					Total:   3,
					Healthy: 3,
					CPU: MetricValue{
						Current: 85.0, // High CPU
						Trend:   TrendIncreasing,
					},
				},
			}

			decision := decisionEngine.CalculatePrimaryScaling(cluster, metrics)
			Expect(decision).NotTo(BeNil())
			Expect(decision.Action).NotTo(Equal(ScaleActionNone))
		})

		It("Should calculate secondary scaling decisions", func() {
			metrics := &ClusterMetrics{
				SecondaryNodes: NodeMetrics{
					Total:   2,
					Healthy: 2,
					CPU: MetricValue{
						Current: 90.0, // Very high CPU
						Trend:   TrendIncreasing,
					},
				},
			}

			decision := decisionEngine.CalculateSecondaryScaling(cluster, metrics)
			Expect(decision).NotTo(BeNil())
			Expect(decision.Action).NotTo(Equal(ScaleActionNone))
		})

		It("Should evaluate CPU metrics correctly", func() {
			metrics := &ClusterMetrics{
				PrimaryNodes: NodeMetrics{
					CPU: MetricValue{
						Current: 75.0,
						Trend:   TrendIncreasing,
					},
				},
			}

			metricConfig := neo4jv1alpha1.AutoScalingMetric{
				Type:   "cpu",
				Target: "70%",
				Weight: "1.0",
			}

			score, reason := decisionEngine.evaluateCPUMetric(metricConfig, metrics)
			Expect(score).To(BeNumerically(">", 0))
			Expect(reason).NotTo(BeEmpty())
		})

		It("Should evaluate memory metrics correctly", func() {
			metrics := &ClusterMetrics{
				PrimaryNodes: NodeMetrics{
					Memory: MetricValue{
						Current: 85.0,
						Trend:   TrendIncreasing,
					},
				},
			}

			metricConfig := neo4jv1alpha1.AutoScalingMetric{
				Type:   "memory",
				Target: "80%",
				Weight: "1.0",
			}

			score, reason := decisionEngine.evaluateMemoryMetric(metricConfig, metrics)
			Expect(score).To(BeNumerically(">", 0))
			Expect(reason).NotTo(BeEmpty())
		})
	})

	Context("Zone-aware scaling", func() {
		BeforeEach(func() {
			cluster.Spec.AutoScaling.Secondaries.ZoneAware = &neo4jv1alpha1.ZoneAwareScalingConfig{
				Enabled:            true,
				MinReplicasPerZone: 1,
				MaxZoneSkew:        2,
			}
		})

		It("Should calculate target zone distribution", func() {
			currentDistribution := map[string]int32{
				"zone-a": 2,
				"zone-b": 1,
				"zone-c": 0,
			}

			targetDistribution := autoScaler.calculateTargetZoneDistribution(
				6, currentDistribution, cluster.Spec.AutoScaling.Secondaries.ZoneAware)

			Expect(targetDistribution).NotTo(BeEmpty())

			// Verify total replicas
			total := int32(0)
			for _, count := range targetDistribution {
				total += count
			}
			Expect(total).To(Equal(int32(6)))
		})
	})
})
