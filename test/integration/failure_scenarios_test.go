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

package integration_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Failure Scenarios", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "failure-test"

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		// Clean up namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	Context("Invalid Cluster Configurations", func() {
		It("should reject cluster with invalid primary count", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-primaries",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   0, // Invalid: should be at least 1
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}

			// This should fail validation
			err := k8sClient.Create(ctx, cluster)
			Expect(err).To(HaveOccurred())
		})

		It("should reject cluster with invalid secondary count", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-secondaries",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: -1, // Invalid: should be non-negative
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}

			// This should fail validation
			err := k8sClient.Create(ctx, cluster)
			Expect(err).To(HaveOccurred())
		})

		It("should reject cluster with invalid storage size", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-storage",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "invalid-size", // Invalid size format
						ClassName: "standard",
					},
				},
			}

			// This should fail validation
			err := k8sClient.Create(ctx, cluster)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Resource Conflicts", func() {
		It("should handle duplicate cluster names gracefully", func() {
			clusterName := "test-cluster-duplicate"

			// Create first cluster
			cluster1 := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster1)).To(Succeed())

			// Try to create duplicate cluster
			cluster2 := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}

			// This should fail due to duplicate name
			err := k8sClient.Create(ctx, cluster2)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, cluster1)).To(Succeed())
		})
	})

	Context("Backup Failure Scenarios", func() {
		It("should handle backup with non-existent target cluster", func() {
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-no-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "non-existent-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Size:             "1Gi",
							StorageClassName: "standard",
						},
					},
				},
			}

			// Create backup
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			// Wait for backup to fail
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-backup-no-cluster",
					Namespace: namespace,
				}, backup); err != nil {
					return ""
				}
				return backup.Status.Phase
			}, time.Minute*2, time.Second*5).Should(Equal("Failed"))

			// Verify failure message is present
			Expect(backup.Status.Message).NotTo(BeEmpty())
		})

		It("should handle backup with invalid storage configuration", func() {
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-invalid-storage",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "s3",
						// Missing required bucket configuration for S3
					},
				},
			}

			// This should fail validation or quickly fail during processing
			err := k8sClient.Create(ctx, backup)
			if err == nil {
				// If creation succeeds, it should fail quickly
				Eventually(func() string {
					if err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      "test-backup-invalid-storage",
						Namespace: namespace,
					}, backup); err != nil {
						return ""
					}
					return backup.Status.Phase
				}, time.Minute*1, time.Second*5).Should(Equal("Failed"))
			} else {
				// Validation should catch this
				Expect(err).To(HaveOccurred())
			}
		})
	})

	Context("Restore Failure Scenarios", func() {
		It("should handle restore with non-existent backup", func() {
			restore := &neo4jv1alpha1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-no-backup",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jRestoreSpec{
					TargetCluster: "test-cluster",
					DatabaseName:  "neo4j",
					Source: neo4jv1alpha1.RestoreSource{
						Type:      "backup",
						BackupRef: "non-existent-backup",
					},
				},
			}

			// Create restore
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			// Wait for restore to fail
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-restore-no-backup",
					Namespace: namespace,
				}, restore); err != nil {
					return ""
				}
				return restore.Status.Phase
			}, time.Minute*2, time.Second*5).Should(Equal("Failed"))

			// Verify failure message is present
			Expect(restore.Status.Message).NotTo(BeEmpty())
		})

		It("should handle restore with invalid target cluster", func() {
			restore := &neo4jv1alpha1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore-no-cluster",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jRestoreSpec{
					TargetCluster: "non-existent-cluster",
					DatabaseName:  "neo4j",
					Source: neo4jv1alpha1.RestoreSource{
						Type:      "backup",
						BackupRef: "test-backup",
					},
				},
			}

			// Create restore
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			// Wait for restore to fail
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-restore-no-cluster",
					Namespace: namespace,
				}, restore); err != nil {
					return ""
				}
				return restore.Status.Phase
			}, time.Minute*2, time.Second*5).Should(Equal("Failed"))

			// Verify failure message is present
			Expect(restore.Status.Message).NotTo(BeEmpty())
		})
	})

	Context("Network and Connectivity Issues", func() {
		It("should handle cluster creation with unreachable storage class", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-bad-storage",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "non-existent-storage-class",
					},
				},
			}

			// Create cluster
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Cluster should not reach Ready state
			Consistently(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-cluster-bad-storage",
					Namespace: namespace,
				}, cluster); err != nil {
					return ""
				}
				return cluster.Status.Phase
			}, time.Minute*2, time.Second*10).ShouldNot(Equal("Ready"))

			// Clean up
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
		})
	})

	Context("Resource Limits and Constraints", func() {
		It("should handle cluster with excessive resource requests", func() {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-excessive-resources",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Ti", // Very large storage request
						ClassName: "standard",
					},
				},
			}

			// Create cluster
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Cluster should not reach Ready state due to resource constraints
			Consistently(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-cluster-excessive-resources",
					Namespace: namespace,
				}, cluster); err != nil {
					return ""
				}
				return cluster.Status.Phase
			}, time.Minute*2, time.Second*10).ShouldNot(Equal("Ready"))

			// Clean up
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
		})
	})

	Context("Concurrent Operations", func() {
		It("should handle multiple backup operations on the same cluster", func() {
			// Create a test cluster first
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-concurrent",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   3,
						Secondaries: 0,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create multiple concurrent backups
			backup1 := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-concurrent-1",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster-concurrent",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Size:             "1Gi",
							StorageClassName: "standard",
						},
					},
				},
			}

			backup2 := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-concurrent-2",
					Namespace: namespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster-concurrent",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Size:             "1Gi",
							StorageClassName: "standard",
						},
					},
				},
			}

			// Create both backups
			Expect(k8sClient.Create(ctx, backup1)).To(Succeed())
			Expect(k8sClient.Create(ctx, backup2)).To(Succeed())

			// At least one should succeed, but they should be handled gracefully
			Eventually(func() bool {
				var b1, b2 neo4jv1alpha1.Neo4jBackup
				err1 := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-backup-concurrent-1",
					Namespace: namespace,
				}, &b1)
				err2 := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-backup-concurrent-2",
					Namespace: namespace,
				}, &b2)

				if err1 != nil || err2 != nil {
					return false
				}

				// At least one should complete or fail (not stuck)
				return (b1.Status.Phase == "Completed" || b1.Status.Phase == "Failed") ||
					(b2.Status.Phase == "Completed" || b2.Status.Phase == "Failed")
			}, time.Minute*5, time.Second*10).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, backup1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, backup2)).To(Succeed())
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
		})
	})
})
