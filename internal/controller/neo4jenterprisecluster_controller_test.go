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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/controller"
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
				Image: neo4jv1alpha1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Servers: 5, // 3 + 2 total servers
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

				// If cluster is stuck with finalizers, force remove them
				if cluster.DeletionTimestamp != nil && len(cluster.Finalizers) > 0 {
					fmt.Printf("Cluster is stuck with finalizers: %v, forcing removal\n", cluster.Finalizers)
					cluster.Finalizers = []string{}
					if err := k8sClient.Update(ctx, cluster); err != nil {
						fmt.Printf("Failed to remove finalizers: %v\n", err)
					}
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
			}, time.Second*60, interval).Should(BeTrue(), "Cluster should be deleted within 60 seconds")
		}
	})

	Context("When creating a basic Neo4j Enterprise Cluster", func() {
		It("Should create cluster successfully", func() {
			By("Creating the cluster resource")
			Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

			By("Waiting for StatefulSets to be created by the controller")
			Eventually(func() bool {
				serverSts := &appsv1.StatefulSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      clusterName + "-server",
					Namespace: namespaceName,
				}, serverSts)
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

// Unit tests for resource version conflict retry logic
var _ = Describe("Neo4jEnterpriseClusterReconciler Resource Version Conflict Handling", func() {
	var (
		ctx        context.Context
		reconciler *controller.Neo4jEnterpriseClusterReconciler
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		sts        *appsv1.StatefulSet
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create test cluster
		cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1alpha1.ImageSpec{
					Repo: "neo4j",
					Tag:  "5.26-enterprise",
				},
				Topology: neo4jv1alpha1.TopologyConfiguration{
					Servers: 3, // 3 + 0 total servers
				},
			},
		}

		// Create test StatefulSet
		sts = &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: int32Ptr(3),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "neo4j"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "neo4j"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "neo4j",
								Image: "neo4j:5.26-enterprise",
							},
						},
					},
				},
			},
		}
	})

	Context("when testing retry logic with mock client", func() {
		It("should retry on resource version conflicts and eventually succeed", func() {
			// Create a mock client that fails twice then succeeds
			mockClient := &mockConflictClient{
				failCount:      2,
				currentAttempt: 0,
			}

			reconciler = &controller.Neo4jEnterpriseClusterReconciler{
				Client:       mockClient,
				Scheme:       k8sClient.Scheme(),
				RequeueAfter: 30 * time.Second,
			}

			// This should succeed after 1 retry due to template comparison optimization
			// Template comparison logic prevents unnecessary updates during cluster formation
			err := reconciler.CreateOrUpdateResource(ctx, sts, cluster)
			Expect(err).ToNot(HaveOccurred())
			// With template comparison, we expect fewer attempts since unnecessary template updates are skipped
			Expect(mockClient.currentAttempt).To(BeNumerically(">=", 1))
			Expect(mockClient.currentAttempt).To(BeNumerically("<=", 3))
		})

		It("should fail after max retries exceeded", func() {
			// Create a StatefulSet that should trigger template updates (different image)
			// This will bypass the template comparison optimization
			stsWithDifferentImage := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "neo4j"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "neo4j"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "neo4j",
									Image: "neo4j:2025.01.0-enterprise", // Different image to trigger template change
								},
							},
						},
					},
				},
			}

			// Create a mock client that always fails
			mockClient := &mockConflictClient{
				failCount:      100, // Always fail
				currentAttempt: 0,
			}

			reconciler = &controller.Neo4jEnterpriseClusterReconciler{
				Client:       mockClient,
				Scheme:       k8sClient.Scheme(),
				RequeueAfter: 30 * time.Second,
			}

			// This should fail after max retries
			err := reconciler.CreateOrUpdateResource(ctx, stsWithDifferentImage, cluster)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsConflict(err)).To(BeTrue())
			// Default retry count is 5, so should attempt at least 5 times
			Expect(mockClient.currentAttempt).To(BeNumerically(">=", 5))
		})
	})
})

// Helper functions and mocks

func int32Ptr(i int32) *int32 {
	return &i
}

// mockConflictClient simulates resource version conflicts
type mockConflictClient struct {
	failCount      int
	currentAttempt int
}

func (m *mockConflictClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	// Return NotFound for first attempt, then return the object for updates
	if m.currentAttempt == 0 {
		return errors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "statefulsets"}, key.Name)
	}
	// For subsequent attempts, simulate an existing object with a template that would be different
	// from the desired template to ensure critical changes are detected
	if sts, ok := obj.(*appsv1.StatefulSet); ok {
		sts.SetResourceVersion("test-version")
		// Simulate an existing StatefulSet with an old image that's different from the desired one
		sts.Spec = appsv1.StatefulSetSpec{
			Replicas: int32Ptr(3),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "neo4j"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "neo4j"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "neo4j",
							Image: "neo4j:5.26.0-enterprise", // Different from the desired 2025.01.0
						},
					},
				},
			},
		}
		// Set a status to indicate all replicas are ready (stable cluster)
		sts.Status = appsv1.StatefulSetStatus{
			ReadyReplicas: 3, // All replicas ready, so template changes should be allowed
		}
	}
	return nil
}

func (m *mockConflictClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	m.currentAttempt++
	if m.currentAttempt <= m.failCount {
		return errors.NewConflict(
			schema.GroupResource{Group: "apps", Resource: "statefulsets"},
			obj.GetName(),
			nil,
		)
	}
	return nil // Success
}

func (m *mockConflictClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	m.currentAttempt++
	if m.currentAttempt <= m.failCount {
		return errors.NewConflict(
			schema.GroupResource{Group: "apps", Resource: "statefulsets"},
			obj.GetName(),
			nil,
		)
	}
	return nil // Success
}

func (m *mockConflictClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) Status() client.StatusWriter {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) Scheme() *runtime.Scheme {
	return k8sClient.Scheme()
}

func (m *mockConflictClient) RESTMapper() meta.RESTMapper {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) SubResource(subResource string) client.SubResourceClient {
	return nil // Not implemented for this test
}

func (m *mockConflictClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil // Not implemented for this test
}

func (m *mockConflictClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return true, nil // Not implemented for this test
}
