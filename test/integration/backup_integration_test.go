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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Backup Integration Tests", Ordered, func() {
	const (
		backupTimeout  = time.Second * 600
		backupInterval = time.Second * 2
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
	)

	BeforeAll(func() {
		testNamespace = createTestNamespace("backup-int")

		By("Creating admin secret")
		adminSecret := &corev1.Secret{
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

		By("Creating shared Neo4j cluster for backup tests")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-test-cluster",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1beta1.ImageSpec{
					Repo: "neo4j",
					Tag:  getNeo4jImageTag(),
				},
				Topology: neo4jv1beta1.TopologyConfiguration{
					Servers: 2,
				},
				Storage: neo4jv1beta1.StorageSpec{
					Size:      "1Gi",
					ClassName: "standard",
				},
				Resources: getCIAppropriateResourceRequirements(),
				Auth: &neo4jv1beta1.AuthSpec{
					AdminSecret: "neo4j-admin-secret",
				},
				TLS: &neo4jv1beta1.TLSSpec{
					Mode: "disabled",
				},
				Env: []corev1.EnvVar{
					{
						Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
						Value: "eval",
					},
				},
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

		By("Waiting for cluster to be ready")
		Eventually(func() bool {
			var clusterStatus neo4jv1beta1.Neo4jEnterpriseCluster
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: testNamespace,
			}, &clusterStatus)
			if err != nil {
				return false
			}
			if clusterStatus.Status.Phase == "Ready" {
				GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
					clusterStatus.Status.Phase, clusterStatus.Status.Message)
				return true
			}
			GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
				clusterStatus.Status.Phase, clusterStatus.Status.Message)
			return false
		}, clusterTimeout, backupInterval).Should(BeTrue())
	})

	AfterAll(func() {
		By("Cleaning up shared backup test resources")
		// Clean up backups
		backupList := &neo4jv1beta1.Neo4jBackupList{}
		if err := k8sClient.List(ctx, backupList, client.InNamespace(testNamespace)); err == nil {
			for i := range backupList.Items {
				backup := &backupList.Items[i]
				backup.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, backup)
				_ = k8sClient.Delete(ctx, backup)
			}
		}
		// Clean up cluster
		if cluster != nil {
			var latest neo4jv1beta1.Neo4jEnterpriseCluster
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name, Namespace: testNamespace,
			}, &latest); err == nil {
				latest.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, &latest)
				_ = k8sClient.Delete(ctx, &latest)
			}
		}
		cleanupCustomResourcesInNamespace(testNamespace)
	})

	It("should automatically create ServiceAccount when backup is created", func() {
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
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
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
		}, backupTimeout, backupInterval).Should(Succeed())
	})

	It("should handle RBAC creation for scheduled backups", func() {
		By("Creating a scheduled backup resource")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scheduled-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: cluster.Name,
				},
				Schedule: "*/5 * * * *",
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
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
		}, backupTimeout, backupInterval).Should(Succeed())
	})

	It("should reuse existing RBAC resources", func() {
		By("Getting the service account UID from previous tests")
		sa := &corev1.ServiceAccount{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      "neo4j-backup-sa",
			Namespace: testNamespace,
		}, sa)).Should(Succeed())
		originalUID := sa.UID

		By("Creating another backup in same namespace")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-reuse-test",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name: "backup-pvc-2",
						Size: "5Gi",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

		By("Verifying service account was not recreated")
		Eventually(func() bool {
			sa := &corev1.ServiceAccount{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-sa",
				Namespace: testNamespace,
			}, sa)
			return err == nil && sa.UID == originalUID
		}, backupTimeout, backupInterval).Should(BeTrue())
	})

	It("should create a backup resource against a ready cluster", func() {
		By("Creating a backup resource")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "simple-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind:      "Cluster",
					Name:      cluster.Name,
					Namespace: testNamespace,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name: "backup-pvc",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())

		By("Waiting for backup to be created")
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      backup.Name,
				Namespace: backup.Namespace,
			}, backup)
			return err == nil
		}, backupTimeout, backupInterval).Should(BeTrue())
	})
})
