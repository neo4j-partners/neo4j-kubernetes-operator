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

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
		reconciler    *Neo4jEnterpriseClusterReconciler
		clusterName   string
		namespaceName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
		namespaceName = "default"

		// Create reconciler
		reconciler = &Neo4jEnterpriseClusterReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(100),
		}

		// Create basic cluster spec
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespaceName,
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
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
			k8sClient.Delete(ctx, cluster, client.PropagationPolicy(metav1.DeletePropagationForeground))

			// Wait for cluster to be deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespaceName}, cluster)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		}
	})

	Context("When creating a basic Neo4j Enterprise Cluster", func() {
		It("Should create cluster successfully", func() {
			By("Creating the cluster resource")
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Reconciling the cluster")
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      clusterName,
					Namespace: namespaceName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})
	})
})
