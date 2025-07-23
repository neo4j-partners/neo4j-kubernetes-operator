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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Backup RBAC Automatic Creation", func() {
	const (
		timeout  = time.Second * 60
		interval = time.Second * 2
	)

	Context("When creating a backup resource", func() {
		var (
			testNamespace string
			cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
			adminSecret   *corev1.Secret
		)

		BeforeEach(func() {
			testNamespace = createTestNamespace("backup-rbac")

			By("Creating admin secret")
			adminSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("password123"),
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

			By("Creating Neo4j cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rbac-test-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						Size:      "10Gi",
						ClassName: "standard",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for cluster to be ready")
			Eventually(func() bool {
				var clusterStatus neo4jv1alpha1.Neo4jEnterpriseCluster
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: testNamespace,
				}, &clusterStatus)
				return err == nil && clusterStatus.Status.Phase == "Ready"
			}, timeout, interval).Should(BeTrue())
		})

		It("should automatically create RBAC resources when backup is created", func() {
			By("Creating a PVC for backup storage")
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-pvc",
					Namespace: testNamespace,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).Should(Succeed())

			By("Creating a backup resource")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: cluster.Name,
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Name: "backup-pvc",
							Size: "5Gi",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Verifying service account is created automatically")
			Eventually(func() error {
				sa := &corev1.ServiceAccount{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-sa",
					Namespace: testNamespace,
				}, sa)
			}, timeout, interval).Should(Succeed())

			By("Verifying role is created automatically")
			Eventually(func() error {
				role := &rbacv1.Role{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-role",
					Namespace: testNamespace,
				}, role)
			}, timeout, interval).Should(Succeed())

			By("Verifying role has correct permissions")
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-role",
				Namespace: testNamespace,
			}, role)).Should(Succeed())

			// Check permissions
			Expect(role.Rules).To(HaveLen(3))
			Expect(role.Rules[0].APIGroups).To(Equal([]string{""}))
			Expect(role.Rules[0].Resources).To(Equal([]string{"pods"}))
			Expect(role.Rules[0].Verbs).To(ConsistOf("get", "list"))

			Expect(role.Rules[1].APIGroups).To(Equal([]string{""}))
			Expect(role.Rules[1].Resources).To(Equal([]string{"pods/exec"}))
			Expect(role.Rules[1].Verbs).To(ConsistOf("create"))

			Expect(role.Rules[2].APIGroups).To(Equal([]string{""}))
			Expect(role.Rules[2].Resources).To(Equal([]string{"pods/log"}))
			Expect(role.Rules[2].Verbs).To(ConsistOf("get"))

			By("Verifying role binding is created automatically")
			Eventually(func() error {
				rb := &rbacv1.RoleBinding{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-rolebinding",
					Namespace: testNamespace,
				}, rb)
			}, timeout, interval).Should(Succeed())

			By("Verifying role binding references correct service account and role")
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-rolebinding",
				Namespace: testNamespace,
			}, rb)).Should(Succeed())

			Expect(rb.RoleRef.Name).To(Equal("neo4j-backup-role"))
			Expect(rb.RoleRef.Kind).To(Equal("Role"))
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Name).To(Equal("neo4j-backup-sa"))
			Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(rb.Subjects[0].Namespace).To(Equal(testNamespace))
		})

		It("should handle RBAC creation for scheduled backups", func() {
			By("Creating a scheduled backup resource")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scheduled-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: cluster.Name,
					},
					Schedule: "*/5 * * * *", // Every 5 minutes
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Name: "backup-pvc",
							Size: "5Gi",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

			By("Verifying RBAC resources are created for scheduled backup")
			Eventually(func() error {
				sa := &corev1.ServiceAccount{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-sa",
					Namespace: testNamespace,
				}, sa)
			}, timeout, interval).Should(Succeed())
		})

		It("should reuse existing RBAC resources", func() {
			By("Creating first backup")
			backup1 := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-1",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: cluster.Name,
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Name: "backup-pvc",
							Size: "5Gi",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup1)).Should(Succeed())

			By("Waiting for RBAC resources to be created")
			Eventually(func() error {
				sa := &corev1.ServiceAccount{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-sa",
					Namespace: testNamespace,
				}, sa)
			}, timeout, interval).Should(Succeed())

			By("Getting the service account UID")
			sa := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-sa",
				Namespace: testNamespace,
			}, sa)).Should(Succeed())
			originalUID := sa.UID

			By("Creating second backup in same namespace")
			backup2 := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-2",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: cluster.Name,
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Name: "backup-pvc-2",
							Size: "5Gi",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup2)).Should(Succeed())

			By("Verifying service account was not recreated")
			Eventually(func() bool {
				sa := &corev1.ServiceAccount{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "neo4j-backup-sa",
					Namespace: testNamespace,
				}, sa)
				return err == nil && sa.UID == originalUID
			}, timeout, interval).Should(BeTrue())
		})

		AfterEach(func() {
			By("Cleaning up resources")
			// Delete backups with finalizer removal
			backupList := &neo4jv1alpha1.Neo4jBackupList{}
			if err := k8sClient.List(ctx, backupList, client.InNamespace(testNamespace)); err == nil {
				for i := range backupList.Items {
					backup := &backupList.Items[i]
					if len(backup.GetFinalizers()) > 0 {
						backup.SetFinalizers([]string{})
						_ = k8sClient.Update(ctx, backup)
					}
					_ = k8sClient.Delete(ctx, backup)
				}
			}

			// Delete cluster with finalizer removal
			if cluster != nil {
				if len(cluster.GetFinalizers()) > 0 {
					cluster.SetFinalizers([]string{})
					_ = k8sClient.Update(ctx, cluster)
				}
				_ = k8sClient.Delete(ctx, cluster)
			}

			// Delete secret
			if adminSecret != nil {
				_ = k8sClient.Delete(ctx, adminSecret)
			}

			// Note: RBAC resources and namespace will be cleaned up by test suite cleanup
		})
	})
})
