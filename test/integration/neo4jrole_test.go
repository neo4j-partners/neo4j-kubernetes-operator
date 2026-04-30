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

// These tests verify Neo4jRole end-to-end:
//  1. A Neo4jRole with explicit privileges creates the role and applies the
//     desired set, observable via SHOW ROLE PRIVILEGES.
//  2. Manually revoking one of the desired privileges out-of-band causes the
//     controller to re-apply it on the next reconcile (drift correction).
//  3. Deleting the Neo4jRole drops the role from Neo4j (deletionPolicy
//     defaults to Delete).

var _ = Describe("Neo4jRole end-to-end", func() {
	const (
		testTimeout = time.Second * 600
	)

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
		role        *neo4jv1beta1.Neo4jRole
		clusterName string
		adminPass   string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		adminPass = randomPassword(18)
		namespaceName := createTestNamespace("role-e2e")
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
		clusterName = fmt.Sprintf("role-%d", time.Now().Unix())

		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: namespaceName},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte(adminPass),
			},
		}
		Expect(k8sClient.Create(testCtx, adminSecret)).To(Succeed())
	})

	AfterEach(func() {
		if role != nil {
			if len(role.GetFinalizers()) > 0 {
				role.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, role)
			}
			_ = k8sClient.Delete(testCtx, role)
			role = nil
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

	It("creates a role, reverts privilege drift, and drops on delete", SpecTimeout(testTimeout), func(ctx SpecContext) {
		By("Creating a 2-server cluster")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Topology:  neo4jv1beta1.TopologyConfiguration{Servers: getCIAppropriateClusterSize(2)},
				Resources: getCIAppropriateResourceRequirements(),
				Storage:   neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "1Gi"},
				Auth: &neo4jv1beta1.AuthSpec{
					AuthenticationProviders: []string{"native"},
					AdminSecret:             "neo4j-admin-secret",
				},
				TLS: &neo4jv1beta1.TLSSpec{Mode: "disabled"},
				Env: []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
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

		By("Creating Neo4jRole 'analytics_reader' with explicit privileges")
		role = &neo4jv1beta1.Neo4jRole{
			ObjectMeta: metav1.ObjectMeta{Name: "analytics-reader", Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jRoleSpec{
				ClusterRef:        clusterName,
				Name:              "analytics_reader",
				EnforcePrivileges: true,
				Privileges: []string{
					"GRANT ACCESS ON DATABASE neo4j TO analytics_reader",
					"GRANT MATCH {*} ON GRAPH neo4j NODES * TO analytics_reader",
				},
			},
		}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())

		By("Waiting for Neo4jRole status.phase=Ready")
		Eventually(func() string {
			r := &neo4jv1beta1.Neo4jRole{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-reader", Namespace: namespace.Name}, r); err != nil {
				return ""
			}
			return r.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		podName := fmt.Sprintf("%s-server-0", clusterName)

		By("Verifying privileges are visible via SHOW ROLE PRIVILEGES")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW ROLE analytics_reader PRIVILEGES YIELD action, access RETURN action, access",
			)
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, clusterTimeout, interval).Should(SatisfyAll(
			ContainSubstring("access"),
			ContainSubstring("GRANTED"),
		))

		By("Waiting for the role controller to fully settle before injecting drift")
		Eventually(func(g Gomega) {
			r := &neo4jv1beta1.Neo4jRole{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-reader", Namespace: namespace.Name}, r)).To(Succeed())
			g.Expect(r.Status.Phase).To(Equal("Ready"))
			g.Expect(r.Status.ObservedGeneration).To(Equal(r.Generation))
			g.Expect(r.Status.AppliedPrivileges).To(HaveLen(len(r.Spec.Privileges)))
		}, clusterTimeout, interval).Should(Succeed())

		// Small pause to let any in-flight reconcile finish before issuing the
		// manual REVOKE — concurrent privilege writes on the same role can
		// fail with a transaction conflict on the system database.
		time.Sleep(5 * time.Second)

		By("Manually revoking ACCESS to simulate drift")
		cmd := exec.CommandContext(ctx, "kubectl", "exec",
			podName, "-n", namespace.Name, "--",
			"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
			"REVOKE ACCESS ON DATABASE neo4j FROM analytics_reader",
		)
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "cypher-shell REVOKE failed; output: %s", string(out))

		By("Waiting for the operator to re-apply the GRANT (drift reconciliation)")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW ROLE analytics_reader PRIVILEGES YIELD action WHERE action = 'access' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW ROLE PRIVILEGES failed: %v; output: %s\n", err, string(out))
				return false
			}
			// Count > 0 — the access privilege has been re-granted.
			text := string(out)
			return strings.Contains(text, "\n1\n") || strings.Contains(text, "\n2\n")
		}, clusterTimeout, interval).Should(BeTrue(), "controller must re-apply revoked privilege")

		By("Deleting the Neo4jRole CR")
		Expect(k8sClient.Delete(ctx, role)).To(Succeed())
		role = nil

		By("Waiting for the role to disappear from SHOW ROLES")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW ROLES YIELD role WHERE role = 'analytics_reader' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW ROLES failed: %v; output: %s\n", err, string(out))
				return false
			}
			return strings.Contains(string(out), "\n0\n")
		}, clusterTimeout, interval).Should(BeTrue(), "DROP ROLE must remove analytics_reader from SHOW ROLES")
	})
})
