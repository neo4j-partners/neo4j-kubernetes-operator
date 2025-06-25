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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("MultiClusterController", func() {
	var (
		multiClusterController *controller.MultiClusterController
		cluster                *neo4jv1alpha1.Neo4jEnterpriseCluster
		ctx                    context.Context
		fakeClient             client.Client
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
		fakeClient = fake.NewClientBuilder().WithScheme(testScheme).Build()

		// Create controller
		multiClusterController = controller.NewMultiClusterController(fakeClient, testScheme)

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
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := multiClusterController.ReconcileMultiCluster(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should skip when multi-cluster is disabled", func() {
			cluster.Spec.MultiCluster.Enabled = false
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := multiClusterController.ReconcileMultiCluster(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("NetworkingManager", func() {
		var networkingManager *controller.NetworkingManager

		BeforeEach(func() {
			networkingManager = controller.NewNetworkingManager(fakeClient, log.Log.WithName("test"))
		})

		It("Should setup Cilium networking", func() {
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := networkingManager.SetupCiliumNetworking(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should setup Istio networking", func() {
			cluster.Spec.MultiCluster.Networking.Type = "istio"
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := networkingManager.SetupIstioNetworking(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("CoordinationManager", func() {
		var coordinationManager *controller.CoordinationManager

		BeforeEach(func() {
			coordinationManager = controller.NewCoordinationManager(fakeClient, log.Log.WithName("test"))
		})

		It("Should setup leader election", func() {
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := coordinationManager.SetupLeaderElection(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should setup state synchronization", func() {
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := coordinationManager.SetupStateSynchronization(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should setup failover coordination", func() {
			Expect(fakeClient.Create(ctx, cluster)).Should(Succeed())

			err := coordinationManager.SetupFailoverCoordination(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
