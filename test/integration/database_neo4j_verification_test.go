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

var _ = Describe("Neo4jDatabase Neo4j-Level Verification", Label("core"), func() {
	const (
		testTimeout = time.Second * 300
	)

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		database    *neo4jv1beta1.Neo4jDatabase
		clusterName string
		adminPass   string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		// Reuse the shared native-auth cluster (see shared_cluster_test.go); its
		// admin password is used for the cypher-shell verification below.
		var nsName string
		clusterName, nsName, adminPass = useSharedNativeCluster(testCtx)
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	})

	AfterEach(func() {
		// Shared cluster torn down in AfterSuite — delete only this spec's CR.
		if database != nil {
			if len(database.GetFinalizers()) > 0 {
				database.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, database)
			}
			_ = k8sClient.Delete(testCtx, database)
			database = nil
		}
	})

	Context("Neo4jDatabase creates database inside Neo4j", func() {
		It("should create 'verifydb' database visible via SHOW DATABASES", SpecTimeout(testTimeout), func(ctx SpecContext) {
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
