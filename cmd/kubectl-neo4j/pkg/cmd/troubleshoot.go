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

package cmd

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/cmd/kubectl-neo4j/pkg/util"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// NewTroubleshootCommand creates the troubleshoot command with subcommands
func NewTroubleshootCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "troubleshoot",
		Short: "Troubleshoot Neo4j clusters",
		Long:  "Diagnose and troubleshoot issues with Neo4j Enterprise clusters.",
	}

	cmd.AddCommand(newTroubleshootDiagnoseCommand(configFlags))
	cmd.AddCommand(newTroubleshootConnectivityCommand(configFlags))
	cmd.AddCommand(newTroubleshootResourcesCommand(configFlags))
	cmd.AddCommand(newTroubleshootConfigCommand(configFlags))

	return cmd
}

func newTroubleshootDiagnoseCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
		detailed    bool
	)

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Run comprehensive diagnostics on a Neo4j cluster",
		Long:  "Run a comprehensive diagnostic check on a Neo4j Enterprise cluster to identify potential issues.",
		Example: `  # Run basic diagnostics
  kubectl neo4j troubleshoot diagnose --cluster=production

  # Run detailed diagnostics
  kubectl neo4j troubleshoot diagnose --cluster=production --detailed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)
			k8sClient := ctx.Value("k8sClient").(*kubernetes.Clientset)

			namespace := util.GetNamespace(configFlags)

			if clusterName == "" {
				return fmt.Errorf("cluster name is required")
			}

			return runDiagnostics(ctx, crClient, k8sClient, clusterName, namespace, detailed)
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().BoolVar(&detailed, "detailed", false, "Run detailed diagnostics")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

func newTroubleshootConnectivityCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
		endpoint    string
	)

	cmd := &cobra.Command{
		Use:   "connectivity",
		Short: "Test connectivity to a Neo4j cluster",
		Long:  "Test network connectivity and service availability for a Neo4j Enterprise cluster.",
		Example: `  # Test connectivity to a cluster
  kubectl neo4j troubleshoot connectivity --cluster=production

  # Test connectivity to a specific endpoint
  kubectl neo4j troubleshoot connectivity --cluster=production --endpoint=bolt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)
			k8sClient := ctx.Value("k8sClient").(*kubernetes.Clientset)

			namespace := util.GetNamespace(configFlags)

			if clusterName == "" {
				return fmt.Errorf("cluster name is required")
			}

			return testConnectivity(ctx, crClient, k8sClient, clusterName, namespace)
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Specific endpoint to test (bolt|http|https)")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

func newTroubleshootResourcesCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
	)

	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Check resource allocation and usage",
		Long:  "Check resource allocation, limits, and usage for a Neo4j Enterprise cluster.",
		Example: `  # Check resource allocation
  kubectl neo4j troubleshoot resources --cluster=production`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)
			k8sClient := ctx.Value("k8sClient").(*kubernetes.Clientset)

			namespace := util.GetNamespace(configFlags)

			if clusterName == "" {
				return fmt.Errorf("cluster name is required")
			}

			return checkResources(ctx, crClient, k8sClient, clusterName, namespace)
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

func newTroubleshootConfigCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
	)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Validate cluster configuration",
		Long:  "Validate the configuration of a Neo4j Enterprise cluster and identify potential issues.",
		Example: `  # Validate cluster configuration
  kubectl neo4j troubleshoot config --cluster=production`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			namespace := util.GetNamespace(configFlags)

			if clusterName == "" {
				return fmt.Errorf("cluster name is required")
			}

			return validateConfig(ctx, crClient, clusterName, namespace)
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

// Helper functions
func runDiagnostics(ctx context.Context, crClient client.Client, k8sClient *kubernetes.Clientset, clusterName, namespace string, detailed bool) error {
	// Get cluster
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	if err := crClient.Get(ctx, types.NamespacedName{
		Name:      clusterName,
		Namespace: namespace,
	}, &cluster); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("cluster %s not found in namespace %s", clusterName, namespace)
		}
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	fmt.Printf("Diagnostics Report: %s\n", clusterName)
	fmt.Printf("=====================%s\n", strings.Repeat("=", len(clusterName)))

	// Check cluster status
	fmt.Printf("\n1. Cluster Status Check\n")
	fmt.Printf("   Phase: %s", cluster.Status.Phase)
	if cluster.Status.Phase == "Ready" {
		fmt.Printf(" ✓\n")
	} else {
		fmt.Printf(" ✗\n")
		if cluster.Status.Message != "" {
			fmt.Printf("   Message: %s\n", cluster.Status.Message)
		}
	}

	// Check replicas
	fmt.Printf("\n2. Replica Status Check\n")
	if cluster.Status.Replicas != nil {
		expectedTotal := cluster.Spec.Topology.Primaries + cluster.Spec.Topology.Secondaries
		actualTotal := cluster.Status.Replicas.Primaries + cluster.Status.Replicas.Secondaries

		if actualTotal == expectedTotal && cluster.Status.Replicas.Ready == actualTotal {
			fmt.Printf("   All replicas ready ✓\n")
		} else {
			fmt.Printf("   Replica issues detected ✗\n")
			fmt.Printf("   Expected: %d primaries, %d secondaries\n",
				cluster.Spec.Topology.Primaries, cluster.Spec.Topology.Secondaries)
			fmt.Printf("   Actual: %d primaries, %d secondaries\n",
				cluster.Status.Replicas.Primaries, cluster.Status.Replicas.Secondaries)
			fmt.Printf("   Ready: %d/%d\n", cluster.Status.Replicas.Ready, expectedTotal)
		}
	}

	// Check conditions
	fmt.Printf("\n3. Condition Checks\n")
	if len(cluster.Status.Conditions) == 0 {
		fmt.Printf("   No conditions reported ⚠\n")
	} else {
		allHealthy := true
		for _, condition := range cluster.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				fmt.Printf("   %s: OK ✓\n", condition.Type)
			} else {
				fmt.Printf("   %s: %s ✗\n", condition.Type, condition.Reason)
				allHealthy = false
				if detailed && condition.Message != "" {
					fmt.Printf("     %s\n", condition.Message)
				}
			}
		}
		if allHealthy {
			fmt.Printf("   All conditions healthy ✓\n")
		}
	}

	// Check endpoints
	fmt.Printf("\n4. Endpoint Availability\n")
	if cluster.Status.Endpoints != nil {
		if cluster.Status.Endpoints.Bolt != "" {
			fmt.Printf("   Bolt: %s ✓\n", cluster.Status.Endpoints.Bolt)
		}
		if cluster.Status.Endpoints.HTTP != "" {
			fmt.Printf("   HTTP: %s ✓\n", cluster.Status.Endpoints.HTTP)
		}
		if cluster.Status.Endpoints.HTTPS != "" {
			fmt.Printf("   HTTPS: %s ✓\n", cluster.Status.Endpoints.HTTPS)
		}
	} else {
		fmt.Printf("   No endpoints available ✗\n")
	}

	if detailed {
		// Additional detailed checks
		fmt.Printf("\n5. Pod Status (Detailed)\n")
		if err := analyzePodsDetailed(ctx, k8sClient, clusterName, namespace); err != nil {
			fmt.Printf("   Error analyzing pods: %v\n", err)
		}

		fmt.Printf("\n6. Resource Usage (Detailed)\n")
		if err := analyzeResourceUsageDetailed(ctx, k8sClient, clusterName, namespace); err != nil {
			fmt.Printf("   Error analyzing resource usage: %v\n", err)
		}

		fmt.Printf("\n7. Configuration Validation (Detailed)\n")
		if err := validateConfigurationDetailed(ctx, crClient, clusterName, namespace); err != nil {
			fmt.Printf("   Error validating configuration: %v\n", err)
		}
	}

	fmt.Printf("\nDiagnostics completed.\n")
	return nil
}

func testConnectivity(ctx context.Context, crClient client.Client, k8sClient *kubernetes.Clientset, clusterName, namespace string) error {
	fmt.Printf("Testing connectivity for cluster %s...\n\n", clusterName)

	// Get cluster information
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	if err := crClient.Get(ctx, types.NamespacedName{
		Name:      clusterName,
		Namespace: namespace,
	}, &cluster); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("cluster %s not found in namespace %s", clusterName, namespace)
		}
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	// Test service endpoints
	fmt.Printf("Service Endpoint Tests:\n")
	if cluster.Status.Endpoints != nil {
		if cluster.Status.Endpoints.Bolt != "" {
			err := testBoltConnectivity(cluster.Status.Endpoints.Bolt)
			if err != nil {
				fmt.Printf("  ❌ Bolt (%s): %v\n", cluster.Status.Endpoints.Bolt, err)
			} else {
				fmt.Printf("  ✅ Bolt (%s): Connected\n", cluster.Status.Endpoints.Bolt)
			}
		}

		if cluster.Status.Endpoints.HTTP != "" {
			err := testHTTPConnectivity(cluster.Status.Endpoints.HTTP)
			if err != nil {
				fmt.Printf("  ❌ HTTP (%s): %v\n", cluster.Status.Endpoints.HTTP, err)
			} else {
				fmt.Printf("  ✅ HTTP (%s): Connected\n", cluster.Status.Endpoints.HTTP)
			}
		}

		if cluster.Status.Endpoints.HTTPS != "" {
			err := testHTTPSConnectivity(cluster.Status.Endpoints.HTTPS)
			if err != nil {
				fmt.Printf("  ❌ HTTPS (%s): %v\n", cluster.Status.Endpoints.HTTPS, err)
			} else {
				fmt.Printf("  ✅ HTTPS (%s): Connected\n", cluster.Status.Endpoints.HTTPS)
			}
		}
	} else {
		fmt.Printf("  ⚠️  No service endpoints configured\n")
	}

	// Test pod network connectivity
	fmt.Printf("\nPod Network Tests:\n")
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "neo4j",
	})

	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("  ⚠️  No pods found for cluster\n")
	} else {
		readyPods, totalPods := 0, len(pods.Items)
		for _, pod := range pods.Items {
			status := "Not Ready"
			if isPodReady(pod) {
				status = "Ready"
				readyPods++
			}
			fmt.Printf("  Pod %s: %s (%s)\n", pod.Name, status, pod.Status.Phase)
		}
		fmt.Printf("  Summary: %d/%d pods ready\n", readyPods, totalPods)
	}

	// Test service resolution
	fmt.Printf("\nService Resolution Tests:\n")
	services, err := k8sClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	if len(services.Items) == 0 {
		fmt.Printf("  ⚠️  No services found for cluster\n")
	} else {
		for _, service := range services.Items {
			clusterIP := service.Spec.ClusterIP
			if clusterIP == "None" {
				fmt.Printf("  Service %s: Headless service\n", service.Name)
			} else if clusterIP != "" {
				fmt.Printf("  Service %s: %s", service.Name, clusterIP)
				if len(service.Spec.Ports) > 0 {
					fmt.Printf(" (ports: %v)", getServicePorts(service))
				}
				fmt.Printf("\n")
			}
		}
	}

	return nil
}

func checkResources(ctx context.Context, crClient client.Client, k8sClient *kubernetes.Clientset, clusterName, namespace string) error {
	fmt.Printf("Resource Analysis: %s\n", clusterName)
	fmt.Printf("===================%s\n", strings.Repeat("=", len(clusterName)))

	// Get cluster
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	if err := crClient.Get(ctx, types.NamespacedName{
		Name:      clusterName,
		Namespace: namespace,
	}, &cluster); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("cluster %s not found in namespace %s", clusterName, namespace)
		}
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	// Get pods for resource analysis
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "neo4j",
	})

	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("No pods found for cluster %s\n", clusterName)
		return nil
	}

	// Analyze resource requests and limits
	fmt.Printf("\n1. Resource Requests and Limits\n")
	totalCPURequests := resource.NewQuantity(0, resource.DecimalSI)
	totalMemoryRequests := resource.NewQuantity(0, resource.BinarySI)
	totalCPULimits := resource.NewQuantity(0, resource.DecimalSI)
	totalMemoryLimits := resource.NewQuantity(0, resource.BinarySI)

	for _, pod := range pods.Items {
		fmt.Printf("   Pod: %s\n", pod.Name)

		podCPURequests := resource.NewQuantity(0, resource.DecimalSI)
		podMemoryRequests := resource.NewQuantity(0, resource.BinarySI)
		podCPULimits := resource.NewQuantity(0, resource.DecimalSI)
		podMemoryLimits := resource.NewQuantity(0, resource.BinarySI)

		for _, container := range pod.Spec.Containers {
			// CPU requests
			if cpuReq := container.Resources.Requests[corev1.ResourceCPU]; !cpuReq.IsZero() {
				podCPURequests.Add(cpuReq)
				totalCPURequests.Add(cpuReq)
			}
			// Memory requests
			if memReq := container.Resources.Requests[corev1.ResourceMemory]; !memReq.IsZero() {
				podMemoryRequests.Add(memReq)
				totalMemoryRequests.Add(memReq)
			}
			// CPU limits
			if cpuLimit := container.Resources.Limits[corev1.ResourceCPU]; !cpuLimit.IsZero() {
				podCPULimits.Add(cpuLimit)
				totalCPULimits.Add(cpuLimit)
			}
			// Memory limits
			if memLimit := container.Resources.Limits[corev1.ResourceMemory]; !memLimit.IsZero() {
				podMemoryLimits.Add(memLimit)
				totalMemoryLimits.Add(memLimit)
			}
		}

		fmt.Printf("     Requests: CPU=%s, Memory=%s\n",
			formatResource(podCPURequests), formatResource(podMemoryRequests))
		fmt.Printf("     Limits:   CPU=%s, Memory=%s\n",
			formatResource(podCPULimits), formatResource(podMemoryLimits))
	}

	fmt.Printf("   Total Cluster Resources:\n")
	fmt.Printf("     Requests: CPU=%s, Memory=%s\n",
		formatResource(totalCPURequests), formatResource(totalMemoryRequests))
	fmt.Printf("     Limits:   CPU=%s, Memory=%s\n",
		formatResource(totalCPULimits), formatResource(totalMemoryLimits))

	// Check storage
	fmt.Printf("\n2. Storage Analysis\n")
	pvcs, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		fmt.Printf("   Failed to get PVCs: %v\n", err)
	} else if len(pvcs.Items) == 0 {
		fmt.Printf("   No PVCs found for cluster\n")
	} else {
		for _, pvc := range pvcs.Items {
			status := string(pvc.Status.Phase)
			capacity := "Unknown"
			if pvc.Status.Capacity != nil {
				if storage := pvc.Status.Capacity[corev1.ResourceStorage]; !storage.IsZero() {
					capacity = storage.String()
				}
			}
			fmt.Printf("   PVC %s: %s (Capacity: %s)\n", pvc.Name, status, capacity)
		}
	}

	// Check node placement
	fmt.Printf("\n3. Node Placement Analysis\n")
	nodeMap := make(map[string]int)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" {
			nodeMap[pod.Spec.NodeName]++
		}
	}

	if len(nodeMap) == 0 {
		fmt.Printf("   No pods scheduled to nodes\n")
	} else {
		fmt.Printf("   Pod distribution across nodes:\n")
		for node, count := range nodeMap {
			fmt.Printf("     Node %s: %d pods\n", node, count)
		}

		// Check for uneven distribution
		if len(nodeMap) > 1 {
			minPods, maxPods := findMinMax(nodeMap)
			if maxPods-minPods > 1 {
				fmt.Printf("   ⚠️  Uneven pod distribution detected (range: %d-%d pods per node)\n", minPods, maxPods)
			} else {
				fmt.Printf("   ✅ Even pod distribution\n")
			}
		}
	}

	// Resource recommendations
	fmt.Printf("\n4. Resource Recommendations\n")
	if totalCPURequests.IsZero() {
		fmt.Printf("   ⚠️  No CPU requests specified - consider adding resource requests\n")
	}
	if totalMemoryRequests.IsZero() {
		fmt.Printf("   ⚠️  No memory requests specified - consider adding resource requests\n")
	}
	if totalCPULimits.IsZero() {
		fmt.Printf("   ⚠️  No CPU limits specified - consider adding resource limits to prevent resource contention\n")
	}
	if totalMemoryLimits.IsZero() {
		fmt.Printf("   ⚠️  No memory limits specified - consider adding resource limits to prevent OOM kills\n")
	}

	return nil
}

func formatResource(q *resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	return q.String()
}

func findMinMax(nodeMap map[string]int) (int, int) {
	if len(nodeMap) == 0 {
		return 0, 0
	}

	min, max := int(^uint(0)>>1), 0 // Initialize min to max int, max to 0
	for _, count := range nodeMap {
		if count < min {
			min = count
		}
		if count > max {
			max = count
		}
	}
	return min, max
}

func validateConfig(ctx context.Context, crClient client.Client, clusterName, namespace string) error {
	// Get cluster
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	if err := crClient.Get(ctx, types.NamespacedName{
		Name:      clusterName,
		Namespace: namespace,
	}, &cluster); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("cluster %s not found in namespace %s", clusterName, namespace)
		}
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	fmt.Printf("Configuration Validation: %s\n", clusterName)
	fmt.Printf("=========================%s\n", strings.Repeat("=", len(clusterName)))

	issues := []string{}

	// Check topology configuration
	fmt.Printf("\n1. Topology Configuration\n")
	if cluster.Spec.Topology.Primaries%2 == 0 {
		issues = append(issues, "Even number of primaries may affect quorum")
		fmt.Printf("   ⚠ Even number of primaries (%d) - consider using odd numbers for better quorum\n", cluster.Spec.Topology.Primaries)
	} else {
		fmt.Printf("   ✓ Primary count is odd (%d)\n", cluster.Spec.Topology.Primaries)
	}

	if cluster.Spec.Topology.Primaries > 7 {
		issues = append(issues, "Too many primary nodes")
		fmt.Printf("   ⚠ High number of primaries (%d) - consider using secondaries for read scaling\n", cluster.Spec.Topology.Primaries)
	}

	// Check auto-scaling configuration
	fmt.Printf("\n2. Auto-scaling Configuration\n")
	if cluster.Spec.AutoScaling != nil && cluster.Spec.AutoScaling.Enabled {
		fmt.Printf("   ✓ Auto-scaling enabled\n")
		if cluster.Spec.AutoScaling.Primaries != nil {
			if cluster.Spec.AutoScaling.Primaries.MaxReplicas%2 == 0 {
				issues = append(issues, "Auto-scaling max primaries is even")
				fmt.Printf("   ⚠ Max primaries (%d) is even - may affect quorum during scaling\n",
					cluster.Spec.AutoScaling.Primaries.MaxReplicas)
			}
		}
	} else {
		fmt.Printf("   - Auto-scaling disabled\n")
	}

	// Check storage configuration
	fmt.Printf("\n3. Storage Configuration\n")
	fmt.Printf("   Size: %s\n", cluster.Spec.Storage.Size)
	if cluster.Spec.Storage.ClassName != "" {
		fmt.Printf("   Storage Class: %s\n", cluster.Spec.Storage.ClassName)
	} else {
		issues = append(issues, "No storage class specified")
		fmt.Printf("   ⚠ No storage class specified - using default\n")
	}

	// Summary
	fmt.Printf("\nValidation Summary:\n")
	if len(issues) == 0 {
		fmt.Printf("✓ No configuration issues detected\n")
	} else {
		fmt.Printf("⚠ %d potential issues detected:\n", len(issues))
		for i, issue := range issues {
			fmt.Printf("  %d. %s\n", i+1, issue)
		}
	}

	return nil
}

func testBoltConnectivity(endpoint string) error {
	host, port := parseEndpoint(endpoint, "bolt://", "7687")
	return testTCPConnection(host, port, 5*time.Second)
}

func testHTTPConnectivity(endpoint string) error {
	host, port := parseEndpoint(endpoint, "http://", "7474")
	return testTCPConnection(host, port, 5*time.Second)
}

func testHTTPSConnectivity(endpoint string) error {
	host, port := parseEndpoint(endpoint, "https://", "7473")
	return testTCPConnection(host, port, 5*time.Second)
}

func parseEndpoint(endpoint, prefix, defaultPort string) (string, string) {
	endpoint = strings.TrimPrefix(endpoint, prefix)
	if !strings.Contains(endpoint, ":") {
		return endpoint, defaultPort
	}
	parts := strings.Split(endpoint, ":")
	return parts[0], parts[1]
}

func testTCPConnection(host, port string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func isPodReady(pod v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func getServicePorts(service v1.Service) []string {
	var ports []string
	for _, port := range service.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%d", port.Port))
	}
	return ports
}

func analyzePodsDetailed(ctx context.Context, k8sClient *kubernetes.Clientset, clusterName, namespace string) error {
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "neo4j",
	})

	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("   No pods found for cluster %s\n", clusterName)
		return nil
	}

	for _, pod := range pods.Items {
		fmt.Printf("   Pod: %s\n", pod.Name)
		fmt.Printf("     Phase: %s\n", pod.Status.Phase)
		fmt.Printf("     Node: %s\n", pod.Spec.NodeName)

		if pod.Status.StartTime != nil {
			age := time.Since(pod.Status.StartTime.Time)
			fmt.Printf("     Age: %s\n", formatDuration(age))
		}

		// Check conditions
		fmt.Printf("     Conditions:\n")
		for _, condition := range pod.Status.Conditions {
			status := "False"
			if condition.Status == corev1.ConditionTrue {
				status = "True"
			}
			fmt.Printf("       %s: %s", condition.Type, status)
			if condition.Message != "" {
				fmt.Printf(" (%s)", condition.Message)
			}
			fmt.Printf("\n")
		}

		// Check container statuses
		fmt.Printf("     Containers:\n")
		for _, containerStatus := range pod.Status.ContainerStatuses {
			fmt.Printf("       %s:\n", containerStatus.Name)
			fmt.Printf("         Ready: %t\n", containerStatus.Ready)
			fmt.Printf("         Restart Count: %d\n", containerStatus.RestartCount)

			if containerStatus.State.Running != nil {
				fmt.Printf("         State: Running (since %s)\n",
					containerStatus.State.Running.StartedAt.Format("15:04:05"))
			} else if containerStatus.State.Waiting != nil {
				fmt.Printf("         State: Waiting (%s: %s)\n",
					containerStatus.State.Waiting.Reason,
					containerStatus.State.Waiting.Message)
			} else if containerStatus.State.Terminated != nil {
				fmt.Printf("         State: Terminated (%s: %s)\n",
					containerStatus.State.Terminated.Reason,
					containerStatus.State.Terminated.Message)
			}
		}

		// Check recent events for this pod
		events, err := k8sClient.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
		})
		if err == nil && len(events.Items) > 0 {
			fmt.Printf("     Recent Events:\n")
			// Sort events by timestamp
			eventList := events.Items
			for i := 0; i < len(eventList)-1; i++ {
				for j := i + 1; j < len(eventList); j++ {
					if eventList[i].LastTimestamp.Before(&eventList[j].LastTimestamp) {
						eventList[i], eventList[j] = eventList[j], eventList[i]
					}
				}
			}

			// Show last 3 events
			maxEvents := 3
			if len(eventList) < maxEvents {
				maxEvents = len(eventList)
			}

			for _, event := range eventList[:maxEvents] {
				timestamp := event.LastTimestamp.Format("15:04:05")
				fmt.Printf("       %s %s: %s\n", timestamp, event.Reason, event.Message)
			}
		}
		fmt.Printf("\n")
	}

	return nil
}

func analyzeResourceUsageDetailed(ctx context.Context, k8sClient *kubernetes.Clientset, clusterName, namespace string) error {
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": clusterName,
		"app.kubernetes.io/name":     "neo4j",
	})

	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("   No pods found for cluster %s\n", clusterName)
		return nil
	}

	// Analyze resource distribution
	nodeUsage := make(map[string]struct {
		cpuRequests    resource.Quantity
		memoryRequests resource.Quantity
		podCount       int
	})

	var totalCPU, totalMemory resource.Quantity

	for _, pod := range pods.Items {
		var podCPU, podMemory resource.Quantity

		for _, container := range pod.Spec.Containers {
			if cpu := container.Resources.Requests[corev1.ResourceCPU]; !cpu.IsZero() {
				podCPU.Add(cpu)
				totalCPU.Add(cpu)
			}
			if memory := container.Resources.Requests[corev1.ResourceMemory]; !memory.IsZero() {
				podMemory.Add(memory)
				totalMemory.Add(memory)
			}
		}

		nodeName := pod.Spec.NodeName
		if nodeName != "" {
			usage := nodeUsage[nodeName]
			usage.cpuRequests.Add(podCPU)
			usage.memoryRequests.Add(podMemory)
			usage.podCount++
			nodeUsage[nodeName] = usage
		}
	}

	// Display resource distribution
	fmt.Printf("   Total Cluster Resources:\n")
	fmt.Printf("     CPU Requests: %s\n", formatResourceShort(totalCPU))
	fmt.Printf("     Memory Requests: %s\n", formatResourceShort(totalMemory))
	fmt.Printf("     Total Pods: %d\n", len(pods.Items))

	fmt.Printf("\n   Resource Distribution by Node:\n")
	for nodeName, usage := range nodeUsage {
		fmt.Printf("     Node: %s\n", nodeName)
		fmt.Printf("       CPU: %s\n", formatResourceShort(usage.cpuRequests))
		fmt.Printf("       Memory: %s\n", formatResourceShort(usage.memoryRequests))
		fmt.Printf("       Pod Count: %d\n", usage.podCount)
	}

	// Check for resource imbalances
	if len(nodeUsage) > 1 {
		fmt.Printf("\n   Resource Balance Analysis:\n")
		minPods, maxPods := getMinMaxPods(nodeUsage)
		if maxPods-minPods > 1 {
			fmt.Printf("     ⚠️  Pod distribution imbalance detected (min: %d, max: %d pods per node)\n", minPods, maxPods)
		} else {
			fmt.Printf("     ✅ Pod distribution is well balanced\n")
		}
	}

	// Check storage usage
	pvcs, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err == nil && len(pvcs.Items) > 0 {
		fmt.Printf("\n   Storage Usage:\n")
		var totalStorage resource.Quantity
		for _, pvc := range pvcs.Items {
			if storage := pvc.Status.Capacity[corev1.ResourceStorage]; !storage.IsZero() {
				totalStorage.Add(storage)
				fmt.Printf("     PVC %s: %s\n", pvc.Name, formatResourceShort(storage))
			}
		}
		fmt.Printf("     Total Storage: %s\n", formatResourceShort(totalStorage))
	}

	return nil
}

func validateConfigurationDetailed(ctx context.Context, crClient client.Client, clusterName, namespace string) error {
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	if err := crClient.Get(ctx, types.NamespacedName{
		Name:      clusterName,
		Namespace: namespace,
	}, &cluster); err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	fmt.Printf("   Configuration Analysis for %s:\n", clusterName)

	// Validate topology configuration
	fmt.Printf("     Topology:\n")
	if cluster.Spec.Topology.Primaries < 1 {
		fmt.Printf("       ❌ Primary count (%d) must be at least 1\n", cluster.Spec.Topology.Primaries)
	} else if cluster.Spec.Topology.Primaries%2 == 0 {
		fmt.Printf("       ⚠️  Primary count (%d) should be odd for proper quorum\n", cluster.Spec.Topology.Primaries)
	} else {
		fmt.Printf("       ✅ Primary count (%d) is properly configured\n", cluster.Spec.Topology.Primaries)
	}

	if cluster.Spec.Topology.Secondaries < 0 {
		fmt.Printf("       ❌ Secondary count (%d) cannot be negative\n", cluster.Spec.Topology.Secondaries)
	} else {
		fmt.Printf("       ✅ Secondary count (%d) is valid\n", cluster.Spec.Topology.Secondaries)
	}

	// Validate resource configuration
	fmt.Printf("     Resources:\n")
	if cluster.Spec.Resources == nil || (len(cluster.Spec.Resources.Requests) == 0 && len(cluster.Spec.Resources.Limits) == 0) {
		fmt.Printf("       ⚠️  No resource requests specified - may cause scheduling issues\n")
	} else {
		if cluster.Spec.Resources.Requests != nil {
			if cpu := cluster.Spec.Resources.Requests[corev1.ResourceCPU]; !cpu.IsZero() {
				fmt.Printf("       ✅ CPU requests: %s\n", cpu.String())
			}
			if memory := cluster.Spec.Resources.Requests[corev1.ResourceMemory]; !memory.IsZero() {
				fmt.Printf("       ✅ Memory requests: %s\n", memory.String())
			}
		}
		if cluster.Spec.Resources.Limits != nil {
			if cpu := cluster.Spec.Resources.Limits[corev1.ResourceCPU]; !cpu.IsZero() {
				fmt.Printf("       ✅ CPU limits: %s\n", cpu.String())
			}
			if memory := cluster.Spec.Resources.Limits[corev1.ResourceMemory]; !memory.IsZero() {
				fmt.Printf("       ✅ Memory limits: %s\n", memory.String())
			}
		}
	}

	// Validate storage configuration
	fmt.Printf("     Storage:\n")
	if cluster.Spec.Storage.Size == "" {
		fmt.Printf("       ❌ Storage size not specified\n")
	} else {
		fmt.Printf("       ✅ Storage size: %s\n", cluster.Spec.Storage.Size)
	}

	if cluster.Spec.Storage.ClassName == "" {
		fmt.Printf("       ⚠️  Storage class not specified - using default\n")
	} else {
		fmt.Printf("       ✅ Storage class: %s\n", cluster.Spec.Storage.ClassName)
	}

	// Validate image configuration
	fmt.Printf("     Image:\n")
	if cluster.Spec.Image.Repo == "" {
		fmt.Printf("       ❌ Image repository not specified\n")
	} else {
		fmt.Printf("       ✅ Image repository: %s\n", cluster.Spec.Image.Repo)
	}

	if cluster.Spec.Image.Tag == "" {
		fmt.Printf("       ⚠️  Image tag not specified - using latest\n")
	} else {
		fmt.Printf("       ✅ Image tag: %s\n", cluster.Spec.Image.Tag)
	}

	// Validate security configuration
	fmt.Printf("     Security:\n")
	if cluster.Spec.Auth == nil || cluster.Spec.Auth.AdminSecret == "" {
		fmt.Printf("       ⚠️  No auth admin secret specified - using default credentials\n")
	} else {
		fmt.Printf("       ✅ Auth admin secret: %s\n", cluster.Spec.Auth.AdminSecret)
	}

	if cluster.Spec.TLS != nil {
		fmt.Printf("       ✅ TLS configuration present\n")
		if cluster.Spec.TLS.Mode == "" {
			fmt.Printf("         ⚠️  TLS mode not specified\n")
		} else {
			fmt.Printf("         ✅ TLS mode: %s\n", cluster.Spec.TLS.Mode)
		}
	} else {
		fmt.Printf("       ⚠️  TLS not configured - connections will be unencrypted\n")
	}

	// Check for common misconfigurations
	fmt.Printf("     Common Issues Check:\n")
	issues := 0

	if cluster.Spec.Topology.Primaries > 7 {
		fmt.Printf("       ⚠️  High primary count (%d) may impact performance\n", cluster.Spec.Topology.Primaries)
		issues++
	}

	if cluster.Spec.Topology.Primaries+cluster.Spec.Topology.Secondaries > 20 {
		fmt.Printf("       ⚠️  Very large cluster size may require special considerations\n")
		issues++
	}

	if issues == 0 {
		fmt.Printf("       ✅ No common configuration issues detected\n")
	}

	return nil
}

func formatResourceShort(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	return q.String()
}

func getMinMaxPods(nodeUsage map[string]struct {
	cpuRequests    resource.Quantity
	memoryRequests resource.Quantity
	podCount       int
}) (int, int) {
	if len(nodeUsage) == 0 {
		return 0, 0
	}

	min, max := int(^uint(0)>>1), 0 // MaxInt, 0
	for _, usage := range nodeUsage {
		if usage.podCount < min {
			min = usage.podCount
		}
		if usage.podCount > max {
			max = usage.podCount
		}
	}
	return min, max
}
