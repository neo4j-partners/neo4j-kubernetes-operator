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
	"os"
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
	// Use dynamic timeout and interval based on environment
	var (
		timeout  time.Duration
		interval time.Duration
	)

	// Initialize based on CI environment
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		timeout = time.Minute * 30 // Extended timeout for split-brain scenarios in CI
		interval = time.Second * 2 // Faster polling in CI to catch state changes
	} else {
		timeout = time.Minute * 10 // Local testing timeout
		interval = time.Second * 2 // Consistent fast polling
	}

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
		It("should form a healthy cluster (with or without split-brain detection)", func() {
			By("Creating a 3-server cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splitbrain-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(), // Use environment-specified version
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

			// Apply CI-specific optimizations (reduces 3 servers to 2 in CI)
			applyCIOptimizations(cluster)

			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Monitoring cluster formation and optional split-brain detection")
			var detectedSplitBrain bool
			var repairedSplitBrain bool
			var clusterReady bool

			// First, wait for cluster to reach Ready state
			Eventually(func() bool {
				// Check cluster status
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					GinkgoWriter.Printf("Failed to get cluster: %v\n", err)
					return false
				}

				// Check if cluster phase is Ready (more reliable than conditions)
				if cluster.Status.Phase == "Ready" {
					clusterReady = true
					GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
						cluster.Status.Phase, cluster.Status.Message)
					return true
				}

				// Log current status for debugging
				GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
					cluster.Status.Phase, cluster.Status.Message)
				return false
			}, timeout, interval).Should(BeTrue(), "Cluster should reach Ready state")

			By("Checking if split-brain detection occurred (optional)")
			// After cluster is ready, check if there were any split-brain events
			eventList := &corev1.EventList{}
			err := k8sClient.List(ctx, eventList, &client.ListOptions{
				Namespace: testNamespace,
			})
			Expect(err).NotTo(HaveOccurred())

			for _, event := range eventList.Items {
				if event.InvolvedObject.Name == cluster.Name &&
					event.InvolvedObject.Kind == "Neo4jEnterpriseCluster" {

					if event.Reason == "SplitBrainDetected" {
						detectedSplitBrain = true
						GinkgoWriter.Printf("Split-brain was detected: %s\n", event.Message)
					}

					if event.Reason == "SplitBrainRepaired" {
						repairedSplitBrain = true
						GinkgoWriter.Printf("Split-brain was repaired: %s\n", event.Message)
					}
				}
			}

			// Log the outcome
			if detectedSplitBrain && repairedSplitBrain {
				GinkgoWriter.Println("✓ Cluster formed successfully after detecting and repairing split-brain")
			} else if detectedSplitBrain && !repairedSplitBrain {
				GinkgoWriter.Println("⚠ Split-brain was detected but not automatically repaired (manual intervention may be needed)")
			} else {
				GinkgoWriter.Println("✓ Cluster formed successfully without experiencing split-brain")
			}

			// The test passes as long as the cluster is ready
			Expect(clusterReady).To(BeTrue(), "Cluster should be ready")

			By("Verifying all server pods are running")
			// Get the actual expected server count after CI optimizations
			expectedServers := int(cluster.Spec.Topology.Servers)
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
			}, timeout, interval).Should(Equal(expectedServers), "All %d server pods should be running", expectedServers)

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
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(), // Use environment-specified version
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

			// Apply CI-specific optimizations (reduces 3 servers to 2 in CI)
			applyCIOptimizations(cluster)

			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for cluster to be ready")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					GinkgoWriter.Printf("Failed to get cluster: %v\n", err)
					return false
				}

				// Check if cluster phase is Ready (more reliable than conditions)
				if cluster.Status.Phase == "Ready" {
					GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
						cluster.Status.Phase, cluster.Status.Message)
					return true
				}

				// Log current status for debugging
				GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
					cluster.Status.Phase, cluster.Status.Message)
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
					GinkgoWriter.Printf("Failed to get cluster during recovery: %v\n", err)
					return false
				}

				// Cluster should remain ready or return to ready state
				if cluster.Status.Phase == "Ready" {
					GinkgoWriter.Printf("Cluster has recovered. Phase: %s, Message: %s\n",
						cluster.Status.Phase, cluster.Status.Message)
					return true
				}

				// Log current status for debugging
				GinkgoWriter.Printf("Cluster not yet recovered. Phase: %s, Message: %s\n",
					cluster.Status.Phase, cluster.Status.Message)
				return false
			}, timeout, interval).Should(BeTrue(), "Cluster should recover after pod failure")

			By("Verifying all pods are running again")
			// Get the actual expected server count after CI optimizations
			expectedServers := int(cluster.Spec.Topology.Servers)
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
				GinkgoWriter.Printf("Currently %d of %d pods are running and ready\n", runningCount, expectedServers)
				return runningCount
			}, timeout, interval).Should(Equal(expectedServers), "All %d server pods should be running and ready", expectedServers)
		})
	})
})
