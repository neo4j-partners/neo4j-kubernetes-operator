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

package util

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// GetNamespace returns the namespace from config flags or default
func GetNamespace(configFlags *genericclioptions.ConfigFlags) string {
	if configFlags.Namespace != nil && *configFlags.Namespace != "" {
		return *configFlags.Namespace
	}
	return "default"
}

// Cluster printing functions
func PrintClusters(clusters []neo4jv1alpha1.Neo4jEnterpriseCluster) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPRIMARIES\tSECONDARIES\tPHASE\tAGE")

	for _, cluster := range clusters {
		age := time.Since(cluster.CreationTimestamp.Time).Round(time.Second)
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n",
			cluster.Name,
			cluster.Spec.Topology.Primaries,
			cluster.Spec.Topology.Secondaries,
			cluster.Status.Phase,
			age,
		)
	}
	w.Flush()
}

func PrintClustersWide(clusters []neo4jv1alpha1.Neo4jEnterpriseCluster) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPRIMARIES\tSECONDARIES\tPHASE\tVERSION\tAUTO-SCALE\tTLS\tAGE")

	for _, cluster := range clusters {
		age := time.Since(cluster.CreationTimestamp.Time).Round(time.Second)
		autoScale := "Disabled"
		if cluster.Spec.AutoScaling != nil && cluster.Spec.AutoScaling.Enabled {
			autoScale = "Enabled"
		}
		tls := "Disabled"
		if cluster.Spec.TLS != nil {
			tls = "Enabled"
		}

		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
			cluster.Name,
			cluster.Spec.Topology.Primaries,
			cluster.Spec.Topology.Secondaries,
			cluster.Status.Phase,
			cluster.Spec.Image.Tag,
			autoScale,
			tls,
			age,
		)
	}
	w.Flush()
}

func PrintClusterDetailed(cluster neo4jv1alpha1.Neo4jEnterpriseCluster) {
	fmt.Printf("Name: %s\n", cluster.Name)
	fmt.Printf("Namespace: %s\n", cluster.Namespace)
	fmt.Printf("Created: %s\n", cluster.CreationTimestamp.Format(time.RFC3339))
	fmt.Printf("Phase: %s\n", cluster.Status.Phase)

	if cluster.Status.Message != "" {
		fmt.Printf("Message: %s\n", cluster.Status.Message)
	}

	fmt.Printf("\nTopology:\n")
	fmt.Printf("  Primaries: %d\n", cluster.Spec.Topology.Primaries)
	fmt.Printf("  Secondaries: %d\n", cluster.Spec.Topology.Secondaries)

	fmt.Printf("\nImage:\n")
	fmt.Printf("  Repository: %s\n", cluster.Spec.Image.Repo)
	fmt.Printf("  Tag: %s\n", cluster.Spec.Image.Tag)

	fmt.Printf("\nStorage:\n")
	fmt.Printf("  Size: %s\n", cluster.Spec.Storage.Size)
	if cluster.Spec.Storage.ClassName != "" {
		fmt.Printf("  Storage Class: %s\n", cluster.Spec.Storage.ClassName)
	}

	if cluster.Spec.AutoScaling != nil && cluster.Spec.AutoScaling.Enabled {
		fmt.Printf("\nAuto-scaling: Enabled\n")
		if cluster.Spec.AutoScaling.Primaries != nil {
			fmt.Printf("  Primaries: %d-%d\n",
				cluster.Spec.AutoScaling.Primaries.MinReplicas,
				cluster.Spec.AutoScaling.Primaries.MaxReplicas)
		}
		if cluster.Spec.AutoScaling.Secondaries != nil {
			fmt.Printf("  Secondaries: %d-%d\n",
				cluster.Spec.AutoScaling.Secondaries.MinReplicas,
				cluster.Spec.AutoScaling.Secondaries.MaxReplicas)
		}
	}

	if cluster.Spec.TLS != nil {
		fmt.Printf("\nTLS: Enabled (%s)\n", cluster.Spec.TLS.Mode)
	}

	if len(cluster.Status.Conditions) > 0 {
		fmt.Printf("\nConditions:\n")
		for _, condition := range cluster.Status.Conditions {
			fmt.Printf("  %s: %s (%s)\n", condition.Type, condition.Status, condition.Reason)
			if condition.Message != "" {
				fmt.Printf("    %s\n", condition.Message)
			}
		}
	}
}

func PrintClusterStatus(cluster neo4jv1alpha1.Neo4jEnterpriseCluster) {
	fmt.Printf("Cluster: %s\n", cluster.Name)
	fmt.Printf("Phase: %s\n", cluster.Status.Phase)

	if cluster.Status.Replicas != nil {
		fmt.Printf("Replicas:\n")
		fmt.Printf("  Primaries: %d\n", cluster.Status.Replicas.Primaries)
		fmt.Printf("  Secondaries: %d\n", cluster.Status.Replicas.Secondaries)
		fmt.Printf("  Ready: %d\n", cluster.Status.Replicas.Ready)
	}

	if cluster.Status.Endpoints != nil {
		fmt.Printf("Endpoints:\n")
		if cluster.Status.Endpoints.Bolt != "" {
			fmt.Printf("  Bolt: %s\n", cluster.Status.Endpoints.Bolt)
		}
		if cluster.Status.Endpoints.HTTP != "" {
			fmt.Printf("  HTTP: %s\n", cluster.Status.Endpoints.HTTP)
		}
		if cluster.Status.Endpoints.HTTPS != "" {
			fmt.Printf("  HTTPS: %s\n", cluster.Status.Endpoints.HTTPS)
		}
	}

	if len(cluster.Status.Conditions) > 0 {
		fmt.Printf("Status Conditions:\n")
		for _, condition := range cluster.Status.Conditions {
			status := "✓"
			if condition.Status != metav1.ConditionTrue {
				status = "✗"
			}
			fmt.Printf("  %s %s: %s\n", status, condition.Type, condition.Reason)
		}
	}
}

func PrintClustersJSON(clusters []neo4jv1alpha1.Neo4jEnterpriseCluster) {
	data, _ := json.MarshalIndent(clusters, "", "  ")
	fmt.Println(string(data))
}

func PrintClusterJSON(cluster neo4jv1alpha1.Neo4jEnterpriseCluster) {
	data, _ := json.MarshalIndent(cluster, "", "  ")
	fmt.Println(string(data))
}

func PrintClustersYAML(clusters []neo4jv1alpha1.Neo4jEnterpriseCluster) {
	data, _ := yaml.Marshal(clusters)
	fmt.Println(string(data))
}

func PrintClusterYAML(cluster neo4jv1alpha1.Neo4jEnterpriseCluster) {
	data, _ := yaml.Marshal(cluster)
	fmt.Println(string(data))
}

// Backup printing functions
func PrintBackups(backups []neo4jv1alpha1.Neo4jBackup) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tCLUSTER\tSTORAGE\tPHASE\tSCHEDULE\tAGE")

	for _, backup := range backups {
		age := time.Since(backup.CreationTimestamp.Time).Round(time.Second)
		schedule := backup.Spec.Schedule
		if schedule == "" {
			schedule = "One-time"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			backup.Name,
			backup.Spec.Target.Name,
			backup.Spec.Storage.Type,
			backup.Status.Phase,
			schedule,
			age,
		)
	}
	w.Flush()
}

func PrintBackupsWide(backups []neo4jv1alpha1.Neo4jBackup) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tCLUSTER\tSTORAGE\tPHASE\tSIZE\tDURATION\tSCHEDULE\tAGE")

	for _, backup := range backups {
		age := time.Since(backup.CreationTimestamp.Time).Round(time.Second)
		schedule := backup.Spec.Schedule
		if schedule == "" {
			schedule = "One-time"
		}

		size := ""
		duration := ""
		if backup.Status.Stats != nil {
			size = backup.Status.Stats.Size
			duration = backup.Status.Stats.Duration
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			backup.Name,
			backup.Spec.Target.Name,
			backup.Spec.Storage.Type,
			backup.Status.Phase,
			size,
			duration,
			schedule,
			age,
		)
	}
	w.Flush()
}

func PrintBackupDetailed(backup neo4jv1alpha1.Neo4jBackup) {
	fmt.Printf("Name: %s\n", backup.Name)
	fmt.Printf("Namespace: %s\n", backup.Namespace)
	fmt.Printf("Created: %s\n", backup.CreationTimestamp.Format(time.RFC3339))
	fmt.Printf("Phase: %s\n", backup.Status.Phase)

	fmt.Printf("\nTarget:\n")
	fmt.Printf("  Type: %s\n", backup.Spec.Target.Kind)
	fmt.Printf("  Name: %s\n", backup.Spec.Target.Name)

	fmt.Printf("\nStorage:\n")
	fmt.Printf("  Type: %s\n", backup.Spec.Storage.Type)
	if backup.Spec.Storage.Bucket != "" {
		fmt.Printf("  Bucket: %s\n", backup.Spec.Storage.Bucket)
	}
	if backup.Spec.Storage.Path != "" {
		fmt.Printf("  Path: %s\n", backup.Spec.Storage.Path)
	}

	if backup.Spec.Schedule != "" {
		fmt.Printf("\nSchedule: %s\n", backup.Spec.Schedule)
	}

	if backup.Spec.Options != nil {
		fmt.Printf("\nOptions:\n")
		fmt.Printf("  Compress: %t\n", backup.Spec.Options.Compress)
		fmt.Printf("  Verify: %t\n", backup.Spec.Options.Verify)
	}

	if backup.Status.Stats != nil {
		fmt.Printf("\nStatistics:\n")
		if backup.Status.Stats.Size != "" {
			fmt.Printf("  Size: %s\n", backup.Status.Stats.Size)
		}
		if backup.Status.Stats.Duration != "" {
			fmt.Printf("  Duration: %s\n", backup.Status.Stats.Duration)
		}
		if backup.Status.Stats.Throughput != "" {
			fmt.Printf("  Throughput: %s\n", backup.Status.Stats.Throughput)
		}
	}
}

func PrintBackupStatus(backup neo4jv1alpha1.Neo4jBackup) {
	fmt.Printf("Backup: %s\n", backup.Name)
	fmt.Printf("Phase: %s\n", backup.Status.Phase)
	fmt.Printf("Target: %s (%s)\n", backup.Spec.Target.Name, backup.Spec.Target.Kind)

	if backup.Status.LastRunTime != nil {
		fmt.Printf("Started: %s\n", backup.Status.LastRunTime.Format(time.RFC3339))
	}

	if backup.Status.LastSuccessTime != nil {
		fmt.Printf("Completed: %s\n", backup.Status.LastSuccessTime.Format(time.RFC3339))
	}

	if backup.Status.Stats != nil {
		fmt.Printf("Statistics:\n")
		if backup.Status.Stats.Size != "" {
			fmt.Printf("  Data Size: %s\n", backup.Status.Stats.Size)
		}
		if backup.Status.Stats.Duration != "" {
			fmt.Printf("  Duration: %s\n", backup.Status.Stats.Duration)
		}
		if backup.Status.Stats.Throughput != "" {
			fmt.Printf("  Throughput: %s\n", backup.Status.Stats.Throughput)
		}
	}
}

func PrintBackupsJSON(backups []neo4jv1alpha1.Neo4jBackup) {
	data, _ := json.MarshalIndent(backups, "", "  ")
	fmt.Println(string(data))
}

func PrintBackupJSON(backup neo4jv1alpha1.Neo4jBackup) {
	data, _ := json.MarshalIndent(backup, "", "  ")
	fmt.Println(string(data))
}

func PrintBackupsYAML(backups []neo4jv1alpha1.Neo4jBackup) {
	data, _ := yaml.Marshal(backups)
	fmt.Println(string(data))
}

func PrintBackupYAML(backup neo4jv1alpha1.Neo4jBackup) {
	data, _ := yaml.Marshal(backup)
	fmt.Println(string(data))
}

// Plugin printing functions
func PrintPlugins(plugins []neo4jv1alpha1.Neo4jPlugin) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPLUGIN\tVERSION\tCLUSTER\tPHASE\tAGE")

	for _, plugin := range plugins {
		age := time.Since(plugin.CreationTimestamp.Time).Round(time.Second)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			plugin.Name,
			plugin.Spec.Name,
			plugin.Spec.Version,
			plugin.Spec.ClusterRef,
			plugin.Status.Phase,
			age,
		)
	}
	w.Flush()
}

func PrintPluginsWide(plugins []neo4jv1alpha1.Neo4jPlugin) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPLUGIN\tVERSION\tCLUSTER\tPHASE\tSOURCE\tENABLED\tAGE")

	for _, plugin := range plugins {
		age := time.Since(plugin.CreationTimestamp.Time).Round(time.Second)
		source := ""
		if plugin.Spec.Source != nil {
			source = plugin.Spec.Source.Type
		}
		enabled := "true"
		if !plugin.Spec.Enabled {
			enabled = "false"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			plugin.Name,
			plugin.Spec.Name,
			plugin.Spec.Version,
			plugin.Spec.ClusterRef,
			plugin.Status.Phase,
			source,
			enabled,
			age,
		)
	}
	w.Flush()
}

func PrintPluginDetailed(plugin neo4jv1alpha1.Neo4jPlugin) {
	fmt.Printf("Name: %s\n", plugin.Name)
	fmt.Printf("Namespace: %s\n", plugin.Namespace)
	fmt.Printf("Created: %s\n", plugin.CreationTimestamp.Format(time.RFC3339))
	fmt.Printf("Phase: %s\n", plugin.Status.Phase)

	fmt.Printf("\nPlugin Details:\n")
	fmt.Printf("  Name: %s\n", plugin.Spec.Name)
	fmt.Printf("  Version: %s\n", plugin.Spec.Version)
	fmt.Printf("  Cluster: %s\n", plugin.Spec.ClusterRef)
	fmt.Printf("  Enabled: %t\n", plugin.Spec.Enabled)

	if plugin.Spec.Source != nil {
		fmt.Printf("\nSource:\n")
		fmt.Printf("  Type: %s\n", plugin.Spec.Source.Type)
		if plugin.Spec.Source.URL != "" {
			fmt.Printf("  URL: %s\n", plugin.Spec.Source.URL)
		}
	}

	if len(plugin.Spec.Config) > 0 {
		fmt.Printf("\nConfiguration:\n")
		for key, value := range plugin.Spec.Config {
			fmt.Printf("  %s: %s\n", key, value)
		}
	}

	if len(plugin.Spec.Dependencies) > 0 {
		fmt.Printf("\nDependencies:\n")
		for _, dep := range plugin.Spec.Dependencies {
			fmt.Printf("  %s: %s\n", dep.Name, dep.VersionConstraint)
		}
	}
}

func PrintPluginStatus(plugin neo4jv1alpha1.Neo4jPlugin) {
	fmt.Printf("Plugin: %s (%s)\n", plugin.Spec.Name, plugin.Name)
	fmt.Printf("Phase: %s\n", plugin.Status.Phase)
	fmt.Printf("Cluster: %s\n", plugin.Spec.ClusterRef)
	fmt.Printf("Version: %s\n", plugin.Spec.Version)
	fmt.Printf("Enabled: %t\n", plugin.Spec.Enabled)

	if plugin.Status.InstalledVersion != "" {
		fmt.Printf("Installed Version: %s\n", plugin.Status.InstalledVersion)
	}

	if len(plugin.Status.Conditions) > 0 {
		fmt.Printf("Status Conditions:\n")
		for _, condition := range plugin.Status.Conditions {
			status := "✓"
			if condition.Status != metav1.ConditionTrue {
				status = "✗"
			}
			fmt.Printf("  %s %s: %s\n", status, condition.Type, condition.Reason)
		}
	}
}

func PrintPluginsJSON(plugins []neo4jv1alpha1.Neo4jPlugin) {
	data, _ := json.MarshalIndent(plugins, "", "  ")
	fmt.Println(string(data))
}

func PrintPluginJSON(plugin neo4jv1alpha1.Neo4jPlugin) {
	data, _ := json.MarshalIndent(plugin, "", "  ")
	fmt.Println(string(data))
}

func PrintPluginsYAML(plugins []neo4jv1alpha1.Neo4jPlugin) {
	data, _ := yaml.Marshal(plugins)
	fmt.Println(string(data))
}

func PrintPluginYAML(plugin neo4jv1alpha1.Neo4jPlugin) {
	data, _ := yaml.Marshal(plugin)
	fmt.Println(string(data))
}

// Wait functions
func WaitForClusterReady(ctx context.Context, client client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
		if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &cluster); err != nil {
			return false, err
		}

		for _, condition := range cluster.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func WaitForClusterDeleted(ctx context.Context, client client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
		err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &cluster)
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
}

func WaitForBackupComplete(ctx context.Context, client client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(10*time.Second, timeout, func() (bool, error) {
		var backup neo4jv1alpha1.Neo4jBackup
		if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &backup); err != nil {
			return false, err
		}

		return backup.Status.Phase == "Completed" || backup.Status.Phase == "Failed", nil
	})
}

func WaitForPluginReady(ctx context.Context, client client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		var plugin neo4jv1alpha1.Neo4jPlugin
		if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &plugin); err != nil {
			return false, err
		}

		for _, condition := range plugin.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func WaitForPluginDeleted(ctx context.Context, client client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		var plugin neo4jv1alpha1.Neo4jPlugin
		err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &plugin)
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
}

// Health check functions
func CheckClusterHealth(ctx context.Context, crClient client.Client, k8sClient *kubernetes.Clientset, clusterName, namespace string, detailed bool) error {
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

	fmt.Printf("Cluster Health Check: %s\n", clusterName)
	fmt.Printf("====================%s\n", strings.Repeat("=", len(clusterName)))

	// Overall status
	overallHealth := "✓ Healthy"
	if cluster.Status.Phase != "Ready" {
		overallHealth = "✗ Unhealthy"
	}
	fmt.Printf("Overall Status: %s\n", overallHealth)
	fmt.Printf("Phase: %s\n", cluster.Status.Phase)

	// Replica status
	if cluster.Status.Replicas != nil {
		fmt.Printf("\nReplica Status:\n")
		fmt.Printf("  Primaries: %d/%d\n", cluster.Status.Replicas.Primaries, cluster.Spec.Topology.Primaries)
		fmt.Printf("  Secondaries: %d/%d\n", cluster.Status.Replicas.Secondaries, cluster.Spec.Topology.Secondaries)
		fmt.Printf("  Ready: %d\n", cluster.Status.Replicas.Ready)
	}

	// Conditions
	if len(cluster.Status.Conditions) > 0 {
		fmt.Printf("\nConditions:\n")
		for _, condition := range cluster.Status.Conditions {
			status := "✓"
			if condition.Status != metav1.ConditionTrue {
				status = "✗"
			}
			fmt.Printf("  %s %s: %s\n", status, condition.Type, condition.Reason)
			if detailed && condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
		}
	}

	// Endpoints
	if cluster.Status.Endpoints != nil {
		fmt.Printf("\nEndpoints:\n")
		if cluster.Status.Endpoints.Bolt != "" {
			fmt.Printf("  Bolt: %s\n", cluster.Status.Endpoints.Bolt)
		}
		if cluster.Status.Endpoints.HTTP != "" {
			fmt.Printf("  HTTP: %s\n", cluster.Status.Endpoints.HTTP)
		}
		if cluster.Status.Endpoints.HTTPS != "" {
			fmt.Printf("  HTTPS: %s\n", cluster.Status.Endpoints.HTTPS)
		}
	}

	if detailed {
		// Get pod status
		fmt.Printf("\nPod Status:\n")
		pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", clusterName),
		})
		if err != nil {
			fmt.Printf("  Error getting pods: %v\n", err)
		} else {
			for _, pod := range pods.Items {
				status := "✓ Running"
				if pod.Status.Phase != "Running" {
					status = fmt.Sprintf("✗ %s", pod.Status.Phase)
				}
				fmt.Printf("  %s: %s\n", pod.Name, status)
			}
		}
	}

	return nil
}

// Logs functions
func GetClusterLogs(ctx context.Context, k8sClient *kubernetes.Clientset, clusterName, namespace, node, container string, follow bool, tail int64) error {
	// Implementation would get logs from pods
	// This is a simplified version
	fmt.Printf("Getting logs for cluster %s...\n", clusterName)

	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", clusterName),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("No pods found for cluster %s\n", clusterName)
		return nil
	}

	// Filter by node if specified
	var targetPods []string
	for _, pod := range pods.Items {
		if node == "" || pod.Name == node {
			targetPods = append(targetPods, pod.Name)
		}
	}

	if len(targetPods) == 0 {
		fmt.Printf("No matching pods found\n")
		return nil
	}

	// For simplicity, just show which pods we would get logs from
	fmt.Printf("Would get logs from pods:\n")
	for _, podName := range targetPods {
		fmt.Printf("  - %s\n", podName)
	}

	return nil
}

// User printing functions
func PrintUsers(users []neo4jv1alpha1.Neo4jUser) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tUSERNAME\tCLUSTER\tROLES\tAGE")

	for _, user := range users {
		age := time.Since(user.CreationTimestamp.Time).Round(time.Second)
		roles := strings.Join(user.Spec.Roles, ",")

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			user.Name,
			user.Spec.Username,
			user.Spec.ClusterRef,
			roles,
			age,
		)
	}
	w.Flush()
}

func PrintUsersWide(users []neo4jv1alpha1.Neo4jUser) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tUSERNAME\tCLUSTER\tROLES\tPASSWORD-SECRET\tAGE")

	for _, user := range users {
		age := time.Since(user.CreationTimestamp.Time).Round(time.Second)
		roles := strings.Join(user.Spec.Roles, ",")

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			user.Name,
			user.Spec.Username,
			user.Spec.ClusterRef,
			roles,
			user.Spec.PasswordSecret,
			age,
		)
	}
	w.Flush()
}

func PrintUserDetailed(user neo4jv1alpha1.Neo4jUser) {
	fmt.Printf("Name: %s\n", user.Name)
	fmt.Printf("Namespace: %s\n", user.Namespace)
	fmt.Printf("Created: %s\n", user.CreationTimestamp.Format(time.RFC3339))

	fmt.Printf("\nUser Details:\n")
	fmt.Printf("  Username: %s\n", user.Spec.Username)
	fmt.Printf("  Cluster: %s\n", user.Spec.ClusterRef)
	fmt.Printf("  Password Secret: %s\n", user.Spec.PasswordSecret)

	if len(user.Spec.Roles) > 0 {
		fmt.Printf("  Roles:\n")
		for _, role := range user.Spec.Roles {
			fmt.Printf("    - %s\n", role)
		}
	}
}

func PrintUsersJSON(users []neo4jv1alpha1.Neo4jUser) {
	data, _ := json.MarshalIndent(users, "", "  ")
	fmt.Println(string(data))
}

func PrintUserJSON(user neo4jv1alpha1.Neo4jUser) {
	data, _ := json.MarshalIndent(user, "", "  ")
	fmt.Println(string(data))
}

func PrintUsersYAML(users []neo4jv1alpha1.Neo4jUser) {
	data, _ := yaml.Marshal(users)
	fmt.Println(string(data))
}

func PrintUserYAML(user neo4jv1alpha1.Neo4jUser) {
	data, _ := yaml.Marshal(user)
	fmt.Println(string(data))
}

// Restore printing functions
func PrintRestores(restores []neo4jv1alpha1.Neo4jRestore) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTARGET\tSOURCE\tDATABASE\tPHASE\tAGE")

	for _, restore := range restores {
		age := time.Since(restore.CreationTimestamp.Time).Round(time.Second)

		source := restore.Spec.Source.Type
		if restore.Spec.Source.BackupRef != "" {
			source = restore.Spec.Source.BackupRef
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			restore.Name,
			restore.Spec.TargetCluster,
			source,
			restore.Spec.DatabaseName,
			restore.Status.Phase,
			age,
		)
	}
	w.Flush()
}

func PrintRestoresWide(restores []neo4jv1alpha1.Neo4jRestore) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTARGET\tSOURCE\tDATABASE\tPHASE\tSTARTED\tDURATION\tAGE")

	for _, restore := range restores {
		age := time.Since(restore.CreationTimestamp.Time).Round(time.Second)

		source := restore.Spec.Source.Type
		if restore.Spec.Source.BackupRef != "" {
			source = restore.Spec.Source.BackupRef
		}

		started := ""
		if restore.Status.StartTime != nil {
			started = restore.Status.StartTime.Format("15:04:05")
		}

		duration := ""
		if restore.Status.Stats != nil {
			duration = restore.Status.Stats.Duration
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			restore.Name,
			restore.Spec.TargetCluster,
			source,
			restore.Spec.DatabaseName,
			restore.Status.Phase,
			started,
			duration,
			age,
		)
	}
	w.Flush()
}

func PrintRestoreDetailed(restore neo4jv1alpha1.Neo4jRestore) {
	fmt.Printf("Name: %s\n", restore.Name)
	fmt.Printf("Namespace: %s\n", restore.Namespace)
	fmt.Printf("Created: %s\n", restore.CreationTimestamp.Format(time.RFC3339))
	fmt.Printf("Phase: %s\n", restore.Status.Phase)

	fmt.Printf("\nTarget:\n")
	fmt.Printf("  Cluster: %s\n", restore.Spec.TargetCluster)
	fmt.Printf("  Database: %s\n", restore.Spec.DatabaseName)

	fmt.Printf("\nSource:\n")
	fmt.Printf("  Type: %s\n", restore.Spec.Source.Type)
	if restore.Spec.Source.BackupRef != "" {
		fmt.Printf("  Backup: %s\n", restore.Spec.Source.BackupRef)
	}

	if restore.Spec.Options != nil {
		fmt.Printf("\nOptions:\n")
		fmt.Printf("  Replace Existing: %t\n", restore.Spec.Options.ReplaceExisting)
		fmt.Printf("  Verify Backup: %t\n", restore.Spec.Options.VerifyBackup)
	}

	if restore.Status.Stats != nil {
		fmt.Printf("\nStatistics:\n")
		if restore.Status.Stats.Duration != "" {
			fmt.Printf("  Duration: %s\n", restore.Status.Stats.Duration)
		}
		if restore.Status.Stats.DataSize != "" {
			fmt.Printf("  Data Size: %s\n", restore.Status.Stats.DataSize)
		}
		if restore.Status.Stats.Throughput != "" {
			fmt.Printf("  Throughput: %s\n", restore.Status.Stats.Throughput)
		}
	}
}

func PrintRestoreStatus(restore neo4jv1alpha1.Neo4jRestore) {
	fmt.Printf("Restore: %s\n", restore.Name)
	fmt.Printf("Phase: %s\n", restore.Status.Phase)
	fmt.Printf("Target: %s\n", restore.Spec.TargetCluster)
	fmt.Printf("Database: %s\n", restore.Spec.DatabaseName)

	if restore.Status.StartTime != nil {
		fmt.Printf("Started: %s\n", restore.Status.StartTime.Format(time.RFC3339))
	}

	if restore.Status.CompletionTime != nil {
		fmt.Printf("Completed: %s\n", restore.Status.CompletionTime.Format(time.RFC3339))
	}

	if restore.Status.Stats != nil {
		fmt.Printf("Statistics:\n")
		if restore.Status.Stats.Duration != "" {
			fmt.Printf("  Duration: %s\n", restore.Status.Stats.Duration)
		}
		if restore.Status.Stats.DataSize != "" {
			fmt.Printf("  Data Size: %s\n", restore.Status.Stats.DataSize)
		}
	}
}

func PrintRestoresJSON(restores []neo4jv1alpha1.Neo4jRestore) {
	data, _ := json.MarshalIndent(restores, "", "  ")
	fmt.Println(string(data))
}

func PrintRestoreJSON(restore neo4jv1alpha1.Neo4jRestore) {
	data, _ := json.MarshalIndent(restore, "", "  ")
	fmt.Println(string(data))
}

func PrintRestoresYAML(restores []neo4jv1alpha1.Neo4jRestore) {
	data, _ := yaml.Marshal(restores)
	fmt.Println(string(data))
}

func PrintRestoreYAML(restore neo4jv1alpha1.Neo4jRestore) {
	data, _ := yaml.Marshal(restore)
	fmt.Println(string(data))
}

func WaitForRestoreComplete(ctx context.Context, client client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(10*time.Second, timeout, func() (bool, error) {
		var restore neo4jv1alpha1.Neo4jRestore
		if err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &restore); err != nil {
			return false, err
		}

		return restore.Status.Phase == "Completed" || restore.Status.Phase == "Failed", nil
	})
}
