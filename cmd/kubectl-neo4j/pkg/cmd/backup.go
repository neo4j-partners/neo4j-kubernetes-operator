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

// NewBackupCommand creates the backup command with subcommands
func NewBackupCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage Neo4j cluster backups",
		Long:  "Create, manage, and monitor backups of Neo4j Enterprise clusters.",
	}

	cmd.AddCommand(newBackupCreateCommand(configFlags))
	cmd.AddCommand(newBackupListCommand(configFlags))
	cmd.AddCommand(newBackupGetCommand(configFlags))
	cmd.AddCommand(newBackupDeleteCommand(configFlags))
	cmd.AddCommand(newBackupScheduleCommand(configFlags))
	cmd.AddCommand(newBackupStatusCommand(configFlags))

	return cmd
}

func newBackupCreateCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
		storageType string
		bucket      string
		path        string
		compress    bool
		verify      bool
		wait        bool
		timeout     time.Duration
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "create <backup-name>",
		Short: "Create a backup of a Neo4j cluster",
		Long:  "Create a one-time backup of a Neo4j Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Create a backup to PVC storage
  kubectl neo4j backup create my-backup --cluster=production --storage=pvc

  # Create a backup to S3
  kubectl neo4j backup create my-backup --cluster=production --storage=s3 --bucket=my-backups --path=/neo4j

  # Create a compressed and verified backup
  kubectl neo4j backup create my-backup --cluster=production --storage=s3 --bucket=my-backups --compress --verify

  # Dry run to see what would be created
  kubectl neo4j backup create my-backup --cluster=production --storage=pvc --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			backupName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Validate backup name
			if strings.TrimSpace(backupName) == "" {
				return fmt.Errorf("backup name cannot be empty")
			}

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

			// Build backup specification
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backupName,
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: clusterName,
					},
					Options: &neo4jv1alpha1.BackupOptions{
						Compress: compress,
						Verify:   verify,
					},
				},
			}

			// Configure storage
			switch storageType {
			case "pvc":
				backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1alpha1.PVCSpec{
						Size:             "50Gi",
						StorageClassName: "standard",
					},
				}
			case "s3":
				if bucket == "" {
					return fmt.Errorf("bucket is required for S3 storage")
				}
				backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
					Type:   "s3",
					Bucket: bucket,
					Path:   path,
					Cloud: &neo4jv1alpha1.CloudBlock{
						Provider: "aws",
						Identity: &neo4jv1alpha1.CloudIdentity{
							Provider: "aws",
						},
					},
				}
			case "gcs":
				if bucket == "" {
					return fmt.Errorf("bucket is required for GCS storage")
				}
				backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
					Type:   "gcs",
					Bucket: bucket,
					Path:   path,
					Cloud: &neo4jv1alpha1.CloudBlock{
						Provider: "gcp",
						Identity: &neo4jv1alpha1.CloudIdentity{
							Provider: "gcp",
						},
					},
				}
			case "azure":
				if bucket == "" {
					return fmt.Errorf("container is required for Azure storage")
				}
				backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
					Type:   "azure",
					Bucket: bucket,
					Path:   path,
					Cloud: &neo4jv1alpha1.CloudBlock{
						Provider: "azure",
						Identity: &neo4jv1alpha1.CloudIdentity{
							Provider: "azure",
						},
					},
				}
			default:
				return fmt.Errorf("unsupported storage type: %s (supported: pvc, s3, gcs, azure)", storageType)
			}

			if dryRun {
				fmt.Printf("Would create backup:\n")
				util.PrintBackupYAML(*backup)
				return nil
			}

			fmt.Printf("Creating backup %s for cluster %s...\n", backupName, clusterName)
			if err := crClient.Create(ctx, backup); err != nil {
				return fmt.Errorf("failed to create backup: %w", err)
			}

			fmt.Printf("Backup %s created successfully.\n", backupName)

			if wait {
				fmt.Printf("Waiting for backup to complete (timeout: %v)...\n", timeout)
				return util.WaitForBackupComplete(ctx, crClient, backupName, namespace, timeout)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().StringVar(&storageType, "storage", "pvc", "Storage type (pvc|s3|gcs|azure)")
	cmd.Flags().StringVar(&bucket, "bucket", "", "Storage bucket/container name")
	cmd.Flags().StringVar(&path, "path", "/", "Storage path")
	cmd.Flags().BoolVar(&compress, "compress", true, "Compress backup")
	cmd.Flags().BoolVar(&verify, "verify", true, "Verify backup integrity")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for backup to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Timeout for waiting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be created without creating")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

func newBackupListCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var allNamespaces bool
	var outputFormat string
	var clusterName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Neo4j backups",
		Long:  "List all Neo4j backups in the current or specified namespace.",
		Example: `  # List all backups
  kubectl neo4j backup list

  # List backups for a specific cluster
  kubectl neo4j backup list --cluster=production

  # List backups in all namespaces
  kubectl neo4j backup list --all-namespaces`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			namespace := ""
			if !allNamespaces {
				namespace = util.GetNamespace(configFlags)
			}

			var backupList neo4jv1alpha1.Neo4jBackupList
			listOpts := []client.ListOption{}
			if namespace != "" {
				listOpts = append(listOpts, client.InNamespace(namespace))
			}

			if err := crClient.List(ctx, &backupList, listOpts...); err != nil {
				return fmt.Errorf("failed to list backups: %w", err)
			}

			// Filter by cluster if specified
			var filteredBackups []neo4jv1alpha1.Neo4jBackup
			for _, backup := range backupList.Items {
				if clusterName == "" || backup.Spec.Target.Name == clusterName {
					filteredBackups = append(filteredBackups, backup)
				}
			}

			if len(filteredBackups) == 0 {
				if clusterName != "" {
					fmt.Printf("No backups found for cluster %s.\n", clusterName)
				} else {
					fmt.Println("No backups found.")
				}
				return nil
			}

			switch outputFormat {
			case "wide":
				util.PrintBackupsWide(filteredBackups)
			case "json":
				util.PrintBackupsJSON(filteredBackups)
			case "yaml":
				util.PrintBackupsYAML(filteredBackups)
			default:
				util.PrintBackups(filteredBackups)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List backups across all namespaces")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (wide|json|yaml)")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "Filter by cluster name")

	return cmd
}

func newBackupGetCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get <backup-name>",
		Short: "Get detailed information about a backup",
		Long:  "Get detailed information about a specific Neo4j backup.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Get backup details
  kubectl neo4j backup get my-backup

  # Get backup details in YAML format
  kubectl neo4j backup get my-backup -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			backupName := args[0]
			namespace := util.GetNamespace(configFlags)

			var backup neo4jv1alpha1.Neo4jBackup
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      backupName,
				Namespace: namespace,
			}, &backup); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("backup %s not found in namespace %s", backupName, namespace)
				}
				return fmt.Errorf("failed to get backup: %w", err)
			}

			switch outputFormat {
			case "json":
				util.PrintBackupJSON(backup)
			case "yaml":
				util.PrintBackupYAML(backup)
			default:
				util.PrintBackupDetailed(backup)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json|yaml)")

	return cmd
}

func newBackupDeleteCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <backup-name>",
		Short: "Delete a Neo4j backup",
		Long:  "Delete a Neo4j backup resource and optionally the backup data.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Delete a backup
  kubectl neo4j backup delete my-backup

  # Force delete without confirmation
  kubectl neo4j backup delete my-backup --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			backupName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Check if backup exists
			var backup neo4jv1alpha1.Neo4jBackup
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      backupName,
				Namespace: namespace,
			}, &backup); err != nil {
				if errors.IsNotFound(err) {
					fmt.Printf("Backup %s not found in namespace %s\n", backupName, namespace)
					return nil
				}
				return fmt.Errorf("failed to get backup: %w", err)
			}

			// Confirmation prompt
			if !force {
				fmt.Printf("Are you sure you want to delete backup %s?\n", backupName)
				fmt.Print("Type 'yes' to confirm: ")
				var response string
				fmt.Scanln(&response)
				if response != "yes" {
					fmt.Println("Operation cancelled.")
					return nil
				}
			}

			fmt.Printf("Deleting backup %s...\n", backupName)
			if err := crClient.Delete(ctx, &backup); err != nil {
				return fmt.Errorf("failed to delete backup: %w", err)
			}

			fmt.Printf("Backup %s deleted successfully.\n", backupName)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

func newBackupScheduleCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName string
		schedule    string
		storageType string
		bucket      string
		path        string
		retention   string
		compress    bool
		verify      bool
	)

	cmd := &cobra.Command{
		Use:   "schedule <backup-name>",
		Short: "Create a scheduled backup",
		Long:  "Create a scheduled backup that runs automatically based on a cron expression.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Schedule daily backups at 2 AM
  kubectl neo4j backup schedule daily-backup --cluster=production --schedule="0 2 * * *" --storage=s3 --bucket=my-backups

  # Schedule weekly backups with retention
  kubectl neo4j backup schedule weekly-backup --cluster=production --schedule="0 2 * * 0" --storage=s3 --bucket=my-backups --retention="4w"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			backupName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Validate backup name and schedule
			if strings.TrimSpace(backupName) == "" {
				return fmt.Errorf("backup name cannot be empty")
			}
			if strings.TrimSpace(schedule) == "" {
				return fmt.Errorf("schedule cannot be empty")
			}

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

			// Build scheduled backup specification
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backupName,
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Schedule: schedule,
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: clusterName,
					},
					Options: &neo4jv1alpha1.BackupOptions{
						Compress: compress,
						Verify:   verify,
					},
				},
			}

			// Add retention policy if specified
			if retention != "" {
				backup.Spec.Retention = &neo4jv1alpha1.RetentionPolicy{
					MaxAge: retention,
				}
			}

			// Configure storage (similar to create command)
			switch storageType {
			case "pvc":
				backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1alpha1.PVCSpec{
						Size:             "50Gi",
						StorageClassName: "standard",
					},
				}
			case "s3":
				if bucket == "" {
					return fmt.Errorf("bucket is required for S3 storage")
				}
				backup.Spec.Storage = neo4jv1alpha1.StorageLocation{
					Type:   "s3",
					Bucket: bucket,
					Path:   path,
					Cloud: &neo4jv1alpha1.CloudBlock{
						Provider: "aws",
						Identity: &neo4jv1alpha1.CloudIdentity{
							Provider: "aws",
						},
					},
				}
			default:
				return fmt.Errorf("unsupported storage type: %s", storageType)
			}

			fmt.Printf("Creating scheduled backup %s for cluster %s with schedule %s...\n", backupName, clusterName, schedule)
			if err := crClient.Create(ctx, backup); err != nil {
				return fmt.Errorf("failed to create scheduled backup: %w", err)
			}

			fmt.Printf("Scheduled backup %s created successfully.\n", backupName)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().StringVar(&schedule, "schedule", "0 2 * * *", "Cron schedule expression")
	cmd.Flags().StringVar(&storageType, "storage", "pvc", "Storage type (pvc|s3|gcs|azure)")
	cmd.Flags().StringVar(&bucket, "bucket", "", "Storage bucket/container name")
	cmd.Flags().StringVar(&path, "path", "/", "Storage path")
	cmd.Flags().StringVar(&retention, "retention", "", "Retention policy (e.g., 7d, 4w, 12m)")
	cmd.Flags().BoolVar(&compress, "compress", true, "Compress backup")
	cmd.Flags().BoolVar(&verify, "verify", true, "Verify backup integrity")
	cmd.MarkFlagRequired("cluster")

	return cmd
}

func newBackupStatusCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <backup-name>",
		Short: "Show the status of a backup",
		Long:  "Show detailed status information about a Neo4j backup.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Show backup status
  kubectl neo4j backup status my-backup`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			backupName := args[0]
			namespace := util.GetNamespace(configFlags)

			var backup neo4jv1alpha1.Neo4jBackup
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      backupName,
				Namespace: namespace,
			}, &backup); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("backup %s not found in namespace %s", backupName, namespace)
				}
				return fmt.Errorf("failed to get backup: %w", err)
			}

			util.PrintBackupStatus(backup)
			return nil
		},
	}

	return cmd
}
