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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
)

var _ = Describe("SplitBrainDetector", func() {
	var (
		ctx           context.Context
		detector      *SplitBrainDetector
		fakeClient    client.Client
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
		testNamespace string
		scheme        *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = "test-namespace"

		// Create scheme
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(neo4jv1alpha1.AddToScheme(scheme)).To(Succeed())

		// Create test cluster
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: testNamespace,
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Servers: 3,
				},
			},
		}

		// Create fake client
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster).
			Build()

		detector = NewSplitBrainDetector(fakeClient)
	})

	Describe("NewSplitBrainDetector", func() {
		It("should create a new detector instance", func() {
			Expect(detector).NotTo(BeNil())
			Expect(detector.Client).To(Equal(fakeClient))
		})
	})

	Describe("analyzeClusterViews", func() {
		Context("with single server cluster", func() {
			It("should not detect split-brain", func() {
				cluster.Spec.Topology.Servers = 1

				analysis := detector.analyzeClusterViews([]ClusterView{}, 1)

				Expect(analysis.IsSplitBrain).To(BeFalse())
				Expect(analysis.RepairAction).To(Equal(RepairActionNone))
			})
		})

		Context("with no working views", func() {
			It("should require investigation", func() {
				views := []ClusterView{
					{
						ServerPodName:   "pod-0",
						ConnectionError: fmt.Errorf("connection failed"),
					},
					{
						ServerPodName:   "pod-1",
						ConnectionError: fmt.Errorf("connection failed"),
					},
				}

				analysis := detector.analyzeClusterViews(views, 3)

				Expect(analysis.RepairAction).To(Equal(RepairActionRestartAll))
				Expect(analysis.ErrorMessage).To(ContainSubstring("Cannot connect to any Neo4j server pods"))
			})
		})

		Context("with too many connection failures", func() {
			It("should require investigation", func() {
				views := []ClusterView{
					{
						ServerPodName: "pod-0",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName:   "pod-1",
						ConnectionError: fmt.Errorf("connection failed"),
					},
					{
						ServerPodName:   "pod-2",
						ConnectionError: fmt.Errorf("connection failed"),
					},
				}

				analysis := detector.analyzeClusterViews(views, 3)

				Expect(analysis.RepairAction).To(Equal(RepairActionInvestigate))
				Expect(analysis.ErrorMessage).To(ContainSubstring("Too many connection failures"))
			})
		})

		Context("with split-brain scenario", func() {
			It("should detect multiple cluster groups", func() {
				views := []ClusterView{
					{
						ServerPodName: "pod-0",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName: "pod-1",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName: "pod-2",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-2", Address: "pod-2:7687", State: "Enabled", Health: "Available"},
						},
					},
				}

				analysis := detector.analyzeClusterViews(views, 3)

				Expect(analysis.IsSplitBrain).To(BeTrue())
				Expect(analysis.RepairAction).To(Equal(RepairActionRestartPods))
				Expect(analysis.OrphanedPods).To(ContainElement("pod-2"))
				Expect(analysis.ErrorMessage).To(ContainSubstring("Split-brain detected: 2 cluster groups found"))
			})
		})

		Context("with partial cluster formation", func() {
			It("should detect missing servers", func() {
				views := []ClusterView{
					{
						ServerPodName: "pod-0",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName: "pod-1",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName: "pod-2",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
						},
					},
				}

				analysis := detector.analyzeClusterViews(views, 3)

				Expect(analysis.IsSplitBrain).To(BeFalse())
				Expect(analysis.RepairAction).To(Equal(RepairActionWaitForming))
				Expect(analysis.ErrorMessage).To(ContainSubstring("Cluster formation in progress: 2/3 servers visible"))
			})
		})

		Context("with healthy cluster", func() {
			It("should not detect issues", func() {
				views := []ClusterView{
					{
						ServerPodName: "pod-0",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
							{Name: "server-2", Address: "pod-2:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName: "pod-1",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
							{Name: "server-2", Address: "pod-2:7687", State: "Enabled", Health: "Available"},
						},
					},
					{
						ServerPodName: "pod-2",
						Servers: []neo4jclient.ServerInfo{
							{Name: "server-0", Address: "pod-0:7687", State: "Enabled", Health: "Available"},
							{Name: "server-1", Address: "pod-1:7687", State: "Enabled", Health: "Available"},
							{Name: "server-2", Address: "pod-2:7687", State: "Enabled", Health: "Available"},
						},
					},
				}

				analysis := detector.analyzeClusterViews(views, 3)

				Expect(analysis.IsSplitBrain).To(BeFalse())
				Expect(analysis.RepairAction).To(Equal(RepairActionNone))
				Expect(len(analysis.OrphanedPods)).To(Equal(0))
			})
		})
	})

	Describe("getServerPods", func() {
		Context("with server pods", func() {
			It("should return pods with correct labels", func() {
				pods := []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cluster-server-0",
							Namespace: testNamespace,
							Labels: map[string]string{
								"neo4j.com/cluster":    "test-cluster",
								"neo4j.com/clustering": "true",
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cluster-server-1",
							Namespace: testNamespace,
							Labels: map[string]string{
								"neo4j.com/cluster":    "test-cluster",
								"neo4j.com/clustering": "true",
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
				}

				// Add pods to fake client
				for i := range pods {
					Expect(fakeClient.Create(ctx, &pods[i])).To(Succeed())
				}

				result, err := detector.getServerPods(ctx, cluster)

				Expect(err).NotTo(HaveOccurred())
				Expect(len(result)).To(Equal(2))
				Expect(result[0].Name).To(Equal("test-cluster-server-0"))
				Expect(result[1].Name).To(Equal("test-cluster-server-1"))
			})
		})

		Context("with no matching pods", func() {
			It("should return empty list", func() {
				result, err := detector.getServerPods(ctx, cluster)

				Expect(err).NotTo(HaveOccurred())
				Expect(len(result)).To(Equal(0))
			})
		})
	})

	Describe("groupPodsByClusterView", func() {
		It("should group pods with similar cluster views", func() {
			views := []ClusterView{
				{
					ServerPodName: "pod-0",
					Servers: []neo4jclient.ServerInfo{
						{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
						{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
					},
				},
				{
					ServerPodName: "pod-1",
					Servers: []neo4jclient.ServerInfo{
						{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
						{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
					},
				},
				{
					ServerPodName: "pod-2",
					Servers: []neo4jclient.ServerInfo{
						{Address: "pod-2:7687", State: "Enabled", Health: "Available"},
					},
				},
			}

			groups := detector.groupPodsByClusterView(views)

			Expect(len(groups)).To(Equal(2))
			// First group should have pods 0 and 1 (similar views)
			Expect(len(groups[0])).To(Equal(2))
			// Second group should have pod 2 (different view)
			Expect(len(groups[1])).To(Equal(1))
		})
	})

	Describe("haveSimilarClusterView", func() {
		It("should detect similar views", func() {
			view1 := ClusterView{
				Servers: []neo4jclient.ServerInfo{
					{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
					{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
				},
			}
			view2 := ClusterView{
				Servers: []neo4jclient.ServerInfo{
					{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
					{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
				},
			}

			similar := detector.haveSimilarClusterView(view1, view2)
			Expect(similar).To(BeTrue())
		})

		It("should detect different views", func() {
			view1 := ClusterView{
				Servers: []neo4jclient.ServerInfo{
					{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
					{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
				},
			}
			view2 := ClusterView{
				Servers: []neo4jclient.ServerInfo{
					{Address: "pod-2:7687", State: "Enabled", Health: "Available"},
				},
			}

			similar := detector.haveSimilarClusterView(view1, view2)
			Expect(similar).To(BeFalse())
		})
	})

	Describe("countUniqueServersInGroup", func() {
		It("should count unique available servers", func() {
			group := []ClusterView{
				{
					Servers: []neo4jclient.ServerInfo{
						{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
						{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
					},
				},
				{
					Servers: []neo4jclient.ServerInfo{
						{Address: "pod-0:7687", State: "Enabled", Health: "Available"},
						{Address: "pod-1:7687", State: "Enabled", Health: "Available"},
						{Address: "pod-2:7687", State: "Disabled", Health: "Unavailable"},
					},
				},
			}

			count := detector.countUniqueServersInGroup(group)
			Expect(count).To(Equal(2)) // Only enabled and available servers
		})
	})

	Describe("repairByRestartingPods", func() {
		It("should delete specified pods", func() {
			// Create test pods
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "orphaned-pod-1",
						Namespace: testNamespace,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "orphaned-pod-2",
						Namespace: testNamespace,
					},
				},
			}

			for i := range pods {
				Expect(fakeClient.Create(ctx, &pods[i])).To(Succeed())
			}

			err := detector.repairByRestartingPods(ctx, cluster, []string{"orphaned-pod-1", "orphaned-pod-2"})

			Expect(err).NotTo(HaveOccurred())

			// Verify pods were deleted
			var pod corev1.Pod
			err1 := fakeClient.Get(ctx, client.ObjectKey{Name: "orphaned-pod-1", Namespace: testNamespace}, &pod)
			err2 := fakeClient.Get(ctx, client.ObjectKey{Name: "orphaned-pod-2", Namespace: testNamespace}, &pod)

			// Pods should not exist after deletion
			Expect(err1).To(HaveOccurred())
			Expect(err2).To(HaveOccurred())
		})
	})
})

// Note: SplitBrainDetector tests are part of the main controller test suite
// No separate TestSplitBrainDetector function needed as tests use Describe() blocks
