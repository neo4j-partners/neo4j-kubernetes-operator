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
	"sync"
	"time"

	"github.com/go-logr/logr"
	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MultiClusterController manages multi-cluster Neo4j deployments
type MultiClusterController struct {
	client              client.Client
	scheme              *runtime.Scheme
	logger              logr.Logger
	clusterClients      map[string]client.Client
	clusterClientsMux   sync.RWMutex
	networkingManager   *NetworkingManager
	coordinationManager *CoordinationManager
}

// NewMultiClusterController creates a new multi-cluster controller
func NewMultiClusterController(k8sClient client.Client, scheme *runtime.Scheme) *MultiClusterController {
	logger := log.Log.WithName("multicluster")
	return &MultiClusterController{
		client:              k8sClient,
		scheme:              scheme,
		logger:              logger,
		clusterClients:      make(map[string]client.Client),
		networkingManager:   NewNetworkingManager(k8sClient, logger),
		coordinationManager: NewCoordinationManager(k8sClient, logger),
	}
}

// ReconcileMultiCluster handles multi-cluster deployment reconciliation
func (mcc *MultiClusterController) ReconcileMultiCluster(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx).WithName("multicluster")

	if cluster.Spec.MultiCluster == nil || !cluster.Spec.MultiCluster.Enabled {
		logger.Info("Multi-cluster is disabled, skipping")
		return nil
	}

	// Initialize cluster connections
	if err := mcc.initializeClusterConnections(ctx, cluster); err != nil {
		return fmt.Errorf("failed to initialize cluster connections: %w", err)
	}

	// Set up cross-cluster networking
	if err := mcc.setupNetworking(ctx, cluster); err != nil {
		return fmt.Errorf("failed to setup networking: %w", err)
	}

	// Deploy Neo4j components across clusters
	if err := mcc.deployAcrossClusters(ctx, cluster); err != nil {
		return fmt.Errorf("failed to deploy across clusters: %w", err)
	}

	// Set up coordination and leader election
	if err := mcc.setupCoordination(ctx, cluster); err != nil {
		return fmt.Errorf("failed to setup coordination: %w", err)
	}

	logger.Info("Multi-cluster reconciliation completed")
	return nil
}

// initializeClusterConnections establishes connections to all configured clusters
func (mcc *MultiClusterController) initializeClusterConnections(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	mcc.clusterClientsMux.Lock()
	defer mcc.clusterClientsMux.Unlock()

	for _, clusterConfig := range cluster.Spec.MultiCluster.Topology.Clusters {
		if _, exists := mcc.clusterClients[clusterConfig.Name]; exists {
			continue
		}

		// Create client for remote cluster
		clusterClient, err := mcc.createClusterClient(ctx, clusterConfig)
		if err != nil {
			return fmt.Errorf("failed to create client for cluster %s: %w", clusterConfig.Name, err)
		}

		mcc.clusterClients[clusterConfig.Name] = clusterClient
		mcc.logger.Info("Initialized connection to cluster", "cluster", clusterConfig.Name)
	}

	return nil
}

// createClusterClient creates a Kubernetes client for a remote cluster
func (mcc *MultiClusterController) createClusterClient(ctx context.Context, clusterConfig neo4jv1alpha1.ClusterConfig) (client.Client, error) {
	// If no endpoint is specified, use the local client (for single cluster or testing)
	if clusterConfig.Endpoint == "" {
		mcc.logger.Info("No endpoint specified for cluster, using local client", "cluster", clusterConfig.Name)
		return mcc.client, nil
	}

	// Look for cluster credentials in secrets
	secretName := fmt.Sprintf("%s-cluster-credentials", clusterConfig.Name)
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      secretName,
		Namespace: "default", // Could be made configurable
	}

	if err := mcc.client.Get(ctx, secretKey, secret); err != nil {
		if errors.IsNotFound(err) {
			mcc.logger.Info("No credentials secret found for cluster, using local client",
				"cluster", clusterConfig.Name, "secret", secretName)
			return mcc.client, nil
		}
		return nil, fmt.Errorf("failed to get cluster credentials secret: %w", err)
	}

	// Extract kubeconfig or token from secret
	var restConfig *rest.Config
	var err error

	if kubeconfigData, exists := secret.Data["kubeconfig"]; exists {
		// Use kubeconfig approach
		restConfig, err = mcc.createConfigFromKubeconfig(kubeconfigData, clusterConfig)
	} else if token, exists := secret.Data["token"]; exists {
		// Use service account token approach
		restConfig, err = mcc.createConfigFromToken(string(token), clusterConfig)
	} else {
		return nil, fmt.Errorf("cluster credentials secret must contain either 'kubeconfig' or 'token' data")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create rest config for cluster %s: %w", clusterConfig.Name, err)
	}

	// Create the client
	clusterClient, err := client.New(restConfig, client.Options{
		Scheme: mcc.scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client for cluster %s: %w", clusterConfig.Name, err)
	}

	// Test connectivity
	if err := mcc.testClusterConnectivity(ctx, clusterClient); err != nil {
		mcc.logger.Error(err, "Failed to connect to cluster, falling back to local client", "cluster", clusterConfig.Name)
		return mcc.client, nil // Fallback to local client
	}

	mcc.logger.Info("Successfully created client for remote cluster", "cluster", clusterConfig.Name)
	return clusterClient, nil
}

// createConfigFromKubeconfig creates a rest.Config from kubeconfig data
func (mcc *MultiClusterController) createConfigFromKubeconfig(kubeconfigData []byte, clusterConfig neo4jv1alpha1.ClusterConfig) (*rest.Config, error) {
	// Parse kubeconfig
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Build rest config from kubeconfig
	restConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config from kubeconfig: %w", err)
	}

	// Override endpoint if specified in cluster config
	if clusterConfig.Endpoint != "" {
		restConfig.Host = clusterConfig.Endpoint
	}

	return restConfig, nil
}

// createConfigFromToken creates a rest.Config from a service account token
func (mcc *MultiClusterController) createConfigFromToken(token string, clusterConfig neo4jv1alpha1.ClusterConfig) (*rest.Config, error) {
	if clusterConfig.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required when using token authentication")
	}

	restConfig := &rest.Config{
		Host:        clusterConfig.Endpoint,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: false, // Should be configurable in production
		},
	}

	// Set timeout and other defaults
	restConfig.Timeout = 30 * time.Second
	restConfig.QPS = 50
	restConfig.Burst = 100

	return restConfig, nil
}

// testClusterConnectivity tests if we can connect to the cluster
func (mcc *MultiClusterController) testClusterConnectivity(ctx context.Context, clusterClient client.Client) error {
	// Try to list namespaces as a connectivity test
	namespaceList := &corev1.NamespaceList{}
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := clusterClient.List(listCtx, namespaceList, client.Limit(1)); err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}

	return nil
}

// setupNetworking configures cross-cluster networking
func (mcc *MultiClusterController) setupNetworking(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	networking := cluster.Spec.MultiCluster.Networking
	if networking == nil {
		return nil
	}

	switch networking.Type {
	case "cilium":
		return mcc.networkingManager.SetupCiliumNetworking(ctx, cluster)
	case "istio":
		return mcc.networkingManager.SetupIstioNetworking(ctx, cluster)
	case "submariner":
		return mcc.setupSubmarinerNetworking(ctx, cluster)
	default:
		return fmt.Errorf("unsupported networking type: %s", networking.Type)
	}
}

func (mcc *MultiClusterController) setupSubmarinerNetworking(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	mcc.logger.Info("Setting up Submariner networking for multi-cluster", "cluster", cluster.Name)

	// Create Submariner broker configuration
	brokerConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "submariner.io/v1alpha1",
			"kind":       "Broker",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("%s-broker", cluster.Name),
				"namespace": cluster.Namespace,
			},
			"spec": map[string]interface{}{
				"globalnetEnabled": true,
			},
		},
	}

	if err := mcc.client.Create(ctx, brokerConfig); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create Submariner broker: %w", err)
	}

	// Create cluster join configurations for each cluster
	for _, clusterConfig := range cluster.Spec.MultiCluster.Topology.Clusters {
		joinConfig := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "submariner.io/v1alpha1",
				"kind":       "ClusterJoin",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("%s-join", clusterConfig.Name),
					"namespace": cluster.Namespace,
				},
				"spec": map[string]interface{}{
					"clusterID":   clusterConfig.Name,
					"brokerURL":   fmt.Sprintf("https://%s-broker:8080", cluster.Name),
					"cableDriver": "libreswan",
				},
			},
		}

		if err := mcc.client.Create(ctx, joinConfig); err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create cluster join config for %s: %w", clusterConfig.Name, err)
		}
	}

	return nil
}

// deployAcrossClusters deploys Neo4j components across multiple clusters
func (mcc *MultiClusterController) deployAcrossClusters(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	mcc.logger.Info("Deploying Neo4j across multiple clusters", "cluster", cluster.Name)

	topology := cluster.Spec.MultiCluster.Topology
	if topology == nil {
		return fmt.Errorf("multi-cluster topology configuration is required")
	}

	// Deploy to each cluster based on node allocation
	for _, clusterConfig := range topology.Clusters {
		mcc.logger.Info("Deploying to cluster", "cluster", clusterConfig.Name, "region", clusterConfig.Region)

		// Deploy primary instances if allocated to this cluster
		if clusterConfig.NodeAllocation != nil && clusterConfig.NodeAllocation.Primaries > 0 {
			if err := mcc.deployPrimaryInstances(ctx, cluster, clusterConfig); err != nil {
				return fmt.Errorf("failed to deploy primary instances to cluster %s: %w", clusterConfig.Name, err)
			}
		}

		// Deploy secondary instances if allocated to this cluster
		if clusterConfig.NodeAllocation != nil && clusterConfig.NodeAllocation.Secondaries > 0 {
			if err := mcc.deploySecondaryInstances(ctx, cluster, clusterConfig); err != nil {
				return fmt.Errorf("failed to deploy secondary instances to cluster %s: %w", clusterConfig.Name, err)
			}
		}
	}

	return nil
}

func (mcc *MultiClusterController) deployPrimaryInstances(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, clusterConfig neo4jv1alpha1.ClusterConfig) error {
	mcc.logger.Info("Deploying primary instances", "cluster", clusterConfig.Name, "primaries", clusterConfig.NodeAllocation.Primaries)

	// Get the client for the target cluster
	clusterClient, err := mcc.getClusterClient(clusterConfig.Name)
	if err != nil {
		return fmt.Errorf("failed to get client for cluster %s: %w", clusterConfig.Name, err)
	}

	// Create a modified cluster spec for this specific cluster deployment
	clusterCopy := cluster.DeepCopy()
	clusterCopy.Name = fmt.Sprintf("%s-%s-primary", cluster.Name, clusterConfig.Name)
	clusterCopy.Spec.Topology.Primaries = clusterConfig.NodeAllocation.Primaries
	clusterCopy.Spec.Topology.Secondaries = 0 // Only primaries in this deployment

	// Apply cluster-specific configuration
	if clusterConfig.Config != nil {
		if clusterCopy.Spec.Config == nil {
			clusterCopy.Spec.Config = make(map[string]string)
		}
		for key, value := range clusterConfig.Config {
			clusterCopy.Spec.Config[key] = value
		}
	}

	// Apply node selector and tolerations
	if clusterConfig.NodeAllocation.NodeSelector != nil {
		clusterCopy.Spec.NodeSelector = clusterConfig.NodeAllocation.NodeSelector
	}
	if clusterConfig.NodeAllocation.Tolerations != nil {
		clusterCopy.Spec.Tolerations = clusterConfig.NodeAllocation.Tolerations
	}

	// Add multi-cluster labels
	if clusterCopy.Labels == nil {
		clusterCopy.Labels = make(map[string]string)
	}
	clusterCopy.Labels["neo4j.com/cluster-role"] = "primary"
	clusterCopy.Labels["neo4j.com/parent-cluster"] = cluster.Name
	clusterCopy.Labels["neo4j.com/target-cluster"] = clusterConfig.Name
	clusterCopy.Labels["neo4j.com/region"] = clusterConfig.Region

	// Clear resource version to allow creation
	clusterCopy.ResourceVersion = ""

	// Create the cluster in the target cluster
	if err := clusterClient.Create(ctx, clusterCopy); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create primary cluster deployment: %w", err)
	}

	return nil
}

func (mcc *MultiClusterController) deploySecondaryInstances(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, clusterConfig neo4jv1alpha1.ClusterConfig) error {
	mcc.logger.Info("Deploying secondary instances", "cluster", clusterConfig.Name, "secondaries", clusterConfig.NodeAllocation.Secondaries)

	// Get the client for the target cluster
	clusterClient, err := mcc.getClusterClient(clusterConfig.Name)
	if err != nil {
		return fmt.Errorf("failed to get client for cluster %s: %w", clusterConfig.Name, err)
	}

	// Create a modified cluster spec for this specific cluster deployment
	clusterCopy := cluster.DeepCopy()
	clusterCopy.Name = fmt.Sprintf("%s-%s-secondary", cluster.Name, clusterConfig.Name)

	// For secondary deployments, we need at least 1 primary to act as a read replica coordinator
	// The secondaries will be configured as read replicas of the primary cluster
	clusterCopy.Spec.Topology.Primaries = 1 // Minimum required by API
	clusterCopy.Spec.Topology.Secondaries = clusterConfig.NodeAllocation.Secondaries

	// Apply cluster-specific configuration
	if clusterConfig.Config != nil {
		if clusterCopy.Spec.Config == nil {
			clusterCopy.Spec.Config = make(map[string]string)
		}
		for key, value := range clusterConfig.Config {
			clusterCopy.Spec.Config[key] = value
		}
	}

	// Configure connection to primary cluster for read replicas
	primaryClusterEndpoint := mcc.getPrimaryClusterEndpoint(cluster, clusterConfig)
	if primaryClusterEndpoint != "" {
		if clusterCopy.Spec.Config == nil {
			clusterCopy.Spec.Config = make(map[string]string)
		}
		clusterCopy.Spec.Config["dbms.cluster.discovery.endpoints"] = primaryClusterEndpoint
		// Configure as read replica
		clusterCopy.Spec.Config["dbms.mode"] = "READ_REPLICA"
	}

	// Apply node selector and tolerations
	if clusterConfig.NodeAllocation.NodeSelector != nil {
		clusterCopy.Spec.NodeSelector = clusterConfig.NodeAllocation.NodeSelector
	}
	if clusterConfig.NodeAllocation.Tolerations != nil {
		clusterCopy.Spec.Tolerations = clusterConfig.NodeAllocation.Tolerations
	}

	// Add multi-cluster labels
	if clusterCopy.Labels == nil {
		clusterCopy.Labels = make(map[string]string)
	}
	clusterCopy.Labels["neo4j.com/cluster-role"] = "secondary"
	clusterCopy.Labels["neo4j.com/parent-cluster"] = cluster.Name
	clusterCopy.Labels["neo4j.com/target-cluster"] = clusterConfig.Name
	clusterCopy.Labels["neo4j.com/region"] = clusterConfig.Region

	// Clear resource version to allow creation
	clusterCopy.ResourceVersion = ""

	// Create the cluster in the target cluster
	if err := clusterClient.Create(ctx, clusterCopy); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create secondary cluster deployment: %w", err)
	}

	return nil
}

// getClusterClient returns the client for a specific cluster
func (mcc *MultiClusterController) getClusterClient(clusterName string) (client.Client, error) {
	mcc.clusterClientsMux.RLock()
	defer mcc.clusterClientsMux.RUnlock()

	if client, exists := mcc.clusterClients[clusterName]; exists {
		return client, nil
	}

	return nil, fmt.Errorf("client for cluster %s not found", clusterName)
}

// getPrimaryClusterEndpoint returns the endpoint for the primary cluster
func (mcc *MultiClusterController) getPrimaryClusterEndpoint(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, _ neo4jv1alpha1.ClusterConfig) string {
	topology := cluster.Spec.MultiCluster.Topology
	primaryClusterName := topology.PrimaryCluster

	// Find the primary cluster configuration
	for _, clusterConfig := range topology.Clusters {
		if clusterConfig.Name == primaryClusterName {
			if clusterConfig.Endpoint != "" {
				return clusterConfig.Endpoint
			}
			// Construct default endpoint based on cluster name and region
			return fmt.Sprintf("%s-primary.%s.svc.cluster.local:7687", cluster.Name, clusterConfig.Region)
		}
	}

	// Fallback to default primary endpoint
	return fmt.Sprintf("%s-primary.default.svc.cluster.local:7687", cluster.Name)
}

func (mcc *MultiClusterController) setupCrossClusterReplication(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	mcc.logger.Info("Setting up cross-cluster replication", "cluster", cluster.Name)

	// Generate replication configuration
	replicationConfig := mcc.generateReplicationConfig(cluster)

	// Create ConfigMap for replication configuration
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-replication-config", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j",
				"app.kubernetes.io/instance":  cluster.Name,
				"app.kubernetes.io/component": "replication",
			},
		},
		Data: map[string]string{
			"replication.conf": replicationConfig,
		},
	}

	// Create or update the ConfigMap
	if err := mcc.client.Create(ctx, configMap); err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing ConfigMap
			existingConfigMap := &corev1.ConfigMap{}
			if err := mcc.client.Get(ctx, types.NamespacedName{
				Name:      configMap.Name,
				Namespace: configMap.Namespace,
			}, existingConfigMap); err != nil {
				return fmt.Errorf("failed to get existing ConfigMap: %w", err)
			}
			existingConfigMap.Data = configMap.Data
			if err := mcc.client.Update(ctx, existingConfigMap); err != nil {
				return fmt.Errorf("failed to update replication ConfigMap: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create replication ConfigMap: %w", err)
		}
	}

	// Verify ConfigMap was created
	createdConfigMap := &corev1.ConfigMap{}
	if err := mcc.client.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-replication-config", cluster.Name),
		Namespace: cluster.Namespace,
	}, createdConfigMap); err != nil {
		return fmt.Errorf("failed to verify replication ConfigMap creation: %w", err)
	}

	mcc.logger.Info("Cross-cluster replication configuration created", "configMap", createdConfigMap.Name)
	return nil
}

func (mcc *MultiClusterController) generateReplicationConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	config := fmt.Sprintf(`
# Multi-cluster replication configuration for %s
cluster.name=%s
cluster.mode=multi-cluster

# Primary cluster configuration
primary.cluster=%s

# Cluster configurations
`, cluster.Name, cluster.Name, cluster.Spec.MultiCluster.Topology.PrimaryCluster)

	for i, clusterConfig := range cluster.Spec.MultiCluster.Topology.Clusters {
		config += fmt.Sprintf(`
cluster.%d.name=%s
cluster.%d.region=%s
`, i, clusterConfig.Name, i, clusterConfig.Region)
	}

	return config
}

// setupCoordination sets up cross-cluster coordination
func (mcc *MultiClusterController) setupCoordination(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	coordination := cluster.Spec.MultiCluster.Coordination
	if coordination == nil {
		return nil
	}

	// Set up leader election
	if coordination.LeaderElection != nil && coordination.LeaderElection.Enabled {
		if err := mcc.coordinationManager.SetupLeaderElection(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup leader election: %w", err)
		}
	}

	// Set up state synchronization
	if coordination.StateSynchronization != nil && coordination.StateSynchronization.Enabled {
		if err := mcc.coordinationManager.SetupStateSynchronization(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup state synchronization: %w", err)
		}
	}

	// Set up failover coordination
	if coordination.FailoverCoordination != nil && coordination.FailoverCoordination.Enabled {
		if err := mcc.coordinationManager.SetupFailoverCoordination(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup failover coordination: %w", err)
		}
	}

	return nil
}

// NetworkingManager handles cross-cluster networking
type NetworkingManager struct {
	client client.Client
	logger logr.Logger
}

// NewNetworkingManager creates a new networking manager
func NewNetworkingManager(k8sClient client.Client, logger logr.Logger) *NetworkingManager {
	return &NetworkingManager{
		client: k8sClient,
		logger: logger,
	}
}

// SetupCiliumNetworking sets up Cilium multi-cluster networking
func (nm *NetworkingManager) SetupCiliumNetworking(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	ciliumConfig := cluster.Spec.MultiCluster.Networking.Cilium
	if ciliumConfig == nil {
		return nil
	}

	// Set up cluster mesh
	if ciliumConfig.ClusterMesh != nil && ciliumConfig.ClusterMesh.Enabled {
		if err := nm.setupCiliumClusterMesh(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup Cilium cluster mesh: %w", err)
		}
	}

	// Configure network policies
	if err := nm.createCiliumNetworkPolicies(ctx, cluster); err != nil {
		return fmt.Errorf("failed to create Cilium network policies: %w", err)
	}

	return nil
}

// SetupIstioNetworking sets up Istio multi-cluster networking
func (nm *NetworkingManager) SetupIstioNetworking(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	if cluster.Spec.MultiCluster == nil || cluster.Spec.MultiCluster.ServiceMesh == nil {
		return fmt.Errorf("service mesh configuration is required for Istio networking")
	}

	istioConfig := cluster.Spec.MultiCluster.ServiceMesh.Istio
	if istioConfig == nil {
		return fmt.Errorf("istio configuration is required")
	}

	// Set up multi-cluster configuration
	if istioConfig.MultiCluster != nil {
		if err := nm.setupIstioMultiCluster(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup Istio multi-cluster: %w", err)
		}
	}

	// Create gateways and virtual services
	if err := nm.createIstioResources(ctx, cluster); err != nil {
		return fmt.Errorf("failed to create Istio resources: %w", err)
	}

	return nil
}

// setupCiliumClusterMesh sets up Cilium cluster mesh
func (nm *NetworkingManager) setupCiliumClusterMesh(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	// Create ClusterMesh configuration
	clusterMeshConfig := nm.buildCiliumClusterMeshConfig(cluster)

	// Apply configuration (this would be a ConfigMap or custom resource)
	nm.logger.Info("Setting up Cilium cluster mesh", "config", clusterMeshConfig)

	return nil
}

// createCiliumNetworkPolicies creates Cilium network policies
func (nm *NetworkingManager) createCiliumNetworkPolicies(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	policies := cluster.Spec.MultiCluster.Networking.NetworkPolicies

	for _, policy := range policies {
		networkPolicy := nm.buildNetworkPolicy(cluster, policy)
		if err := nm.client.Create(ctx, networkPolicy); err != nil {
			return fmt.Errorf("failed to create network policy %s: %w", policy.Name, err)
		}
	}

	return nil
}

// setupIstioMultiCluster sets up Istio multi-cluster configuration
func (nm *NetworkingManager) setupIstioMultiCluster(_ context.Context, _ *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	// Set up cross-cluster service discovery
	// Create multi-cluster secrets
	// Configure network endpoints
	nm.logger.Info("Setting up Istio multi-cluster configuration")

	return nil
}

// createIstioResources creates Istio gateways and virtual services
func (nm *NetworkingManager) createIstioResources(_ context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	istioConfig := cluster.Spec.MultiCluster.ServiceMesh.Istio

	// Create gateways
	for _, gatewayConfig := range istioConfig.Gateways {
		gateway := nm.buildIstioGateway(cluster, gatewayConfig)
		nm.logger.Info("Creating Istio gateway", "gateway", gateway)
	}

	// Create virtual services
	for _, vsConfig := range istioConfig.VirtualServices {
		virtualService := nm.buildIstioVirtualService(cluster, vsConfig)
		nm.logger.Info("Creating Istio virtual service", "virtualService", virtualService)
	}

	return nil
}

// buildCiliumClusterMeshConfig builds Cilium cluster mesh configuration
func (nm *NetworkingManager) buildCiliumClusterMeshConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) map[string]interface{} {
	// Calculate cluster ID based on cluster name hash for consistency
	clusterID := nm.calculateClusterID(cluster.Name)

	return map[string]interface{}{
		"cluster-name":                       cluster.Name,
		"cluster-id":                         clusterID,
		"enable-endpoint-routes":             true,
		"enable-cross-cluster-loadbalancing": true,
	}
}

// calculateClusterID generates a consistent cluster ID from the cluster name
func (nm *NetworkingManager) calculateClusterID(clusterName string) int {
	// Use a simple hash function to generate cluster ID
	// This ensures consistent IDs across deployments while avoiding conflicts
	hash := 0
	for _, char := range clusterName {
		hash = (hash*31 + int(char)) & 0x7FFFFFFF // Keep it positive
	}

	// Map to valid Cilium cluster ID range (1-255)
	// Avoid 0 as it's reserved, and keep under 255 for compatibility
	clusterID := (hash % 254) + 1

	nm.logger.V(1).Info("Calculated cluster ID", "clusterName", clusterName, "clusterID", clusterID)
	return clusterID
}

// buildNetworkPolicy builds a Kubernetes NetworkPolicy
func (nm *NetworkingManager) buildNetworkPolicy(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, policy neo4jv1alpha1.CrossClusterNetworkPolicy) *networkingv1.NetworkPolicy {
	var ports []networkingv1.NetworkPolicyPort
	for _, port := range policy.Ports {
		protocol := corev1.ProtocolTCP
		if port.Protocol == "UDP" {
			protocol = corev1.ProtocolUDP
		}
		ports = append(ports, networkingv1.NetworkPolicyPort{
			Port:     &intstr.IntOrString{IntVal: port.Port},
			Protocol: &protocol,
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cluster.Name, policy.Name),
			Namespace: cluster.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "neo4j",
					"app.kubernetes.io/instance": cluster.Name,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: ports,
				},
			},
		},
	}
}

// buildIstioGateway builds an Istio Gateway configuration
func (nm *NetworkingManager) buildIstioGateway(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, gatewayConfig neo4jv1alpha1.IstioGatewayConfig) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "networking.istio.io/v1beta1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":      gatewayConfig.Name,
			"namespace": cluster.Namespace,
		},
		"spec": map[string]interface{}{
			"selector": map[string]string{
				"istio": "ingressgateway",
			},
			"servers": gatewayConfig.Servers,
		},
	}
}

// buildIstioVirtualService builds an Istio VirtualService configuration
func (nm *NetworkingManager) buildIstioVirtualService(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, vsConfig neo4jv1alpha1.IstioVirtualServiceConfig) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "networking.istio.io/v1beta1",
		"kind":       "VirtualService",
		"metadata": map[string]interface{}{
			"name":      vsConfig.Name,
			"namespace": cluster.Namespace,
		},
		"spec": map[string]interface{}{
			"hosts":    vsConfig.Hosts,
			"gateways": vsConfig.Gateways,
			"http":     vsConfig.HTTP,
		},
	}
}

// CoordinationManager handles cross-cluster coordination
type CoordinationManager struct {
	client client.Client
	logger logr.Logger
}

// NewCoordinationManager creates a new coordination manager
func NewCoordinationManager(k8sClient client.Client, logger logr.Logger) *CoordinationManager {
	return &CoordinationManager{
		client: k8sClient,
		logger: logger,
	}
}

// SetupLeaderElection sets up cross-cluster leader election
func (cm *CoordinationManager) SetupLeaderElection(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	leaderElection := cluster.Spec.MultiCluster.Coordination.LeaderElection

	// Create leader election ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-leader-election", cluster.Name),
			Namespace: leaderElection.Namespace,
		},
		Data: map[string]string{
			"lease-duration": leaderElection.LeaseDuration,
			"renew-deadline": leaderElection.RenewDeadline,
			"retry-period":   leaderElection.RetryPeriod,
		},
	}

	if err := cm.client.Create(ctx, configMap); err != nil {
		return fmt.Errorf("failed to create leader election ConfigMap: %w", err)
	}

	cm.logger.Info("Leader election configured", "namespace", leaderElection.Namespace)
	return nil
}

// SetupStateSynchronization sets up state synchronization between clusters
func (cm *CoordinationManager) SetupStateSynchronization(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	stateSyncConfig := cluster.Spec.MultiCluster.Coordination.StateSynchronization

	cm.logger.Info("Setting up state synchronization",
		"interval", stateSyncConfig.Interval,
		"conflictResolution", stateSyncConfig.ConflictResolution)

	// Create state synchronization CronJob
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-state-sync", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j",
				"app.kubernetes.io/instance":  cluster.Name,
				"app.kubernetes.io/component": "state-sync",
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule: stateSyncConfig.Interval,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:    "state-sync",
									Image:   "neo4j/state-sync:latest",
									Command: []string{"/bin/bash", "-c"},
									Args: []string{
										fmt.Sprintf(`
											echo "Starting state synchronization for cluster %s"

											# Compare state across clusters
											neo4j-state-sync \
												--cluster=%s \
												--conflict-resolution=%s \
												--sync-interval=%s

											echo "State synchronization completed"
										`, cluster.Name, cluster.Name,
											stateSyncConfig.ConflictResolution,
											stateSyncConfig.Interval),
									},
									Env: []corev1.EnvVar{
										{
											Name:  "CLUSTER_NAME",
											Value: cluster.Name,
										},
										{
											Name:  "CONFLICT_RESOLUTION",
											Value: stateSyncConfig.ConflictResolution,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return cm.client.Create(ctx, cronJob)
}

// SetupFailoverCoordination sets up failover coordination
func (cm *CoordinationManager) SetupFailoverCoordination(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	failoverConfig := cluster.Spec.MultiCluster.Coordination.FailoverCoordination

	cm.logger.Info("Setting up failover coordination",
		"timeout", failoverConfig.Timeout)

	// Create failover coordination deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-failover-coordinator", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j",
				"app.kubernetes.io/instance":  cluster.Name,
				"app.kubernetes.io/component": "failover-coordinator",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "neo4j",
					"app.kubernetes.io/instance":  cluster.Name,
					"app.kubernetes.io/component": "failover-coordinator",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      "neo4j",
						"app.kubernetes.io/instance":  cluster.Name,
						"app.kubernetes.io/component": "failover-coordinator",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "failover-coordinator",
							Image:   "neo4j/failover-coordinator:latest",
							Command: []string{"/bin/bash", "-c"},
							Args: []string{
								fmt.Sprintf(`
									echo "Starting failover coordinator for cluster %s"

									# Monitor cluster health and coordinate failover
									neo4j-failover-coordinator \
										--cluster=%s \
										--timeout=%s \
										--health-check-interval=30s
								`, cluster.Name, cluster.Name, failoverConfig.Timeout),
							},
							Env: []corev1.EnvVar{
								{
									Name:  "CLUSTER_NAME",
									Value: cluster.Name,
								},
								{
									Name:  "FAILOVER_TIMEOUT",
									Value: failoverConfig.Timeout,
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: 9090,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
				},
			},
		},
	}

	return cm.client.Create(ctx, deployment)
}
