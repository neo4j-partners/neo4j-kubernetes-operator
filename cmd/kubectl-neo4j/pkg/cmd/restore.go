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

// NewRestoreCommand creates the restore command with subcommands
func NewRestoreCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Manage Neo4j cluster restores",
		Long:  "Restore Neo4j Enterprise clusters from backups.",
	}

	cmd.AddCommand(newRestoreCreateCommand(configFlags))
	cmd.AddCommand(newRestoreListCommand(configFlags))
	cmd.AddCommand(newRestoreGetCommand(configFlags))
	cmd.AddCommand(newRestoreDeleteCommand(configFlags))
	cmd.AddCommand(newRestoreStatusCommand(configFlags))

	return cmd
}

func newRestoreCreateCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
		backupName  string
		storageType string
		bucket      string
		path        string
		wait        bool
		timeout     time.Duration
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "create <restore-name>",
		Short: "Create a restore operation",
		Long:  "Create a restore operation to restore a Neo4j cluster from a backup.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Restore from a backup resource
  kubectl neo4j restore create my-restore --cluster=production --backup=my-backup

  # Restore from S3 storage
  kubectl neo4j restore create my-restore --cluster=production --storage=s3 --bucket=my-backups --path=/backup-2024-01-01

  # Restore and wait for completion
  kubectl neo4j restore create my-restore --cluster=production --backup=my-backup --wait

  # Dry run to see what would be created
  kubectl neo4j restore create my-restore --cluster=production --backup=my-backup --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			restoreName := args[0]
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

			// Build restore specification
			restore := &neo4jv1alpha1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      restoreName,
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jRestoreSpec{
					TargetCluster: clusterName,
					DatabaseName:  "neo4j", // Default database name
				},
			}

			// Configure source
			if backupName != "" {
				// Restore from backup resource
				restore.Spec.Source = neo4jv1alpha1.RestoreSource{
					Type:      "backup",
					BackupRef: backupName,
				}
			} else if storageType != "" {
				// Restore from storage
				switch storageType {
				case "s3":
					if bucket == "" {
						return fmt.Errorf("bucket is required for S3 storage")
					}
					restore.Spec.Source = neo4jv1alpha1.RestoreSource{
						Type: "storage",
						Storage: &neo4jv1alpha1.StorageLocation{
							Type:   "s3",
							Bucket: bucket,
							Path:   path,
							Cloud: &neo4jv1alpha1.CloudBlock{
								Provider: "aws",
								Identity: &neo4jv1alpha1.CloudIdentity{
									Provider: "aws",
								},
							},
						},
					}
				case "gcs":
					if bucket == "" {
						return fmt.Errorf("bucket is required for GCS storage")
					}
					restore.Spec.Source = neo4jv1alpha1.RestoreSource{
						Type: "storage",
						Storage: &neo4jv1alpha1.StorageLocation{
							Type:   "gcs",
							Bucket: bucket,
							Path:   path,
							Cloud: &neo4jv1alpha1.CloudBlock{
								Provider: "gcp",
								Identity: &neo4jv1alpha1.CloudIdentity{
									Provider: "gcp",
								},
							},
						},
					}
				case "azure":
					if bucket == "" {
						return fmt.Errorf("container is required for Azure storage")
					}
					restore.Spec.Source = neo4jv1alpha1.RestoreSource{
						Type: "storage",
						Storage: &neo4jv1alpha1.StorageLocation{
							Type:   "azure",
							Bucket: bucket,
							Path:   path,
							Cloud: &neo4jv1alpha1.CloudBlock{
								Provider: "azure",
								Identity: &neo4jv1alpha1.CloudIdentity{
									Provider: "azure",
								},
							},
						},
					}
				default:
					return fmt.Errorf("unsupported storage type: %s", storageType)
				}
			} else {
				return fmt.Errorf("either --backup or --storage must be specified")
			}

			if dryRun {
				fmt.Printf("Would create restore:\n")
				util.PrintRestoreYAML(*restore)
				return nil
			}

			fmt.Printf("Creating restore %s for cluster %s...\n", restoreName, clusterName)
			if err := crClient.Create(ctx, restore); err != nil {
				return fmt.Errorf("failed to create restore: %w", err)
			}

			fmt.Printf("Restore %s created successfully.\n", restoreName)

			if wait {
				fmt.Printf("Waiting for restore to complete (timeout: %v)...\n", timeout)
				return util.WaitForRestoreComplete(ctx, crClient, restoreName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().StringVar(&backupName, "backup", "", "Backup resource name")
	cmd.Flags().StringVar(&storageType, "storage", "", "Storage type (s3|gcs|azure)")
	cmd.Flags().StringVar(&bucket, "bucket", "", "Storage bucket/container name")
	cmd.Flags().StringVar(&path, "path", "", "Storage path")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for restore to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Minute, "Timeout for waiting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be created without creating")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

func newRestoreListCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var allNamespaces bool
	var outputFormat string
	var clusterName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Neo4j restores",
		Long:  "List all Neo4j restores in the current or specified namespace.",
		Example: `  # List all restores
  kubectl neo4j restore list

  # List restores for a specific cluster
  kubectl neo4j restore list --cluster=production

  # List restores in all namespaces
  kubectl neo4j restore list --all-namespaces`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			namespace := ""
			if !allNamespaces {
				namespace = util.GetNamespace(configFlags)
			}

			var restoreList neo4jv1alpha1.Neo4jRestoreList
			listOpts := []client.ListOption{}
			if namespace != "" {
				listOpts = append(listOpts, client.InNamespace(namespace))
			}

			if err := crClient.List(ctx, &restoreList, listOpts...); err != nil {
				return fmt.Errorf("failed to list restores: %w", err)
			}

			// Filter by cluster if specified
			var filteredRestores []neo4jv1alpha1.Neo4jRestore
			for _, restore := range restoreList.Items {
				if clusterName == "" || restore.Spec.TargetCluster == clusterName {
					filteredRestores = append(filteredRestores, restore)
				}
			}

			if len(filteredRestores) == 0 {
				if clusterName != "" {
					fmt.Printf("No restores found for cluster %s.\n", clusterName)
				} else {
					fmt.Println("No restores found.")
				}
				return nil
			}

			switch outputFormat {
			case "wide":
				util.PrintRestoresWide(filteredRestores)
			case "json":
				util.PrintRestoresJSON(filteredRestores)
			case "yaml":
				util.PrintRestoresYAML(filteredRestores)
			default:
				util.PrintRestores(filteredRestores)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List restores across all namespaces")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (wide|json|yaml)")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "Filter by cluster name")

	return cmd
}

func newRestoreGetCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get <restore-name>",
		Short: "Get detailed information about a restore",
		Long:  "Get detailed information about a specific Neo4j restore.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Get restore details
  kubectl neo4j restore get my-restore

  # Get restore details in YAML format
  kubectl neo4j restore get my-restore -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			restoreName := args[0]
			namespace := util.GetNamespace(configFlags)

			var restore neo4jv1alpha1.Neo4jRestore
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      restoreName,
				Namespace: namespace,
			}, &restore); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("restore %s not found in namespace %s", restoreName, namespace)
				}
				return fmt.Errorf("failed to get restore: %w", err)
			}

			switch outputFormat {
			case "json":
				util.PrintRestoreJSON(restore)
			case "yaml":
				util.PrintRestoreYAML(restore)
			default:
				util.PrintRestoreDetailed(restore)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json|yaml)")

	return cmd
}

func newRestoreDeleteCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <restore-name>",
		Short: "Delete a Neo4j restore",
		Long:  "Delete a Neo4j restore resource.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Delete a restore
  kubectl neo4j restore delete my-restore

  # Force delete without confirmation
  kubectl neo4j restore delete my-restore --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			restoreName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Check if restore exists
			var restore neo4jv1alpha1.Neo4jRestore
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      restoreName,
				Namespace: namespace,
			}, &restore); err != nil {
				if errors.IsNotFound(err) {
					fmt.Printf("Restore %s not found in namespace %s\n", restoreName, namespace)
					return nil
				}
				return fmt.Errorf("failed to get restore: %w", err)
			}

			// Confirmation prompt
			if !force {
				fmt.Printf("Are you sure you want to delete restore %s?\n", restoreName)
				fmt.Print("Type 'yes' to confirm: ")
				var response string
				fmt.Scanln(&response)
				if response != "yes" {
					fmt.Println("Operation cancelled.")
					return nil
				}
			}

			fmt.Printf("Deleting restore %s...\n", restoreName)
			if err := crClient.Delete(ctx, &restore); err != nil {
				return fmt.Errorf("failed to delete restore: %w", err)
			}

			fmt.Printf("Restore %s deleted successfully.\n", restoreName)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

func newRestoreStatusCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <restore-name>",
		Short: "Show the status of a restore",
		Long:  "Show detailed status information about a Neo4j restore.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Show restore status
  kubectl neo4j restore status my-restore`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			restoreName := args[0]
			namespace := util.GetNamespace(configFlags)

			var restore neo4jv1alpha1.Neo4jRestore
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      restoreName,
				Namespace: namespace,
			}, &restore); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("restore %s not found in namespace %s", restoreName, namespace)
				}
				return fmt.Errorf("failed to get restore: %w", err)
			}

			util.PrintRestoreStatus(restore)
			return nil
		},
	}

	return cmd
}
