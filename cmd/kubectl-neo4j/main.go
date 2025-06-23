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

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/cmd/kubectl-neo4j/pkg/cmd"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	ctx := context.Background()

	// Initialize Kubernetes client configuration
	configFlags := genericclioptions.NewConfigFlags(true)

	// Create the root command
	rootCmd := &cobra.Command{
		Use:   "kubectl-neo4j",
		Short: "kubectl plugin for managing Neo4j Enterprise clusters",
		Long: `kubectl-neo4j is a kubectl plugin for managing Neo4j Enterprise clusters deployed with the Neo4j Enterprise Operator.

This plugin provides convenient commands for:
- Managing Neo4j Enterprise clusters
- Backup and restore operations
- User and security management
- Plugin lifecycle management
- Monitoring and troubleshooting
- Performance optimization

Examples:
  # List all Neo4j clusters
  kubectl neo4j cluster list

  # Create a backup
  kubectl neo4j backup create my-backup --cluster=production --storage=s3

  # Scale a cluster
  kubectl neo4j cluster scale production --primaries=5 --secondaries=3

  # Install a plugin
  kubectl neo4j plugin install apoc --cluster=production --version=5.26.0

  # Check cluster health
  kubectl neo4j cluster health production

For more information, visit: https://github.com/neo4j-labs/neo4j-kubernetes-operator`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize Kubernetes clients
			restConfig, err := configFlags.ToRESTConfig()
			if err != nil {
				return fmt.Errorf("failed to create REST config: %w", err)
			}

			k8sClient, err := kubernetes.NewForConfig(restConfig)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create controller-runtime client for CRDs
			scheme := cmd.Context().Value("scheme")
			if scheme == nil {
				return fmt.Errorf("scheme not found in context")
			}

			crClient, err := client.New(restConfig, client.Options{
				Scheme: scheme.(*runtime.Scheme),
			})
			if err != nil {
				return fmt.Errorf("failed to create controller-runtime client: %w", err)
			}

			// Store clients in command context
			cmd.SetContext(context.WithValue(cmd.Context(), "k8sClient", k8sClient))
			cmd.SetContext(context.WithValue(cmd.Context(), "crClient", crClient))
			cmd.SetContext(context.WithValue(cmd.Context(), "restConfig", restConfig))

			return nil
		},
		SilenceUsage: true,
	}

	// Add global flags
	configFlags.AddFlags(rootCmd.PersistentFlags())

	// Add version flag
	rootCmd.PersistentFlags().Bool("version", false, "Print version information")

	// Initialize scheme with Neo4j CRDs
	scheme := runtime.NewScheme()
	if err := neo4jv1alpha1.AddToScheme(scheme); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to add Neo4j CRDs to scheme: %v\n", err)
		os.Exit(1)
	}
	rootCmd.SetContext(context.WithValue(ctx, "scheme", scheme))

	// Add subcommands
	rootCmd.AddCommand(cmd.NewClusterCommand(configFlags))
	rootCmd.AddCommand(cmd.NewBackupCommand(configFlags))
	rootCmd.AddCommand(cmd.NewRestoreCommand(configFlags))
	rootCmd.AddCommand(cmd.NewUserCommand(configFlags))
	rootCmd.AddCommand(cmd.NewPluginCommand(configFlags))
	rootCmd.AddCommand(cmd.NewMonitorCommand(configFlags))
	rootCmd.AddCommand(cmd.NewTroubleshootCommand(configFlags))

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
