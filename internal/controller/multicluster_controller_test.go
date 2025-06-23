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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

var _ = Describe("MultiClusterController", func() {
	var (
		multiClusterController *MultiClusterController
		cluster                *neo4jv1alpha1.Neo4jEnterpriseCluster
		ctx                    context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create scheme and register types
		testScheme := runtime.NewScheme()
		Expect(scheme.AddToScheme(testScheme)).To(Succeed())
		Expect(neo4jv1alpha1.AddToScheme(testScheme)).To(Succeed())
		Expect(batchv1.AddToScheme(testScheme)).To(Succeed())
		Expect(networkingv1.AddToScheme(testScheme)).To(Succeed())

		// Create fake client
		fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()

		// Create controller
		multiClusterController = NewMultiClusterController(fakeClient, testScheme)

		// Create test cluster with comprehensive multi-cluster configuration
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Primaries:   3,
					Secondaries: 2,
				},
				MultiCluster: &neo4jv1alpha1.MultiClusterSpec{
					Enabled: true,
					Topology: &neo4jv1alpha1.MultiClusterTopology{
						PrimaryCluster: "cluster-east",
						Strategy:       "active-active",
						Clusters: []neo4jv1alpha1.ClusterConfig{
							{
								Name:     "cluster-east",
								Region:   "us-east-1",
								Endpoint: "https://cluster-east.example.com",
								NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
									Primaries:   3,
									Secondaries: 2,
								},
							},
							{
								Name:     "cluster-west",
								Region:   "us-west-1",
								Endpoint: "https://cluster-west.example.com",
								NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
									Primaries:   0,
									Secondaries: 3,
								},
							},
						},
					},
					Networking: &neo4jv1alpha1.MultiClusterNetworking{
						Type: "istio",
						NetworkPolicies: []neo4jv1alpha1.CrossClusterNetworkPolicy{
							{
								Name:                "test-policy",
								SourceClusters:      []string{"cluster-east"},
								DestinationClusters: []string{"cluster-west"},
								Ports: []neo4jv1alpha1.CrossClusterNetworkPolicyPort{
									{Port: 7687, Protocol: "TCP"},
								},
							},
						},
					},
					ServiceMesh: &neo4jv1alpha1.ServiceMeshConfig{
						Type: "istio",
						Istio: &neo4jv1alpha1.IstioConfig{
							MultiCluster: &neo4jv1alpha1.IstioMultiClusterConfig{
								Networks: map[string]neo4jv1alpha1.IstioNetworkConfig{
									"network1": {
										Endpoints: []neo4jv1alpha1.IstioNetworkEndpoint{
											{Service: "neo4j-service"},
										},
									},
								},
							},
							Gateways: []neo4jv1alpha1.IstioGatewayConfig{
								{
									Name: "neo4j-gateway",
									Servers: []neo4jv1alpha1.IstioServerConfig{
										{
											Port: neo4jv1alpha1.IstioPortConfig{
												Number:   7687,
												Name:     "bolt",
												Protocol: "TCP",
											},
											Hosts: []string{"neo4j.example.com"},
										},
									},
								},
							},
							VirtualServices: []neo4jv1alpha1.IstioVirtualServiceConfig{
								{
									Name:  "neo4j-vs",
									Hosts: []string{"neo4j.example.com"},
									HTTP: []neo4jv1alpha1.IstioHTTPRouteConfig{
										{
											Route: []neo4jv1alpha1.IstioHTTPRouteDestination{
												{
													Destination: neo4jv1alpha1.IstioDestination{
														Host: "neo4j-service",
														Port: &neo4jv1alpha1.IstioPortSelector{
															Number: 7687,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					Coordination: &neo4jv1alpha1.CrossClusterCoordination{
						LeaderElection: &neo4jv1alpha1.CrossClusterLeaderElection{
							Enabled:       true,
							LeaseDuration: "15s",
							RenewDeadline: "10s",
							RetryPeriod:   "2s",
						},
						StateSynchronization: &neo4jv1alpha1.StateSynchronizationConfig{
							Enabled:            true,
							Interval:           "30s",
							ConflictResolution: "last_writer_wins",
						},
						FailoverCoordination: &neo4jv1alpha1.FailoverCoordinationConfig{
							Enabled: true,
							Timeout: "300s",
							HealthCheck: &neo4jv1alpha1.CrossClusterHealthCheckConfig{
								Interval:         "10s",
								Timeout:          "5s",
								FailureThreshold: 3,
							},
						},
					},
				},
			},
		}
	})

	Context("When multi-cluster is enabled", func() {
		It("Should reconcile multi-cluster deployment successfully", func() {
			Expect(multiClusterController.client.Create(ctx, cluster)).Should(Succeed())

			err := multiClusterController.ReconcileMultiCluster(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should skip when multi-cluster is disabled", func() {
			cluster.Spec.MultiCluster.Enabled = false
			Expect(multiClusterController.client.Create(ctx, cluster)).Should(Succeed())

			err := multiClusterController.ReconcileMultiCluster(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should initialize cluster connections", func() {
			err := multiClusterController.initializeClusterConnections(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Verify cluster clients are initialized
			multiClusterController.clusterClientsMux.RLock()
			defer multiClusterController.clusterClientsMux.RUnlock()
			Expect(multiClusterController.clusterClients).To(HaveLen(2))
		})

		It("Should create cluster client for remote cluster", func() {
			clusterConfig := neo4jv1alpha1.ClusterConfig{
				Name:     "test-remote",
				Endpoint: "remote.example.com",
			}

			client, err := multiClusterController.createClusterClient(ctx, clusterConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(client).NotTo(BeNil())
		})
	})

	Context("StatefulSet creation", func() {
		It("Should deploy across multiple clusters", func() {
			// Setup multi-cluster configuration
			cluster.Spec.MultiCluster = &neo4jv1alpha1.MultiClusterSpec{
				Enabled: true,
				Topology: &neo4jv1alpha1.MultiClusterTopology{
					PrimaryCluster: "cluster-east",
					Strategy:       "active-active",
					Clusters: []neo4jv1alpha1.ClusterConfig{
						{
							Name:   "cluster-east",
							Region: "us-east-1",
							NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
								Primaries:   3,
								Secondaries: 2,
							},
						},
						{
							Name:   "cluster-west",
							Region: "us-west-2",
							NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
								Primaries:   0,
								Secondaries: 3,
							},
						},
					},
				},
			}

			// Initialize cluster connections
			err := multiClusterController.initializeClusterConnections(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Deploy across clusters
			err = multiClusterController.deployAcrossClusters(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should handle missing topology configuration", func() {
			cluster.Spec.MultiCluster = &neo4jv1alpha1.MultiClusterSpec{
				Enabled:  true,
				Topology: nil,
			}

			err := multiClusterController.deployAcrossClusters(ctx, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("multi-cluster topology configuration is required"))
		})

		It("Should deploy primary instances correctly", func() {
			clusterConfig := neo4jv1alpha1.ClusterConfig{
				Name:   "test-cluster",
				Region: "us-east-1",
				NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
					Primaries: 3,
				},
			}

			// Mock cluster client
			multiClusterController.clusterClients["test-cluster"] = k8sClient

			err := multiClusterController.deployPrimaryInstances(ctx, cluster, clusterConfig)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deploy secondary instances correctly", func() {
			clusterConfig := neo4jv1alpha1.ClusterConfig{
				Name:   "test-cluster",
				Region: "us-west-2",
				NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
					Secondaries: 2,
				},
			}

			// Mock cluster client
			multiClusterController.clusterClients["test-cluster"] = k8sClient

			err := multiClusterController.deploySecondaryInstances(ctx, cluster, clusterConfig)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should handle missing cluster client", func() {
			clusterConfig := neo4jv1alpha1.ClusterConfig{
				Name:   "missing-cluster",
				Region: "us-east-1",
				NodeAllocation: &neo4jv1alpha1.NodeAllocationConfig{
					Primaries: 1,
				},
			}

			err := multiClusterController.deployPrimaryInstances(ctx, cluster, clusterConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("client for cluster missing-cluster not found"))
		})
	})

	Context("Service creation", func() {
		It("Should setup cross-cluster replication", func() {
			cluster.Spec.MultiCluster = &neo4jv1alpha1.MultiClusterSpec{
				Enabled: true,
				Topology: &neo4jv1alpha1.MultiClusterTopology{
					PrimaryCluster: "cluster-east",
					Clusters: []neo4jv1alpha1.ClusterConfig{
						{
							Name:   "cluster-east",
							Region: "us-east-1",
						},
						{
							Name:   "cluster-west",
							Region: "us-west-2",
						},
					},
				},
			}

			err := multiClusterController.setupCrossClusterReplication(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())

			// Verify replication config was created
			configMap := &corev1.ConfigMap{}
			err = multiClusterController.client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-replication-config", cluster.Name),
				Namespace: cluster.Namespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Data["replication.conf"]).To(ContainSubstring("cluster.name=" + cluster.Name))
		})

		It("Should generate correct replication config", func() {
			cluster.Spec.MultiCluster = &neo4jv1alpha1.MultiClusterSpec{
				Topology: &neo4jv1alpha1.MultiClusterTopology{
					PrimaryCluster: "primary-cluster",
					Clusters: []neo4jv1alpha1.ClusterConfig{
						{
							Name:   "primary-cluster",
							Region: "us-east-1",
						},
						{
							Name:   "secondary-cluster",
							Region: "us-west-2",
						},
					},
				},
			}

			config := multiClusterController.generateReplicationConfig(cluster)
			Expect(config).To(ContainSubstring("cluster.name=" + cluster.Name))
			Expect(config).To(ContainSubstring("primary.cluster=primary-cluster"))
			Expect(config).To(ContainSubstring("cluster.0.name=primary-cluster"))
			Expect(config).To(ContainSubstring("cluster.1.name=secondary-cluster"))
		})
	})

	Context("NetworkingManager", func() {
		var networkingManager *NetworkingManager

		BeforeEach(func() {
			networkingManager = NewNetworkingManager(multiClusterController.client, log.Log.WithName("test"))
		})

		It("Should setup Cilium networking", func() {
			err := networkingManager.SetupCiliumNetworking(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should setup Istio networking", func() {
			cluster.Spec.MultiCluster.Networking.Type = "istio"
			cluster.Spec.MultiCluster.Networking.Cilium = nil

			err := networkingManager.SetupIstioNetworking(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should build Cilium cluster mesh config", func() {
			config := networkingManager.buildCiliumClusterMeshConfig(cluster)
			Expect(config).NotTo(BeEmpty())
			Expect(config["enabled"]).To(BeNil())
			Expect(config["cluster-id"]).To(Equal(1))
			Expect(config["cluster-name"]).To(Equal(cluster.Name))
		})

		It("Should build network policy correctly", func() {
			policySpec := neo4jv1alpha1.CrossClusterNetworkPolicy{
				Name:                "test-policy",
				SourceClusters:      []string{"cluster-east"},
				DestinationClusters: []string{"cluster-west"},
				Ports: []neo4jv1alpha1.CrossClusterNetworkPolicyPort{
					{Port: 7687, Protocol: "TCP"},
				},
			}

			networkPolicy := networkingManager.buildNetworkPolicy(cluster, policySpec)
			Expect(networkPolicy).NotTo(BeNil())
			Expect(networkPolicy.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, policySpec.Name)))
		})
	})

	Context("CoordinationManager", func() {
		var coordinationManager *CoordinationManager

		BeforeEach(func() {
			coordinationManager = NewCoordinationManager(multiClusterController.client, log.Log.WithName("test"))
			cluster.Spec.MultiCluster.Coordination = &neo4jv1alpha1.CrossClusterCoordination{
				LeaderElection: &neo4jv1alpha1.CrossClusterLeaderElection{
					Enabled:       true,
					LeaseDuration: "15s",
					RenewDeadline: "10s",
					RetryPeriod:   "2s",
				},
			}
		})

		It("Should setup leader election", func() {
			err := coordinationManager.SetupLeaderElection(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should setup state synchronization", func() {
			cluster.Spec.MultiCluster.Coordination.StateSynchronization = &neo4jv1alpha1.StateSynchronizationConfig{
				Enabled:            true,
				Interval:           "30s",
				ConflictResolution: "last_writer_wins",
			}

			err := coordinationManager.SetupStateSynchronization(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should setup failover coordination", func() {
			cluster.Spec.MultiCluster.Coordination.FailoverCoordination = &neo4jv1alpha1.FailoverCoordinationConfig{
				Enabled: true,
				Timeout: "5m",
			}

			err := coordinationManager.SetupFailoverCoordination(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Service Mesh Integration", func() {
		BeforeEach(func() {
			cluster.Spec.MultiCluster.ServiceMesh = &neo4jv1alpha1.ServiceMeshConfig{
				Type: "istio",
				Istio: &neo4jv1alpha1.IstioConfig{
					MultiCluster: &neo4jv1alpha1.IstioMultiClusterConfig{
						Networks: map[string]neo4jv1alpha1.IstioNetworkConfig{
							"network1": {
								Endpoints: []neo4jv1alpha1.IstioNetworkEndpoint{
									{Service: "neo4j"},
								},
								Gateways: []string{"gateway1"},
							},
						},
					},
				},
			}
		})

		It("Should build Istio gateway configuration", func() {
			gatewayConfig := neo4jv1alpha1.IstioGatewayConfig{
				Name: "neo4j-gateway",
				Servers: []neo4jv1alpha1.IstioServerConfig{
					{
						Port: neo4jv1alpha1.IstioPortConfig{
							Number:   7687,
							Name:     "bolt",
							Protocol: "TCP",
						},
						Hosts: []string{"neo4j.example.com"},
					},
				},
			}

			gateway := multiClusterController.networkingManager.buildIstioGateway(cluster, gatewayConfig)
			Expect(gateway).NotTo(BeEmpty())
			Expect(gateway["metadata"].(map[string]interface{})["name"]).To(Equal("neo4j-gateway"))
		})

		It("Should build Istio virtual service configuration", func() {
			vsConfig := neo4jv1alpha1.IstioVirtualServiceConfig{
				Name:     "neo4j-vs",
				Hosts:    []string{"neo4j.example.com"},
				Gateways: []string{"neo4j-gateway"},
			}

			virtualService := multiClusterController.networkingManager.buildIstioVirtualService(cluster, vsConfig)
			Expect(virtualService).NotTo(BeEmpty())
			Expect(virtualService["metadata"].(map[string]interface{})["name"]).To(Equal("neo4j-vs"))
		})
	})
})
