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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Simple Backup Test", func() {
	const (
		timeout  = time.Second * 180
		interval = time.Second * 5
	)

	Context("When testing backup functionality", func() {
		var (
			testNamespace string
			cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
			adminSecret   *corev1.Secret
			backupPVC     *corev1.PersistentVolumeClaim
		)

		BeforeEach(func() {
			testNamespace = createTestNamespace("simple-backup")

			By("Creating admin secret")
			adminSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "neo4j-admin-secret",
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"username": []byte("neo4j"),
					"password": []byte("admin123"),
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			By("Creating backup PVC")
			backupPVC = &corev1.PersistentVolumeClaim{
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
			Expect(k8sClient.Create(ctx, backupPVC)).To(Succeed())

			By("Creating a test cluster")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-cluster-%d", time.Now().UnixNano()),
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Edition: "enterprise",
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  "5.26.0-enterprise",
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Primaries:   1,
						Secondaries: 1,
					},
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AdminSecret: adminSecret.Name,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for cluster to be ready")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				}, cluster)
				if err != nil {
					return false
				}
				for _, condition := range cluster.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		AfterEach(func() {
			By("Cleaning up test resources")
			if cluster != nil {
				Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
			}
			if backupPVC != nil {
				Expect(k8sClient.Delete(ctx, backupPVC)).To(Succeed())
			}
			if adminSecret != nil {
				Expect(k8sClient.Delete(ctx, adminSecret)).To(Succeed())
			}
		})

		It("Should create a backup", func() {
			By("Creating a backup resource")
			backup := &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind:      "Cluster",
						Name:      cluster.Name,
						Namespace: testNamespace,
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Name: backupPVC.Name,
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
			}, timeout, interval).Should(BeTrue())

			By("Backup resource created successfully")
		})
	})
})
