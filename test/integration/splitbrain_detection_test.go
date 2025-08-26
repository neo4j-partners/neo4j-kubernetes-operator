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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Split-Brain Detection Integration Tests", func() {
	const (
		timeout  = time.Second * 1200 // 20 minutes for CI environment constraints (split-brain detection and repair)
		interval = time.Second * 10   // Increased polling interval to reduce API load in CI
	)

	var testNamespace string
	var cluster *neo4jv1alpha1.Neo4jEnterpriseCluster

	BeforeEach(func() {
		By("Creating test namespace")
		testNamespace = createTestNamespace("splitbrain")

		By("Creating admin secret for Neo4j authentication")
		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("admin123"),
			},
		}
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())
	})

	AfterEach(func() {
		// Critical: Clean up resources immediately to prevent CI resource exhaustion
		if cluster != nil {
			By("Cleaning up cluster in split-brain test AfterEach")
			// Remove finalizers first
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			// Delete the resource
			_ = k8sClient.Delete(ctx, cluster)
			cluster = nil
		}
		// Clean up any remaining resources in namespace
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	Context("When cluster experiences split-brain during startup", func() {
		It("should detect and repair split-brain automatically", func() {
			By("Creating a 3-server cluster that may experience split-brain")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splitbrain-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "disabled",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Monitoring cluster for split-brain detection events")
			var detectedSplitBrain bool
			var repairedSplitBrain bool

			Eventually(func() bool {
				// Check for split-brain related events
				eventList := &corev1.EventList{}
				err := k8sClient.List(ctx, eventList, &client.ListOptions{
					Namespace: testNamespace,
				})
				if err != nil {
					return false
				}

				for _, event := range eventList.Items {
					if event.InvolvedObject.Name == cluster.Name &&
						event.InvolvedObject.Kind == "Neo4jEnterpriseCluster" {

						if event.Reason == "SplitBrainDetected" {
							detectedSplitBrain = true
							GinkgoWriter.Printf("Split-brain detected: %s\n", event.Message)
						}

						if event.Reason == "SplitBrainRepaired" {
							repairedSplitBrain = true
							GinkgoWriter.Printf("Split-brain repaired: %s\n", event.Message)
						}
					}
				}

				// Also check cluster status
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					return false
				}

				// If no split-brain events but cluster is ready, that's also good
				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						if !detectedSplitBrain {
							GinkgoWriter.Println("Cluster formed successfully without split-brain")
							return true
						}
						if repairedSplitBrain {
							GinkgoWriter.Println("Cluster formed successfully after split-brain repair")
							return true
						}
					}
				}

				return false
			}, timeout, interval).Should(BeTrue(), "Should either form cluster without split-brain or detect and repair split-brain")

			By("Verifying cluster reached healthy state")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					return false
				}

				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Cluster should eventually be ready")

			By("Verifying all server pods are running")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{
					"neo4j.com/cluster":    cluster.Name,
					"neo4j.com/clustering": "true",
				})
				if err != nil {
					return 0
				}

				runningCount := 0
				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						runningCount++
					}
				}
				return runningCount
			}, timeout, interval).Should(Equal(3), "All 3 server pods should be running")

			By("Immediately cleaning up cluster to prevent CI resource exhaustion")
			if cluster != nil {
				err := k8sClient.Delete(ctx, cluster)
				if err == nil {
					// Wait briefly for deletion to start
					time.Sleep(time.Second * 5)
				}
			}
		})
	})

	Context("When testing split-brain prevention", func() {
		It("should maintain cluster health during pod restarts", func() {
			By("Creating a stable 3-server cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stable-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "500Mi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: "neo4j-admin-secret",
					},
					Resources: getCIAppropriateResourceRequirements(), // Automatically adjusts for CI vs local environments
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "disabled",
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
							Value: "eval",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for cluster to be ready")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					return false
				}

				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Simulating pod failure by deleting one server pod")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{
				"neo4j.com/cluster":    cluster.Name,
				"neo4j.com/clustering": "true",
			})).To(Succeed())

			Expect(len(podList.Items)).To(BeNumerically(">", 0))
			podToDelete := podList.Items[0]

			GinkgoWriter.Printf("Deleting pod %s to simulate failure\n", podToDelete.Name)
			Expect(k8sClient.Delete(ctx, &podToDelete)).To(Succeed())

			By("Verifying split-brain detection monitors the situation")
			time.Sleep(60 * time.Second) // Give more time for detection and pod rescheduling in CI

			By("Verifying cluster eventually recovers")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					return false
				}

				// Cluster should remain ready or return to ready state
				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Cluster should recover after pod failure")

			By("Verifying all pods are running again")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{
					"neo4j.com/cluster":    cluster.Name,
					"neo4j.com/clustering": "true",
				})
				if err != nil {
					GinkgoWriter.Printf("Error listing pods: %v\n", err)
					return 0
				}

				runningCount := 0
				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						// Check that all containers in the pod are ready
						allContainersReady := true
						for _, containerStatus := range pod.Status.ContainerStatuses {
							if !containerStatus.Ready {
								allContainersReady = false
								GinkgoWriter.Printf("Pod %s container %s not ready: %s\n",
									pod.Name, containerStatus.Name, containerStatus.State.String())
								break
							}
						}
						if allContainersReady && len(pod.Status.ContainerStatuses) > 0 {
							runningCount++
						}
					} else {
						GinkgoWriter.Printf("Pod %s not running: phase=%s", pod.Name, pod.Status.Phase)
						// Add more detail about why pod isn't running
						for _, condition := range pod.Status.Conditions {
							if condition.Status != corev1.ConditionTrue {
								GinkgoWriter.Printf(" [%s=%s: %s]", condition.Type, condition.Status, condition.Reason)
							}
						}
						GinkgoWriter.Printf("\n")
					}
				}
				GinkgoWriter.Printf("Currently %d of 3 pods are running and ready\n", runningCount)
				return runningCount
			}, timeout, interval).Should(Equal(3), "All 3 server pods should be running and ready")
		})
	})
})
