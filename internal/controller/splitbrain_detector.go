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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
)

// SplitBrainDetector handles detection and repair of Neo4j split-brain scenarios
type SplitBrainDetector struct {
	Client client.Client
}

// ClusterView represents what one server sees of the cluster
type ClusterView struct {
	ServerPodName   string                   // Which pod this view is from
	Servers         []neo4jclient.ServerInfo // What servers this pod sees
	ConnectionError error                    // Error if we couldn't connect to this pod
}

// SplitBrainAnalysis contains the results of split-brain detection
type SplitBrainAnalysis struct {
	IsSplitBrain    bool          // Whether split-brain was detected
	ClusterViews    []ClusterView // Views from each server
	ExpectedServers int           // How many servers should be in the cluster
	LargestCluster  ClusterView   // The cluster view with the most servers
	OrphanedPods    []string      // Pod names that are isolated or in minority clusters
	RepairAction    RepairAction  // What action should be taken
	ErrorMessage    string        // Human-readable error description
}

// RepairAction defines what action to take to repair split-brain
type RepairAction string

const (
	RepairActionNone        RepairAction = "none"         // No action needed
	RepairActionRestartPods RepairAction = "restart_pods" // Restart specific pods
	RepairActionRestartAll  RepairAction = "restart_all"  // Restart all pods (nuclear option)
	RepairActionWaitForming RepairAction = "wait_forming" // Wait for cluster to form naturally
	RepairActionInvestigate RepairAction = "investigate"  // Manual investigation required
)

// NewSplitBrainDetector creates a new split-brain detector
func NewSplitBrainDetector(client client.Client) *SplitBrainDetector {
	return &SplitBrainDetector{
		Client: client,
	}
}

// DetectSplitBrain analyzes the cluster for split-brain scenarios
func (d *SplitBrainDetector) DetectSplitBrain(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*SplitBrainAnalysis, error) {
	logger := log.FromContext(ctx)

	expectedServers := int(cluster.Spec.Topology.Servers)

	// Single server clusters cannot have split-brain
	if expectedServers == 1 {
		return &SplitBrainAnalysis{
			IsSplitBrain:    false,
			ExpectedServers: expectedServers,
			RepairAction:    RepairActionNone,
		}, nil
	}

	logger.Info("Starting split-brain detection",
		"cluster", cluster.Name,
		"expectedServers", expectedServers)

	// Get all server pods
	serverPods, err := d.getServerPods(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get server pods: %w", err)
	}

	// If we don't have all pods running, this might be normal startup
	runningPods := 0
	for _, pod := range serverPods {
		if pod.Status.Phase == corev1.PodRunning {
			runningPods++
		}
	}

	if runningPods < expectedServers {
		return &SplitBrainAnalysis{
			IsSplitBrain:    false,
			ExpectedServers: expectedServers,
			RepairAction:    RepairActionWaitForming,
			ErrorMessage:    fmt.Sprintf("Only %d of %d pods are running, waiting for cluster formation", runningPods, expectedServers),
		}, nil
	}

	// Query each running pod to get its view of the cluster
	clusterViews := make([]ClusterView, 0, len(serverPods))
	for _, pod := range serverPods {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		view := d.getClusterViewFromPod(ctx, cluster, pod.Name)
		clusterViews = append(clusterViews, view)

		logger.Info("Got cluster view from pod",
			"pod", pod.Name,
			"visibleServers", len(view.Servers),
			"connectionError", view.ConnectionError != nil)
	}

	// Analyze the cluster views to detect split-brain
	analysis := d.analyzeClusterViews(clusterViews, expectedServers)

	logger.Info("Split-brain analysis complete",
		"isSplitBrain", analysis.IsSplitBrain,
		"repairAction", analysis.RepairAction,
		"orphanedPods", len(analysis.OrphanedPods))

	return analysis, nil
}

// getServerPods returns all server pods for the cluster
func (d *SplitBrainDetector) getServerPods(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}

	// List pods with the server label selector
	err := d.Client.List(ctx, podList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		"neo4j.com/cluster":    cluster.Name,
		"neo4j.com/clustering": "true",
	})

	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// getClusterViewFromPod connects to a specific pod and gets its view of the cluster
func (d *SplitBrainDetector) getClusterViewFromPod(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, podName string) ClusterView {
	view := ClusterView{
		ServerPodName: podName,
		Servers:       []neo4jclient.ServerInfo{},
	}

	// Create a Neo4j client that connects specifically to this pod
	// We'll modify the client creation to target a specific pod
	neo4jClient, err := d.createPodSpecificNeo4jClient(ctx, cluster, podName)
	if err != nil {
		view.ConnectionError = fmt.Errorf("failed to connect to pod %s: %w", podName, err)
		return view
	}
	defer neo4jClient.Close()

	// Get the server list as seen by this pod
	servers, err := neo4jClient.GetServerList(ctx)
	if err != nil {
		view.ConnectionError = fmt.Errorf("failed to get server list from pod %s: %w", podName, err)
		return view
	}

	view.Servers = servers
	return view
}

// createPodSpecificNeo4jClient creates a client that connects to a specific pod
func (d *SplitBrainDetector) createPodSpecificNeo4jClient(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, podName string) (*neo4jclient.Client, error) {
	// Create a temporary connection URL that targets the specific pod
	// Format: bolt://pod-name.headless-service.namespace.svc.cluster.local:7687
	podURL := fmt.Sprintf("bolt://%s.%s-headless.%s.svc.cluster.local:7687",
		podName, cluster.Name, cluster.Namespace)

	// Create client with pod-specific URL and cluster credentials
	return neo4jclient.NewClientForPod(cluster, d.Client, cluster.Spec.Auth.AdminSecret, podURL)
}

// analyzeClusterViews analyzes all cluster views to determine if there's a split-brain
func (d *SplitBrainDetector) analyzeClusterViews(views []ClusterView, expectedServers int) *SplitBrainAnalysis {
	analysis := &SplitBrainAnalysis{
		ClusterViews:    views,
		ExpectedServers: expectedServers,
		IsSplitBrain:    false,
		OrphanedPods:    []string{},
		RepairAction:    RepairActionNone,
	}

	// Single server clusters cannot have split-brain
	if expectedServers == 1 {
		analysis.RepairAction = RepairActionNone
		return analysis
	}

	// Count connection failures
	connectionFailures := 0
	workingViews := []ClusterView{}

	for _, view := range views {
		if view.ConnectionError != nil {
			connectionFailures++
		} else {
			workingViews = append(workingViews, view)
		}
	}

	// If no working views, something is very wrong
	if len(workingViews) == 0 {
		analysis.RepairAction = RepairActionRestartAll
		analysis.ErrorMessage = "Cannot connect to any Neo4j server pods"
		return analysis
	}

	// If too many connection failures, we can't determine split-brain status
	if connectionFailures > expectedServers/2 {
		analysis.RepairAction = RepairActionInvestigate
		analysis.ErrorMessage = fmt.Sprintf("Too many connection failures (%d/%d) to determine cluster status",
			connectionFailures, len(views))
		return analysis
	}

	// Analyze cluster consistency
	// Group pods by the servers they can see
	clusterGroups := d.groupPodsByClusterView(workingViews)

	// Find the largest cluster group
	var largestGroup []ClusterView
	maxSize := 0

	for _, group := range clusterGroups {
		// Count unique servers seen by this group
		uniqueServers := d.countUniqueServersInGroup(group)
		if uniqueServers > maxSize {
			maxSize = uniqueServers
			largestGroup = group
		}
	}

	if len(largestGroup) > 0 {
		analysis.LargestCluster = largestGroup[0]
	}

	// Determine if this is a split-brain scenario
	if len(clusterGroups) > 1 {
		analysis.IsSplitBrain = true

		// Find orphaned pods (pods not in the largest cluster group)
		orphanedPods := []string{}
		for _, group := range clusterGroups {
			if !d.isSameGroup(group, largestGroup) {
				for _, view := range group {
					orphanedPods = append(orphanedPods, view.ServerPodName)
				}
			}
		}

		analysis.OrphanedPods = orphanedPods
		analysis.RepairAction = RepairActionRestartPods
		analysis.ErrorMessage = fmt.Sprintf("Split-brain detected: %d cluster groups found, %d orphaned pods",
			len(clusterGroups), len(orphanedPods))
	} else {
		// Single cluster group - check if it has the right number of servers
		uniqueServers := d.countUniqueServersInGroup(workingViews)
		if uniqueServers < expectedServers {
			// Some servers are missing from the cluster view - this is normal during formation
			// Only consider it split-brain if some pods are seeing different servers than others
			analysis.RepairAction = RepairActionWaitForming
			analysis.ErrorMessage = fmt.Sprintf("Cluster formation in progress: %d/%d servers visible", uniqueServers, expectedServers)
		}
	}

	return analysis
}

// groupPodsByClusterView groups pods that see the same cluster configuration
func (d *SplitBrainDetector) groupPodsByClusterView(views []ClusterView) [][]ClusterView {
	groups := [][]ClusterView{}

	for _, view := range views {
		// Find if this view belongs to an existing group
		foundGroup := false
		for i, group := range groups {
			if len(group) > 0 && d.haveSimilarClusterView(view, group[0]) {
				groups[i] = append(groups[i], view)
				foundGroup = true
				break
			}
		}

		// If no matching group found, create a new one
		if !foundGroup {
			groups = append(groups, []ClusterView{view})
		}
	}

	return groups
}

// haveSimilarClusterView determines if two pods see a similar cluster configuration
func (d *SplitBrainDetector) haveSimilarClusterView(view1, view2 ClusterView) bool {
	// Compare the server addresses each view can see
	addresses1 := d.getVisibleServerAddresses(view1.Servers)
	addresses2 := d.getVisibleServerAddresses(view2.Servers)

	// Views are similar if they see mostly the same servers
	commonCount := d.countCommonAddresses(addresses1, addresses2)
	totalCount := len(addresses1) + len(addresses2) - commonCount

	// Consider views similar if they share at least 80% of servers
	if totalCount == 0 {
		return true
	}

	similarity := float64(commonCount*2) / float64(totalCount)
	return similarity >= 0.8
}

// countUniqueServersInGroup counts unique servers visible across a group of views
func (d *SplitBrainDetector) countUniqueServersInGroup(group []ClusterView) int {
	addresses := make(map[string]bool)

	for _, view := range group {
		for _, server := range view.Servers {
			if server.State == "Enabled" && server.Health == "Available" {
				addresses[server.Address] = true
			}
		}
	}

	return len(addresses)
}

// isSameGroup checks if two cluster groups are the same
func (d *SplitBrainDetector) isSameGroup(group1, group2 []ClusterView) bool {
	if len(group1) != len(group2) {
		return false
	}

	// Compare pod names in both groups
	names1 := make([]string, len(group1))
	names2 := make([]string, len(group2))

	for i, view := range group1 {
		names1[i] = view.ServerPodName
	}
	for i, view := range group2 {
		names2[i] = view.ServerPodName
	}

	sort.Strings(names1)
	sort.Strings(names2)

	for i := range names1 {
		if names1[i] != names2[i] {
			return false
		}
	}

	return true
}

// getVisibleServerAddresses extracts addresses from server list
func (d *SplitBrainDetector) getVisibleServerAddresses(servers []neo4jclient.ServerInfo) []string {
	addresses := make([]string, 0, len(servers))

	for _, server := range servers {
		if server.State == "Enabled" && server.Health == "Available" {
			addresses = append(addresses, server.Address)
		}
	}

	return addresses
}

// getPodExpectedAddress returns the expected FQDN address for a pod
func (d *SplitBrainDetector) getPodExpectedAddress(podName string) string {
	// This would need to be filled with the actual cluster namespace and service name
	// For now, return a pattern that can be matched
	return podName + ".headless.svc.cluster.local:7687"
}

// isAddressVisible checks if an address is in the visible list
func (d *SplitBrainDetector) isAddressVisible(address string, visibleAddresses []string) bool {
	for _, visible := range visibleAddresses {
		if visible == address {
			return true
		}
	}
	return false
}

// countCommonAddresses counts addresses that appear in both lists
func (d *SplitBrainDetector) countCommonAddresses(addresses1, addresses2 []string) int {
	common := 0

	for _, addr1 := range addresses1 {
		for _, addr2 := range addresses2 {
			if addr1 == addr2 {
				common++
				break
			}
		}
	}

	return common
}

// RepairSplitBrain attempts to repair a detected split-brain scenario
func (d *SplitBrainDetector) RepairSplitBrain(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, analysis *SplitBrainAnalysis) error {
	logger := log.FromContext(ctx)

	if !analysis.IsSplitBrain {
		logger.Info("No split-brain detected, no repair needed")
		return nil
	}

	logger.Info("Starting split-brain repair",
		"repairAction", analysis.RepairAction,
		"orphanedPods", analysis.OrphanedPods)

	switch analysis.RepairAction {
	case RepairActionRestartPods:
		return d.repairByRestartingPods(ctx, cluster, analysis.OrphanedPods)
	case RepairActionRestartAll:
		return d.repairByRestartingAllPods(ctx, cluster)
	case RepairActionWaitForming:
		logger.Info("Cluster still forming, no immediate action needed")
		return nil
	case RepairActionInvestigate:
		logger.Info("Manual investigation required for split-brain scenario",
			"error", analysis.ErrorMessage)
		return fmt.Errorf("manual investigation required: %s", analysis.ErrorMessage)
	default:
		return fmt.Errorf("unknown repair action: %s", analysis.RepairAction)
	}
}

// repairByRestartingPods repairs split-brain by restarting specific pods
func (d *SplitBrainDetector) repairByRestartingPods(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, podNames []string) error {
	logger := log.FromContext(ctx)

	logger.Info("Restarting orphaned pods to repair split-brain", "pods", podNames)

	for _, podName := range podNames {
		// Delete the pod - StatefulSet will recreate it
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: cluster.Namespace,
			},
		}

		err := d.Client.Delete(ctx, pod)
		if err != nil {
			logger.Error(err, "Failed to delete orphaned pod", "pod", podName)
			return fmt.Errorf("failed to delete pod %s: %w", podName, err)
		}

		logger.Info("Successfully deleted orphaned pod", "pod", podName)

		// Wait a moment between deletions to avoid overwhelming the system
		time.Sleep(2 * time.Second)
	}

	return nil
}

// repairByRestartingAllPods repairs split-brain by restarting all pods (nuclear option)
func (d *SplitBrainDetector) repairByRestartingAllPods(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	logger.Info("Nuclear option: restarting all cluster pods to repair split-brain")

	// Get all server pods
	serverPods, err := d.getServerPods(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to get server pods: %w", err)
	}

	// Delete all pods
	for _, pod := range serverPods {
		err := d.Client.Delete(ctx, &pod)
		if err != nil {
			logger.Error(err, "Failed to delete pod", "pod", pod.Name)
			return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
		}

		logger.Info("Successfully deleted pod", "pod", pod.Name)
		time.Sleep(2 * time.Second)
	}

	return nil
}
