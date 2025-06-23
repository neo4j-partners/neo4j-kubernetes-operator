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

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/cmd/kubectl-neo4j/pkg/util"
)

// NewUserCommand creates the user command with subcommands
func NewUserCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage Neo4j cluster users",
		Long:  "Create, manage, and configure users for Neo4j Enterprise clusters.",
	}

	cmd.AddCommand(newUserCreateCommand(configFlags))
	cmd.AddCommand(newUserListCommand(configFlags))
	cmd.AddCommand(newUserGetCommand(configFlags))
	cmd.AddCommand(newUserDeleteCommand(configFlags))

	return cmd
}

func newUserCreateCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		clusterName    string
		passwordSecret string
		roles          []string
		dryRun         bool
	)

	cmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a Neo4j user",
		Long:  "Create a new user for a Neo4j Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Create a basic user
  kubectl neo4j user create alice --cluster=production --password-secret=alice-password

  # Create a user with specific roles
  kubectl neo4j user create bob --cluster=production --password-secret=bob-password --roles=reader,editor

  # Dry run to see what would be created
  kubectl neo4j user create charlie --cluster=production --password-secret=charlie-password --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			username := args[0]
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

			// Build user specification
			user := &neo4jv1alpha1.Neo4jUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%s", clusterName, username),
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jUserSpec{
					ClusterRef: clusterName,
					Username:   username,
					PasswordSecret: neo4jv1alpha1.PasswordSecretRef{
						Name: passwordSecret,
						Key:  "password",
					},
					Roles: roles,
				},
			}

			if dryRun {
				fmt.Printf("Would create user:\n")
				util.PrintUserYAML(*user)
				return nil
			}

			fmt.Printf("Creating user %s for cluster %s...\n", username, clusterName)
			if err := crClient.Create(ctx, user); err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}

			fmt.Printf("User %s created successfully.\n", username)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster", "", "Target cluster name (required)")
	cmd.Flags().StringVar(&passwordSecret, "password-secret", "", "Secret containing user password (required)")
	cmd.Flags().StringSliceVar(&roles, "roles", []string{}, "User roles")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be created without creating")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("password-secret")

	return cmd
}

func newUserListCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var allNamespaces bool
	var outputFormat string
	var clusterName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Neo4j users",
		Long:  "List all Neo4j users in the current or specified namespace.",
		Example: `  # List all users
  kubectl neo4j user list

  # List users for a specific cluster
  kubectl neo4j user list --cluster=production

  # List users in all namespaces
  kubectl neo4j user list --all-namespaces`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			namespace := ""
			if !allNamespaces {
				namespace = util.GetNamespace(configFlags)
			}

			var userList neo4jv1alpha1.Neo4jUserList
			listOpts := []client.ListOption{}
			if namespace != "" {
				listOpts = append(listOpts, client.InNamespace(namespace))
			}

			if err := crClient.List(ctx, &userList, listOpts...); err != nil {
				return fmt.Errorf("failed to list users: %w", err)
			}

			// Filter by cluster if specified
			var filteredUsers []neo4jv1alpha1.Neo4jUser
			for _, user := range userList.Items {
				if clusterName == "" || user.Spec.ClusterRef == clusterName {
					filteredUsers = append(filteredUsers, user)
				}
			}

			if len(filteredUsers) == 0 {
				if clusterName != "" {
					fmt.Printf("No users found for cluster %s.\n", clusterName)
				} else {
					fmt.Println("No users found.")
				}
				return nil
			}

			switch outputFormat {
			case "wide":
				util.PrintUsersWide(filteredUsers)
			case "json":
				util.PrintUsersJSON(filteredUsers)
			case "yaml":
				util.PrintUsersYAML(filteredUsers)
			default:
				util.PrintUsers(filteredUsers)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List users across all namespaces")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (wide|json|yaml)")
	cmd.Flags().StringVar(&clusterName, "cluster", "", "Filter by cluster name")

	return cmd
}

func newUserGetCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get <user-name>",
		Short: "Get detailed information about a user",
		Long:  "Get detailed information about a specific Neo4j user.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Get user details
  kubectl neo4j user get production-alice

  # Get user details in YAML format
  kubectl neo4j user get production-alice -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			userName := args[0]
			namespace := util.GetNamespace(configFlags)

			var user neo4jv1alpha1.Neo4jUser
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      userName,
				Namespace: namespace,
			}, &user); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("user %s not found in namespace %s", userName, namespace)
				}
				return fmt.Errorf("failed to get user: %w", err)
			}

			switch outputFormat {
			case "json":
				util.PrintUserJSON(user)
			case "yaml":
				util.PrintUserYAML(user)
			default:
				util.PrintUserDetailed(user)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json|yaml)")

	return cmd
}

func newUserDeleteCommand(configFlags *genericclioptions.ConfigFlags) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <user-name>",
		Short: "Delete a Neo4j user",
		Long:  "Delete a Neo4j user from an Enterprise cluster.",
		Args:  cobra.ExactArgs(1),
		Example: `  # Delete a user
  kubectl neo4j user delete production-alice

  # Force delete without confirmation
  kubectl neo4j user delete production-alice --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			crClient := ctx.Value("crClient").(client.Client)

			userName := args[0]
			namespace := util.GetNamespace(configFlags)

			// Check if user exists
			var user neo4jv1alpha1.Neo4jUser
			if err := crClient.Get(ctx, types.NamespacedName{
				Name:      userName,
				Namespace: namespace,
			}, &user); err != nil {
				if errors.IsNotFound(err) {
					fmt.Printf("User %s not found in namespace %s\n", userName, namespace)
					return nil
				}
				return fmt.Errorf("failed to get user: %w", err)
			}

			// Confirmation prompt
			if !force {
				fmt.Printf("Are you sure you want to delete user %s from cluster %s?\n", user.Spec.Username, user.Spec.ClusterRef)
				fmt.Print("Type 'yes' to confirm: ")
				var response string
				fmt.Scanln(&response)
				if response != "yes" {
					fmt.Println("Operation cancelled.")
					return nil
				}
			}

			fmt.Printf("Deleting user %s from cluster %s...\n", user.Spec.Username, user.Spec.ClusterRef)
			if err := crClient.Delete(ctx, &user); err != nil {
				return fmt.Errorf("failed to delete user: %w", err)
			}

			fmt.Printf("User %s deleted successfully.\n", user.Spec.Username)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}
