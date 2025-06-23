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
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/cmd/kubectl-neo4j/pkg/util"
)

// NewPluginCommand creates the plugin command with subcommands
func NewPluginCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage Neo4j cluster plugins",
		Long:  "Install, configure, and manage plugins for Neo4j Enterprise clusters.",
	}

	cmd.AddCommand(newPluginInstallCommand(configFlags))
	cmd.AddCommand(newPluginListCommand(configFlags))
	cmd.AddCommand(newPluginGetCommand(configFlags))
	cmd.AddCommand(newPluginUninstallCommand(configFlags))
	cmd.AddCommand(newPluginStatusCommand(configFlags))
	cmd.AddCommand(newPluginUpdateCommand(configFlags))

	return cmd
}

func newPluginInstallCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
		version     string
		sourceType  string
		repository  string
		config      []string
		wait        bool
		timeout     time.Duration
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "install <plugin-name>",
		Short: "Install a plugin on a Neo4j cluster",
		Long:  "Install a Neo4j plugin on an Enterprise cluster with automatic dependency resolution.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Install APOC plugin
  kubectl neo4j plugin install apoc --cluster=production --version=5.26.0

  # Install Graph Data Science plugin
  kubectl neo4j plugin install graph-data-science --cluster=production --version=2.8.0

  # Install plugin with custom configuration
  kubectl neo4j plugin install apoc --cluster=production --version=5.26.0 --config=apoc.export.file.enabled=true --config=apoc.import.file.enabled=true

  # Install from custom repository
  kubectl neo4j plugin install my-plugin --cluster=production --version=1.0.0 --source=maven --repository=https://my-repo.com/maven2

  # Dry run to see what would be created
  kubectl neo4j plugin install apoc --cluster=production --version=5.26.0 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			pluginName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Validate cluster exists
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

			// Build plugin specification
			plugin := &neo4jv1alpha1.Neo4jPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%s", clusterName, pluginName),
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jPluginSpec{
					ClusterRef: clusterName,
					Name:       pluginName,
					Version:    version,
					Enabled:    true,
				},
			}

			// Configure source
			if sourceType != "" || repository != "" {
				plugin.Spec.Source = &neo4jv1alpha1.PluginSource{
					Type: sourceType,
				}
				if repository != "" {
					plugin.Spec.Source.URL = repository
				}
			} else {
				// Use default source for known plugins
				plugin.Spec.Source = &neo4jv1alpha1.PluginSource{
					Type: "official",
				}
			}

			// Parse configuration
			if len(config) > 0 {
				plugin.Spec.Config = make(map[string]string)
				for _, cfg := range config {
					parts := strings.SplitN(cfg, "=", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid config format: %s (expected key=value)", cfg)
					}
					plugin.Spec.Config[parts[0]] = parts[1]
				}
			}

			// Set default configurations for known plugins
			switch pluginName {
			case "apoc":
				if plugin.Spec.Config == nil {
					plugin.Spec.Config = make(map[string]string)
				}
				if _, exists := plugin.Spec.Config["apoc.export.file.enabled"]; !exists {
					plugin.Spec.Config["apoc.export.file.enabled"] = "true"
				}
				if _, exists := plugin.Spec.Config["apoc.import.file.enabled"]; !exists {
					plugin.Spec.Config["apoc.import.file.enabled"] = "true"
				}
			case "graph-data-science":
				plugin.Spec.Dependencies = []neo4jv1alpha1.PluginDependency{
					{Name: "apoc", VersionConstraint: ">=5.26.0"},
				}
			}

			if dryRun {
				fmt.Printf("Would install plugin:\n")
				util.PrintPluginYAML(*plugin)
				return nil
			}

			fmt.Printf("Installing plugin %s version %s on cluster %s...\n", pluginName, version, clusterName)
			if err := crClient.Create(ctx, plugin); err != nil {
				return fmt.Errorf("failed to install plugin: %w", err)
			}

			fmt.Printf("Plugin %s installation initiated.\n", pluginName)

			if wait {
				fmt.Printf("Waiting for plugin to be ready (timeout: %v)...\n", timeout)
				return util.WaitForPluginReady(ctx, crClient, plugin.Name, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().StringVar(&version, "version", "", "Plugin version (required)")
	cmd.Flags().StringVar(&sourceType, "source", "", "Plugin source type (official|maven|url)")
	cmd.Flags().StringVar(&repository, "repository", "", "Custom repository URL")
	cmd.Flags().StringArrayVar(&config, "config", []string{}, "Plugin configuration (key=value)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for plugin to be ready")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for waiting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be created without creating")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("version")

	return cmd
}

func newPluginListCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var allNamespaces bool
	var outputFormat string
	var clusterName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Neo4j plugins",
		Long:  "List all Neo4j plugins in the current or specified namespace.",
		Example: `  # List all plugins
  kubectl neo4j plugin list

  # List plugins for a specific cluster
  kubectl neo4j plugin list --cluster=production

  # List plugins in all namespaces
  kubectl neo4j plugin list --all-namespaces`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			namespace := ""
			if !allNamespaces {
				namespace = util.GetNamespace(configFlags)
			}

			var pluginList neo4jv1alpha1.Neo4jPluginList
			listOpts := []client.ListOption{}
			if namespace != "" {
				listOpts = append(listOpts, client.InNamespace(namespace))
			}

			if err := crClient.List(ctx, &pluginList, listOpts...); err != nil {
				return fmt.Errorf("failed to list plugins: %w", err)
			}

			// Filter by cluster if specified
			var filteredPlugins []neo4jv1alpha1.Neo4jPlugin
			for _, plugin := range pluginList.Items {
				if clusterName == "" || plugin.Spec.ClusterRef == clusterName {
					filteredPlugins = append(filteredPlugins, plugin)
				}
			}

			if len(filteredPlugins) == 0 {
				if clusterName != "" {
					fmt.Printf("No plugins found for cluster %s.\n", clusterName)
				} else {
					fmt.Println("No plugins found.")
				}
				return nil
			}

			switch outputFormat {
			case "wide":
				util.PrintPluginsWide(filteredPlugins)
			case "json":
				util.PrintPluginsJSON(filteredPlugins)
			case "yaml":
				util.PrintPluginsYAML(filteredPlugins)
			default:
				util.PrintPlugins(filteredPlugins)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List plugins across all namespaces")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (wide|json|yaml)")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "Filter by cluster name")

	return cmd
}

func newPluginGetCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get <plugin-name>",
		Short: "Get detailed information about a plugin",
		Long:  "Get detailed information about a specific Neo4j plugin.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Get plugin details
  kubectl neo4j plugin get production-apoc

  # Get plugin details in YAML format
  kubectl neo4j plugin get production-apoc -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			pluginName := args[0]
			namespace := util.GetNamespace(configFlags)

			var plugin neo4jv1alpha1.Neo4jPlugin
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      pluginName,
				Namespace: namespace,
			}, &plugin); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("plugin %s not found in namespace %s", pluginName, namespace)
				}
				return fmt.Errorf("failed to get plugin: %w", err)
			}

			switch outputFormat {
			case "json":
				util.PrintPluginJSON(plugin)
			case "yaml":
				util.PrintPluginYAML(plugin)
			default:
				util.PrintPluginDetailed(plugin)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json|yaml)")

	return cmd
}

func newPluginUninstallCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var force bool
	var wait bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "uninstall <plugin-name>",
		Short: "Uninstall a plugin from a Neo4j cluster",
		Long:  "Uninstall a Neo4j plugin from an Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Uninstall a plugin
  kubectl neo4j plugin uninstall production-apoc

  # Force uninstall without confirmation
  kubectl neo4j plugin uninstall production-apoc --force

  # Uninstall and wait for completion
  kubectl neo4j plugin uninstall production-apoc --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			pluginName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Check if plugin exists
			var plugin neo4jv1alpha1.Neo4jPlugin
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      pluginName,
				Namespace: namespace,
			}, &plugin); err != nil {
				if errors.IsNotFound(err) {
					fmt.Printf("Plugin %s not found in namespace %s\n", pluginName, namespace)
					return nil
				}
				return fmt.Errorf("failed to get plugin: %w", err)
			}

			// Confirmation prompt
			if !force {
				fmt.Printf("Are you sure you want to uninstall plugin %s from cluster %s?\n", plugin.Spec.Name, plugin.Spec.ClusterRef)
				fmt.Print("Type 'yes' to confirm: ")
				var response string
				fmt.Scanln(&response)
				if strings.ToLower(response) != "yes" {
					fmt.Println("Operation cancelled.")
					return nil
				}
			}

			fmt.Printf("Uninstalling plugin %s from cluster %s...\n", plugin.Spec.Name, plugin.Spec.ClusterRef)
			if err := crClient.Delete(ctx, &plugin); err != nil {
				return fmt.Errorf("failed to uninstall plugin: %w", err)
			}

			fmt.Printf("Plugin %s uninstallation initiated.\n", plugin.Spec.Name)

			if wait {
				fmt.Printf("Waiting for plugin uninstallation to complete (timeout: %v)...\n", timeout)
				return util.WaitForPluginDeleted(ctx, crClient, pluginName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for uninstallation to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for waiting")

	return cmd
}

func newPluginStatusCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <plugin-name>",
		Short: "Show the status of a plugin",
		Long:  "Show detailed status information about a Neo4j plugin.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Show plugin status
  kubectl neo4j plugin status production-apoc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			pluginName := args[0]
			namespace := util.GetNamespace(configFlags)

			var plugin neo4jv1alpha1.Neo4jPlugin
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      pluginName,
				Namespace: namespace,
			}, &plugin); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("plugin %s not found in namespace %s", pluginName, namespace)
				}
				return fmt.Errorf("failed to get plugin: %w", err)
			}

			util.PrintPluginStatus(plugin)
			return nil
		},
	}

	return cmd
}

func newPluginUpdateCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		version string
		config  []string
		wait    bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "update <plugin-name>",
		Short: "Update a plugin version or configuration",
		Long:  "Update a Neo4j plugin to a new version or modify its configuration.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Update plugin version
  kubectl neo4j plugin update production-apoc --version=5.27.0

  # Update plugin configuration
  kubectl neo4j plugin update production-apoc --config=apoc.export.file.enabled=false

  # Update both version and configuration
  kubectl neo4j plugin update production-apoc --version=5.27.0 --config=apoc.export.file.enabled=true --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			pluginName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Get current plugin
			var plugin neo4jv1alpha1.Neo4jPlugin
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      pluginName,
				Namespace: namespace,
			}, &plugin); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("plugin %s not found in namespace %s", pluginName, namespace)
				}
				return fmt.Errorf("failed to get plugin: %w", err)
			}

			updated := false

			// Update version if specified
			if version != "" {
				fmt.Printf("Updating plugin %s from version %s to %s...\n", plugin.Spec.Name, plugin.Spec.Version, version)
				plugin.Spec.Version = version
				updated = true
			}

			// Update configuration if specified
			if len(config) > 0 {
				if plugin.Spec.Config == nil {
					plugin.Spec.Config = make(map[string]string)
				}
				for _, cfg := range config {
					parts := strings.SplitN(cfg, "=", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid config format: %s (expected key=value)", cfg)
					}
					plugin.Spec.Config[parts[0]] = parts[1]
					fmt.Printf("Setting configuration %s=%s\n", parts[0], parts[1])
				}
				updated = true
			}

			if !updated {
				return fmt.Errorf("no update parameters provided")
			}

			if err := crClient.Update(ctx, &plugin); err != nil {
				return fmt.Errorf("failed to update plugin: %w", err)
			}

			fmt.Printf("Plugin %s update initiated.\n", plugin.Spec.Name)

			if wait {
				fmt.Printf("Waiting for plugin update to complete (timeout: %v)...\n", timeout)
				return util.WaitForPluginReady(ctx, crClient, pluginName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "New plugin version")
	cmd.Flags().StringArrayVar(&config, "config", []string{}, "Plugin configuration updates (key=value)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for update to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for waiting")

	return cmd
}
