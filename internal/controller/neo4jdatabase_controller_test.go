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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
)

var _ = Describe("Neo4jDatabase Controller", func() {
	Context("When reconciling a database with missing cluster reference", func() {
		It("should handle missing referenced cluster gracefully", func() {
			ctx := context.Background()

			By("Creating a database with non-existent cluster reference")
			database := &neo4jv1alpha1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-orphan-db",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jDatabaseSpec{
					ClusterRef: "non-existent-cluster",
					Name:       "orphandb",
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Reconciling the database with missing cluster")
			controllerReconciler := &controller.Neo4jDatabaseReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-orphan-db",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred()) // Should not error when cluster is missing

			// Cleanup
			Expect(k8sClient.Delete(ctx, database)).To(Succeed())
		})
	})
})
