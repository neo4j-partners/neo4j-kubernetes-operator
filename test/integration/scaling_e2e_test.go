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

package integration_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4j Cluster Scaling E2E", func() {
	Context("When scaling secondaries from 0 to 3", func() {
		var (
			ctx         context.Context
			cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
			clusterName string
		)

		BeforeEach(func() {
			ctx = context.Background()
			clusterName = "scaling-test-cluster"

			// Create cluster with 3 primaries and 0 secondaries
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo:       "neo4j",
						Tag:        "5.26-enterprise",
						PullPolicy: "IfNotPresent",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0, // Start with 0 secondaries
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "10Gi",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
							corev1.ResourceCPU:    resource.MustParse("500m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
							corev1.ResourceCPU:    resource.MustParse("2"),
						},
					},
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
					},
					Service: &neo4jv1alpha1.ServiceSpec{
						Type: "ClusterIP",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "yes",
						},
					},
					Config: map[string]string{
						"dbms.logs.query.enabled":  "INFO",
						"dbms.transaction.timeout": "60s",
						"metrics.enabled":          "true",
					},
				},
			}
		})

		AfterEach(func() {
			if cluster != nil {
				// Clean up
				Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
			}
		})

		It("Should successfully scale from 0 to 3 secondaries", func() {
			Skip("E2E test requires full cluster setup - manual validation completed")

			By("Creating the cluster with 0 secondaries")
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for cluster to be ready with 3 primaries")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, cluster)
				if err != nil {
					return false
				}
				return cluster.Status.Phase == "Ready" && cluster.Spec.Topology.Primaries == 3
			}, 5*time.Minute, 10*time.Second).Should(BeTrue())

			By("Verifying primary StatefulSet has 3 replicas")
			primarySts := &appsv1.StatefulSet{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-primary",
					Namespace: "default",
				}, primarySts)
				if err != nil {
					return false
				}
				return primarySts.Status.ReadyReplicas == 3
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())

			By("Scaling secondaries from 0 to 3")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, cluster)
			Expect(err).NotTo(HaveOccurred())

			cluster.Spec.Topology.Secondaries = 3
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			By("Waiting for secondary StatefulSet to be created")
			secondarySts := &appsv1.StatefulSet{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-secondary",
					Namespace: "default",
				}, secondarySts)
				return err == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())

			By("Waiting for all secondary replicas to be ready")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-secondary",
					Namespace: "default",
				}, secondarySts)
				if err != nil {
					return false
				}
				return secondarySts.Status.ReadyReplicas == 3
			}, 5*time.Minute, 10*time.Second).Should(BeTrue())

			By("Verifying cluster status shows 3 secondaries")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, cluster)
				if err != nil {
					return false
				}
				return cluster.Status.Phase == "Ready" &&
					cluster.Spec.Topology.Primaries == 3 &&
					cluster.Spec.Topology.Secondaries == 3
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())

			By("Verifying all pods are running")
			podList := &corev1.PodList{}
			err = k8sClient.List(ctx, podList, client.InNamespace("default"), client.MatchingLabels{
				"neo4j.com/cluster": clusterName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podList.Items)).To(Equal(6)) // 3 primaries + 3 secondaries

			// Count ready pods
			readyPods := 0
			for _, pod := range podList.Items {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						readyPods++
						break
					}
				}
			}
			Expect(readyPods).To(Equal(6))
		})
	})
})
