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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

var _ = Describe("Server Role Hints Integration", func() {
	const testTimeout = time.Second * 300

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1alpha1.Neo4jEnterpriseCluster
		clusterName string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		namespaceName := createTestNamespace("role-hints")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
		}

		clusterName = fmt.Sprintf("role-hints-%d", time.Now().Unix())

		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: namespaceName,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(testCtx, adminSecret)).To(Succeed())
	})

	AfterEach(func() {
		if cluster != nil {
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, cluster)
			}
			_ = k8sClient.Delete(testCtx, cluster)
			cluster = nil
		}
		if namespace != nil {
			cleanupCustomResourcesInNamespace(namespace.Name)
			_ = k8sClient.Delete(testCtx, namespace)
			namespace = nil
		}
	})

	Context("ServerModeConstraint propagation", func() {
		It("should write initial.server.mode_constraint=PRIMARY to the config ConfigMap", SpecTimeout(testTimeout), func(ctx SpecContext) {
			By("Creating cluster with ServerModeConstraint=PRIMARY")
			cluster = &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1alpha1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers:              getCIAppropriateClusterSize(2),
						ServerModeConstraint: "PRIMARY",
					},
					Resources: getCIAppropriateResourceRequirements(),
					Storage: neo4jv1alpha1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Auth: &neo4jv1alpha1.AuthSpec{
						AuthenticationProviders: []string{"native"},
						AdminSecret:             "neo4j-admin-secret",
					},
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "disabled",
					},
					Env: []corev1.EnvVar{
						{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"},
					},
				},
			}
			applyCIOptimizations(cluster)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			configMapKey := types.NamespacedName{
				Name:      fmt.Sprintf("%s-config", clusterName),
				Namespace: namespace.Name,
			}

			By("Waiting for the config ConfigMap to be created")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, configMapKey, configMap)
			}, testTimeout, interval).Should(Succeed())

			By("Asserting initial.server.mode_constraint=PRIMARY appears in startup.sh")
			// The mode constraint is appended to /tmp/neo4j-config/neo4j.conf at runtime by startup.sh,
			// not in the static neo4j.conf ConfigMap key. Check startup.sh instead.
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, configMapKey, configMap); err != nil {
					return false
				}
				script, ok := configMap.Data["startup.sh"]
				if !ok {
					return false
				}
				GinkgoWriter.Printf("startup.sh snapshot (relevant section):\n")
				for _, line := range strings.Split(script, "\n") {
					if strings.Contains(line, "mode_constraint") {
						GinkgoWriter.Printf("  %s\n", line)
					}
				}
				return strings.Contains(script, "initial.server.mode_constraint=PRIMARY")
			}, testTimeout, interval).Should(BeTrue(),
				"initial.server.mode_constraint=PRIMARY must be present in the ConfigMap's startup.sh")

			By("Asserting the server StatefulSet exists (basic K8s creation smoke test)")
			serverSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-server", clusterName),
					Namespace: namespace.Name,
				}, serverSts)
			}, testTimeout, interval).Should(Succeed())
		})
	})
})
