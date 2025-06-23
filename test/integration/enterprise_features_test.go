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

package integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Enterprise Features Integration Tests", func() {
	var (
		namespace string
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = fmt.Sprintf("test-enterprise-%d", time.Now().Unix())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Create admin secret
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"NEO4J_AUTH": []byte("neo4j/testpassword123"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	})

	AfterEach(func() {
		// Clean up namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	Describe("Auto-Scaling Feature", func() {
		It("Should validate auto-scaling configuration", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "autoscaling-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
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
							Enabled:          true,
							MinReplicas:      1,
							MaxReplicas:      7,
							AllowQuorumBreak: false,
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
							MaxReplicas: 20,
							Metrics: []neo4jv1alpha1.AutoScalingMetric{
								{
									Type:   "cpu",
									Target: "80%",
									Weight: "1.0",
								},
							},
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}

			By("Creating the Neo4j cluster with auto-scaling")
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Verifying cluster configuration is accepted")
			Eventually(func() error {
				updated := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "autoscaling-cluster",
					Namespace: namespace,
				}, updated)
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Checking auto-scaling configuration")
			updated := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "autoscaling-cluster",
				Namespace: namespace,
			}, updated)).To(Succeed())

			Expect(updated.Spec.AutoScaling.Enabled).To(BeTrue())
			Expect(updated.Spec.AutoScaling.Primaries.MinReplicas).To(Equal(int32(1)))
			Expect(updated.Spec.AutoScaling.Primaries.MaxReplicas).To(Equal(int32(7)))
			Expect(updated.Spec.AutoScaling.Secondaries.MaxReplicas).To(Equal(int32(20)))
		})
	})

	Describe("Plugin Management Feature", func() {
		It("Should create and manage Neo4j plugins", func() {
			// First create a cluster
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plugin-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "10Gi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create a plugin
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "apoc-plugin",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: "plugin-cluster",
					Name:       "apoc",
					Version:    "5.26.0",
					Enabled:    true,
					Source: &neo4jv1alpha1.PluginSource{
						Type: "official",
					},
					Config: map[string]string{
						"apoc.export.file.enabled": "true",
						"apoc.import.file.enabled": "true",
					},
				},
			}

			By("Creating the Neo4j plugin")
			Expect(k8sClient.Create(ctx, plugin)).To(Succeed())

			By("Verifying plugin is processed")
			Eventually(func() error {
				updated := &neo4jv1alpha1.Neo4jPlugin{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "apoc-plugin",
					Namespace: namespace,
				}, updated)
			}, time.Minute*2, time.Second*5).Should(Succeed())
		})
	})

	Describe("Query Monitoring Feature", func() {
		It("Should configure query monitoring", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "monitoring-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "10Gi",
					},
					QueryMonitoring: &neo4jv1alpha1.QueryMonitoringSpec{
						Enabled:              true,
						SlowQueryThreshold:   "2s",
						ExplainPlan:          true,
						IndexRecommendations: true,
						Sampling: &neo4jv1alpha1.QuerySamplingConfig{
							Rate:                "0.1",
							MaxQueriesPerSecond: 100,
						},
						MetricsExport: &neo4jv1alpha1.QueryMetricsExportConfig{
							Prometheus: true,
							Interval:   "30s",
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
				},
			}

			By("Creating the cluster with query monitoring")
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Verifying cluster configuration is accepted")
			Eventually(func() error {
				updated := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "monitoring-cluster",
					Namespace: namespace,
				}, updated)
			}, time.Minute*2, time.Second*5).Should(Succeed())

			By("Checking query monitoring configuration")
			updated := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "monitoring-cluster",
				Namespace: namespace,
			}, updated)).To(Succeed())

			Expect(updated.Spec.QueryMonitoring.Enabled).To(BeTrue())
			Expect(updated.Spec.QueryMonitoring.SlowQueryThreshold).To(Equal("2s"))
			Expect(updated.Spec.QueryMonitoring.ExplainPlan).To(BeTrue())
			Expect(updated.Spec.QueryMonitoring.IndexRecommendations).To(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
		})
	})
})
