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
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/cmd/kubectl-neo4j/pkg/util"
)

// NewClusterCommand creates the cluster command with subcommands
func NewClusterCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Neo4j Enterprise clusters",
		Long:  "Manage Neo4j Enterprise clusters including creation, scaling, upgrades, and health monitoring.",
	}

	cmd.AddCommand(newClusterListCommand(configFlags))
	cmd.AddCommand(newClusterGetCommand(configFlags))
	cmd.AddCommand(newClusterCreateCommand(configFlags))
	cmd.AddCommand(newClusterDeleteCommand(configFlags))
	cmd.AddCommand(newClusterScaleCommand(configFlags))
	cmd.AddCommand(newClusterUpgradeCommand(configFlags))
	cmd.AddCommand(newClusterHealthCommand(configFlags))
	cmd.AddCommand(newClusterStatusCommand(configFlags))
	cmd.AddCommand(newClusterLogsCommand(configFlags))

	return cmd
}

func newClusterListCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var allNamespaces bool
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Neo4j Enterprise clusters",
		Long:  "List all Neo4j Enterprise clusters in the current or specified namespace.",
		Example: `  # List clusters in current namespace
  kubectl neo4j cluster list

  # List clusters in all namespaces
  kubectl neo4j cluster list --all-namespaces

  # List clusters with detailed output
  kubectl neo4j cluster list -o wide`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			namespace := ""
			if !allNamespaces {
				if configFlags.Namespace != nil && *configFlags.Namespace != "" {
					namespace = *configFlags.Namespace
				} else {
					namespace = "default"
				}
			}

			var clusterList neo4jv1alpha1.Neo4jEnterpriseClusterList
			listOpts := []client.ListOption{}
			if namespace != "" {
				listOpts = append(listOpts, client.InNamespace(namespace))
			}

			if err := crClient.List(ctx, &clusterList, listOpts...); err != nil {
				return fmt.Errorf("failed to list clusters: %w", err)
			}

			if len(clusterList.Items) == 0 {
				fmt.Println("No Neo4j Enterprise clusters found.")
				return nil
			}

			switch outputFormat {
			case "wide":
				util.PrintClustersWide(clusterList.Items)
			case "json":
				util.PrintClustersJSON(clusterList.Items)
			case "yaml":
				util.PrintClustersYAML(clusterList.Items)
			default:
				util.PrintClusters(clusterList.Items)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List clusters across all namespaces")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (wide|json|yaml)")

	return cmd
}

func newClusterGetCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get <cluster-name>",
		Short: "Get detailed information about a Neo4j cluster",
		Long:  "Get detailed information about a specific Neo4j Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Get cluster details
  kubectl neo4j cluster get production

  # Get cluster details in YAML format
  kubectl neo4j cluster get production -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

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

			switch outputFormat {
			case "json":
				util.PrintClusterJSON(cluster)
			case "yaml":
				util.PrintClusterYAML(cluster)
			default:
				util.PrintClusterDetailed(cluster)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json|yaml)")

	return cmd
}

func newClusterCreateCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		primaries       int32
		secondaries     int32
		image           string
		storageSize     string
		storageClass    string
		enableTLS       bool
		enableAutoScale bool
		enableBackups   bool
		dryRun          bool
		wait            bool
		timeout         time.Duration
	)

	cmd := &cobra.Command{
		Use:   "create <cluster-name>",
		Short: "Create a new Neo4j Enterprise cluster",
		Long:  "Create a new Neo4j Enterprise cluster with the specified configuration.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Create a basic 3-node cluster
  kubectl neo4j cluster create production --primaries=3

  # Create a cluster with read replicas and auto-scaling
  kubectl neo4j cluster create production --primaries=3 --secondaries=2 --enable-autoscale

  # Create a cluster with custom storage
  kubectl neo4j cluster create production --primaries=3 --storage-size=100Gi --storage-class=fast-ssd

  # Dry run to see what would be created
  kubectl neo4j cluster create production --primaries=3 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Validate cluster name and parameters
			if strings.TrimSpace(clusterName) == "" {
				return fmt.Errorf("cluster name cannot be empty")
			}
			if primaries < 1 || primaries > 7 {
				return fmt.Errorf("primaries must be between 1 and 7, got %d", primaries)
			}
			if secondaries < 0 || secondaries > 20 {
				return fmt.Errorf("secondaries must be between 0 and 20, got %d", secondaries)
			}
			if primaries%2 == 0 {
				fmt.Printf("Warning: Even number of primaries (%d) may affect quorum consensus\n", primaries)
			}
			if strings.TrimSpace(image) == "" {
				return fmt.Errorf("image cannot be empty")
			}

			// Build cluster specification
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  image,
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   primaries,
						Secondaries: secondaries,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      storageSize,
						ClassName: storageClass,
					},
				},
			}

			// Add optional configurations
			if enableTLS {
				cluster.Spec.TLS = &neo4jv1alpha1.TLSSpec{
					Mode: "cert-manager",
				}
			}

			if enableAutoScale {
				cluster.Spec.AutoScaling = &neo4jv1alpha1.AutoScalingSpec{
					Enabled: true,
					Primaries: &neo4jv1alpha1.PrimaryAutoScalingConfig{
						Enabled:     true,
						MinReplicas: primaries,
						MaxReplicas: 7,
					},
					Secondaries: &neo4jv1alpha1.SecondaryAutoScalingConfig{
						Enabled:     true,
						MinReplicas: secondaries,
						MaxReplicas: 20,
					},
				}
			}

			if enableBackups {
				cluster.Spec.Backups = &neo4jv1alpha1.BackupsSpec{
					DefaultStorage: &neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Size:             "10Gi",
							StorageClassName: "standard",
						},
					},
				}
			}

			if dryRun {
				fmt.Printf("Would create cluster:\n")
				util.PrintClusterYAML(*cluster)
				return nil
			}

			fmt.Printf("Creating Neo4j Enterprise cluster %s...\n", clusterName)
			if err := crClient.Create(ctx, cluster); err != nil {
				return fmt.Errorf("failed to create cluster: %w", err)
			}

			fmt.Printf("Cluster %s created successfully.\n", clusterName)

			if wait {
				fmt.Printf("Waiting for cluster to be ready (timeout: %v)...\n", timeout)
				return util.WaitForClusterReady(ctx, crClient, clusterName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().Int32Var(&primaries, "primaries", 3, "Number of primary nodes (1-7, odd numbers recommended)")
	cmd.Flags().Int32Var(&secondaries, "secondaries", 0, "Number of secondary nodes (0-20)")
	cmd.Flags().StringVar(&image, "image", "5.26-enterprise", "Neo4j Docker image tag")
	cmd.Flags().StringVar(&storageSize, "storage-size", "10Gi", "Storage size per node")
	cmd.Flags().StringVar(&storageClass, "storage-class", "", "Storage class name")
	cmd.Flags().BoolVar(&enableTLS, "enable-tls", false, "Enable TLS encryption")
	cmd.Flags().BoolVar(&enableAutoScale, "enable-autoscale", false, "Enable auto-scaling")
	cmd.Flags().BoolVar(&enableBackups, "enable-backups", false, "Enable scheduled backups")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be created without actually creating")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for cluster to be ready")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Timeout for waiting")

	return cmd
}

func newClusterDeleteCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var force bool
	var wait bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "delete <cluster-name>",
		Short: "Delete a Neo4j Enterprise cluster",
		Long:  "Delete a Neo4j Enterprise cluster and all associated resources.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Delete a cluster
  kubectl neo4j cluster delete production

  # Force delete without confirmation
  kubectl neo4j cluster delete production --force

  # Delete and wait for completion
  kubectl neo4j cluster delete production --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Check if cluster exists
			var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      clusterName,
				Namespace: namespace,
			}, &cluster); err != nil {
				if errors.IsNotFound(err) {
					fmt.Printf("Cluster %s not found in namespace %s\n", clusterName, namespace)
					return nil
				}
				return fmt.Errorf("failed to get cluster: %w", err)
			}

			// Confirmation prompt
			if !force {
				fmt.Printf("Are you sure you want to delete cluster %s? This action cannot be undone.\n", clusterName)
				fmt.Print("Type 'yes' to confirm: ")
				var response string
				fmt.Scanln(&response)
				if strings.ToLower(response) != "yes" {
					fmt.Println("Operation cancelled.")
					return nil
				}
			}

			fmt.Printf("Deleting Neo4j Enterprise cluster %s...\n", clusterName)
			if err := crClient.Delete(ctx, &cluster); err != nil {
				return fmt.Errorf("failed to delete cluster: %w", err)
			}

			fmt.Printf("Cluster %s deletion initiated.\n", clusterName)

			if wait {
				fmt.Printf("Waiting for cluster deletion to complete (timeout: %v)...\n", timeout)
				return util.WaitForClusterDeleted(ctx, crClient, clusterName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for deletion to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for waiting")

	return cmd
}

func newClusterScaleCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var primaries int32
	var secondaries int32
	var wait bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "scale <cluster-name>",
		Short: "Scale a Neo4j Enterprise cluster",
		Long:  "Scale the number of primary and secondary nodes in a Neo4j Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Scale to 5 primaries and 3 secondaries
  kubectl neo4j cluster scale production --primaries=5 --secondaries=3

  # Scale only secondaries
  kubectl neo4j cluster scale production --secondaries=5

  # Scale and wait for completion
  kubectl neo4j cluster scale production --primaries=5 --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Get current cluster
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

			// Update topology
			updated := false
			if cmd.Flags().Changed("primaries") {
				if primaries%2 == 0 {
					fmt.Printf("Warning: Even number of primaries (%d) may affect quorum. Consider using odd numbers.\n", primaries)
				}
				cluster.Spec.Topology.Primaries = primaries
				updated = true
			}
			if cmd.Flags().Changed("secondaries") {
				cluster.Spec.Topology.Secondaries = secondaries
				updated = true
			}

			if !updated {
				return fmt.Errorf("no scaling parameters provided")
			}

			fmt.Printf("Scaling cluster %s to %d primaries and %d secondaries...\n",
				clusterName, cluster.Spec.Topology.Primaries, cluster.Spec.Topology.Secondaries)

			if err := crClient.Update(ctx, &cluster); err != nil {
				return fmt.Errorf("failed to update cluster: %w", err)
			}

			fmt.Printf("Cluster %s scaling initiated.\n", clusterName)

			if wait {
				fmt.Printf("Waiting for scaling to complete (timeout: %v)...\n", timeout)
				return util.WaitForClusterReady(ctx, crClient, clusterName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().Int32Var(&primaries, "primaries", 0, "Number of primary nodes")
	cmd.Flags().Int32Var(&secondaries, "secondaries", 0, "Number of secondary nodes")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for scaling to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Timeout for waiting")

	return cmd
}

func newClusterUpgradeCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var imageTag string
	var strategy string
	var wait bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "upgrade <cluster-name>",
		Short: "Upgrade a Neo4j Enterprise cluster",
		Long:  "Upgrade a Neo4j Enterprise cluster to a new version with zero downtime.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Upgrade to Neo4j 5.27
  kubectl neo4j cluster upgrade production --image=5.27-enterprise

  # Upgrade with recreate strategy
  kubectl neo4j cluster upgrade production --image=5.27-enterprise --strategy=Recreate

  # Upgrade and wait for completion
  kubectl neo4j cluster upgrade production --image=5.27-enterprise --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Get current cluster
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

			currentVersion := cluster.Spec.Image.Tag
			fmt.Printf("Upgrading cluster %s from %s to %s...\n", clusterName, currentVersion, imageTag)

			// Update image
			cluster.Spec.Image.Tag = imageTag

			// Set upgrade strategy if specified
			if strategy != "" {
				if cluster.Spec.UpgradeStrategy == nil {
					cluster.Spec.UpgradeStrategy = &neo4jv1alpha1.UpgradeStrategySpec{}
				}
				cluster.Spec.UpgradeStrategy.Strategy = strategy
			}

			if err := crClient.Update(ctx, &cluster); err != nil {
				return fmt.Errorf("failed to update cluster: %w", err)
			}

			fmt.Printf("Cluster %s upgrade initiated.\n", clusterName)

			if wait {
				fmt.Printf("Waiting for upgrade to complete (timeout: %v)...\n", timeout)
				return util.WaitForClusterReady(ctx, crClient, clusterName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&imageTag, "image", "", "Neo4j Docker image tag (required)")
	cmd.Flags().StringVar(&strategy, "strategy", "RollingUpgrade", "Upgrade strategy (RollingUpgrade|Recreate)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for upgrade to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "Timeout for waiting")
	cmd.MarkFlagRequired("image")

	return cmd
}

func newClusterHealthCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var detailed bool

	cmd := &cobra.Command{
		Use:   "health <cluster-name>",
		Short: "Check the health of a Neo4j cluster",
		Long:  "Check the health status of a Neo4j Enterprise cluster and its components.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Check cluster health
  kubectl neo4j cluster health production

  # Check detailed health information
  kubectl neo4j cluster health production --detailed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)
			k8sClient := ctx.Value("k8sClient").(*kubernetes.Clientset)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

			return util.CheckClusterHealth(ctx, crClient, k8sClient, clusterName, namespace, detailed)
		},
	}

	cmd.Flags().BoolVar(&detailed, "detailed", false, "Show detailed health information")

	return cmd
}

func newClusterStatusCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <cluster-name>",
		Short: "Show the status of a Neo4j cluster",
		Long:  "Show detailed status information about a Neo4j Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Show cluster status
  kubectl neo4j cluster status production`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

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

			util.PrintClusterStatus(cluster)
			return nil
		},
	}

	return cmd
}

func newClusterLogsCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var follow bool
	var tail int64
	var container string
	var node string

	cmd := &cobra.Command{
		Use:   "logs <cluster-name>",
		Short: "Get logs from Neo4j cluster nodes",
		Long:  "Get logs from Neo4j cluster nodes with various filtering options.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Get logs from all nodes
  kubectl neo4j cluster logs production

  # Follow logs from a specific node
  kubectl neo4j cluster logs production --node=production-0 --follow

  # Get last 100 lines from all nodes
  kubectl neo4j cluster logs production --tail=100`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			k8sClient := ctx.Value("k8sClient").(*kubernetes.Clientset)

			clusterName := args[0]
			namespace := util.GetNamespace(configFlags)

			return util.GetClusterLogs(ctx, k8sClient, clusterName, namespace, node, container, follow, tail)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().Int64Var(&tail, "tail", -1, "Number of lines to show from the end of the logs")
	cmd.Flags().StringVar(&container, "container", "", "Container name (default: neo4j)")
	cmd.Flags().StringVar(&node, "node", "", "Specific node to get logs from")

	return cmd
}
