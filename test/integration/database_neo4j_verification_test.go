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
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

var _ = Describe("Neo4jDatabase Neo4j-Level Verification", func() {
	const (
		testTimeout = time.Second * 300
		adminPass   = "password123"
	)

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
		database    *neo4jv1beta1.Neo4jDatabase
		clusterName string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		namespaceName := createTestNamespace("db-verify")
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
		}

		clusterName = fmt.Sprintf("db-verify-%d", time.Now().Unix())

		// Admin secret — password used for cypher-shell verification below
		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: namespaceName,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte(adminPass),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(testCtx, adminSecret)).To(Succeed())
	})

	AfterEach(func() {
		if database != nil {
			if len(database.GetFinalizers()) > 0 {
				database.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, database)
			}
			_ = k8sClient.Delete(testCtx, database)
			database = nil
		}
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

	Context("Neo4jDatabase creates database inside Neo4j", func() {
		It("should create 'verifydb' database visible via SHOW DATABASES", SpecTimeout(testTimeout), func(ctx SpecContext) {
			By("Creating a 2-server cluster")
			cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace.Name,
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: getCIAppropriateClusterSize(2),
					},
					Resources: getCIAppropriateResourceRequirements(),
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "standard",
						Size:      "1Gi",
					},
					Auth: &neo4jv1beta1.AuthSpec{
						AuthenticationProviders: []string{"native"},
						AdminSecret:             "neo4j-admin-secret",
					},
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "disabled",
					},
					Env: []corev1.EnvVar{
						{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"},
					},
				},
			}
			applyCIOptimizations(cluster)
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			By("Waiting for cluster phase=Ready")
			Eventually(func() string {
				c := &neo4jv1beta1.Neo4jEnterpriseCluster{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace.Name}, c); err != nil {
					return ""
				}
				return c.Status.Phase
			}, clusterTimeout, interval).Should(Equal("Ready"))

			By("Creating Neo4jDatabase 'verifydb'")
			database = &neo4jv1beta1.Neo4jDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "verifydb",
					Namespace: namespace.Name,
				},
				Spec: neo4jv1beta1.Neo4jDatabaseSpec{
					ClusterRef:  clusterName,
					Name:        "verifydb",
					IfNotExists: true,
				},
			}
			Expect(k8sClient.Create(ctx, database)).To(Succeed())

			By("Waiting for Neo4jDatabase status.phase=Ready")
			Eventually(func() string {
				db := &neo4jv1beta1.Neo4jDatabase{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "verifydb", Namespace: namespace.Name}, db); err != nil {
					return ""
				}
				return db.Status.Phase
			}, clusterTimeout, interval).Should(Equal("Ready"),
				"operator must successfully execute CREATE DATABASE via Bolt")

			By("Verifying 'verifydb' is visible via cypher-shell SHOW DATABASES")
			podName := fmt.Sprintf("%s-server-0", clusterName)
			Eventually(func() bool {
				cmd := exec.CommandContext(ctx, "kubectl", "exec",
					podName, "-n", namespace.Name, "--",
					"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
					"SHOW DATABASES YIELD name WHERE name = 'verifydb' RETURN count(*) AS n",
				)
				out, err := cmd.CombinedOutput()
				outStr := string(out)
				GinkgoWriter.Printf("cypher-shell output: %s (err: %v)\n", outStr, err)
				if err != nil {
					return false
				}
				// The output contains a line with the count value.
				// When the database exists the count is 1 (or more for CA entries).
				return strings.Contains(outStr, "\n1\n") || strings.Contains(outStr, "\n2\n")
			}, clusterTimeout, interval).Should(BeTrue(),
				"'verifydb' must be present in SHOW DATABASES output")
		})
	})
})
