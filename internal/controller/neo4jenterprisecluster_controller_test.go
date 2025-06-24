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

package controller_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Neo4jEnterpriseCluster Controller", func() {
	const (
		timeout  = time.Second * 30
		interval = time.Millisecond * 250
	)

	var (
		ctx           context.Context
		cluster       *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName   string
		namespaceName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
		namespaceName = "default"

		// Create basic cluster spec
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespaceName,
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Edition: "enterprise",
				Image: neo4jv1alpha1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Primaries:   3,
					Secondaries: 2,
				},
				Storage: neo4jv1alpha1.StorageSpec{
					ClassName: "standard",
					Size:      "10Gi",
				},
			},
		}
	})

	AfterEach(func() {
		if cluster != nil {
			// Clean up the cluster and related resources
			if err := k8sClient.Delete(ctx, cluster, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				// Log the error but don't fail the test cleanup
				fmt.Printf("Warning: Failed to delete cluster during cleanup: %v\n", err)
			}

			// Wait for cluster to be deleted, but don't fail the test if it takes longer
			// This is a cleanup issue, not a functional test failure
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespaceName}, cluster)
				if errors.IsNotFound(err) {
					return true
				}
				if err != nil {
					fmt.Printf("Error getting cluster during cleanup: %v\n", err)
					return false
				}
				// Debug: Print finalizers and status
				fmt.Printf("Cluster still exists. Finalizers: %v, DeletionTimestamp: %v\n", cluster.Finalizers, cluster.DeletionTimestamp)
				if cluster.DeletionTimestamp != nil {
					fmt.Printf("Cluster is marked for deletion but still exists. Checking dependent resources...\n")
					// Check for dependent resources - list StatefulSets, Services, and PVCs
					stsList := &appsv1.StatefulSetList{}
					if err := k8sClient.List(ctx, stsList, client.InNamespace(namespaceName), client.MatchingLabels(map[string]string{"app": clusterName})); err == nil {
						fmt.Printf("Found %d StatefulSets\n", len(stsList.Items))
					}

					svcList := &corev1.ServiceList{}
					if err := k8sClient.List(ctx, svcList, client.InNamespace(namespaceName), client.MatchingLabels(map[string]string{"app": clusterName})); err == nil {
						fmt.Printf("Found %d Services\n", len(svcList.Items))
					}

					pvcList := &corev1.PersistentVolumeClaimList{}
					if err := k8sClient.List(ctx, pvcList, client.InNamespace(namespaceName), client.MatchingLabels(map[string]string{"app": clusterName})); err == nil {
						fmt.Printf("Found %d PVCs\n", len(pvcList.Items))
					}
				}
				return false
			}, time.Second*180, interval).Should(BeTrue(), "Cluster should be deleted within 180 seconds")
		}
	})

	Context("When creating a basic Neo4j Enterprise Cluster", func() {
		It("Should create cluster successfully", func() {
			By("Creating the cluster resource")
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for StatefulSets to be created by the controller")
			Eventually(func() bool {
				primarySts := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-primary",
					Namespace: namespaceName,
				}, primarySts)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying that Services are created")
			Eventually(func() bool {
				// Check for headless service
				headlessService := &corev1.Service{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-headless",
					Namespace: namespaceName,
				}, headlessService)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
