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

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// These tests verify Neo4jRoleBinding end-to-end. Because the operator
// never creates the user, the test pre-creates an "external" user via
// cypher-shell to simulate SSO/LDAP first-login provisioning, then asserts
// that the binding grants and (on delete) revokes the listed roles.

var _ = Describe("Neo4jRoleBinding end-to-end", func() {
	const (
		testTimeout = time.Second * 600
		adminPass   = "password123"
		extUserPass = "externuserpass"
	)

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
		binding     *neo4jv1beta1.Neo4jRoleBinding
		clusterName string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		namespaceName := createTestNamespace("rb-e2e")
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
		clusterName = fmt.Sprintf("rb-%d", time.Now().Unix())

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
		if binding != nil {
			if len(binding.GetFinalizers()) > 0 {
				binding.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, binding)
			}
			_ = k8sClient.Delete(testCtx, binding)
			binding = nil
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

	It("grants and revokes roles for an externally-provisioned user", SpecTimeout(testTimeout), func(ctx SpecContext) {
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

		podName := fmt.Sprintf("%s-server-0", clusterName)

		By("Pre-creating an external user via cypher-shell (simulating SSO first-login)")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "neo4j", "-p", adminPass,
				fmt.Sprintf("CREATE USER externuser SET PASSWORD '%s' CHANGE NOT REQUIRED", extUserPass),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("cypher-shell CREATE USER failed: %w; output: %s", err, string(out))
			}
			return nil
		}, clusterTimeout, interval).Should(Succeed())

		By("Creating a Neo4jRoleBinding granting reader to externuser")
		binding = &neo4jv1beta1.Neo4jRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "externuser-binding", Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jRoleBindingSpec{
				ClusterRef: clusterName,
				Username:   "externuser",
				Roles:      []string{"reader"},
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		By("Waiting for binding status.phase=Ready")
		Eventually(func() string {
			b := &neo4jv1beta1.Neo4jRoleBinding{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "externuser-binding", Namespace: namespace.Name}, b); err != nil {
				return ""
			}
			return b.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		By("Verifying externuser holds the reader role")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "neo4j", "-p", adminPass,
				"SHOW USERS YIELD user, roles WHERE user = 'externuser' RETURN user, roles",
			)
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, clusterTimeout, interval).Should(SatisfyAll(
			ContainSubstring("externuser"),
			ContainSubstring("reader"),
		))

		By("Confirming GrantedRoles is recorded on status")
		b := &neo4jv1beta1.Neo4jRoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "externuser-binding", Namespace: namespace.Name}, b)).To(Succeed())
		Expect(b.Status.GrantedRoles).To(ContainElement("reader"))

		By("Deleting the binding (deletionPolicy defaults to Revoke)")
		Expect(k8sClient.Delete(ctx, binding)).To(Succeed())
		binding = nil

		By("Waiting for the reader role to be revoked from externuser")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "neo4j", "-p", adminPass,
				"SHOW USERS YIELD user, roles WHERE user = 'externuser' RETURN roles",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW USERS failed: %v; output: %s\n", err, string(out))
				return false
			}
			text := string(out)
			// reader must no longer appear in this user's roles list
			return strings.Contains(text, "externuser") == false ||
				!strings.Contains(text, "reader")
		}, clusterTimeout, interval).Should(BeTrue(), "binding deletion must revoke the reader role")

		By("Cleaning up the externally-created user")
		_, _ = exec.CommandContext(ctx, "kubectl", "exec",
			podName, "-n", namespace.Name, "--",
			"cypher-shell", "-u", "neo4j", "-p", adminPass,
			"DROP USER externuser IF EXISTS",
		).CombinedOutput()
	})

	It("waits in UserNotFound when the user does not exist", SpecTimeout(testTimeout), func(ctx SpecContext) {
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

		By("Creating a binding for a user that doesn't exist")
		binding = &neo4jv1beta1.Neo4jRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "ghost-binding", Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jRoleBindingSpec{
				ClusterRef: clusterName,
				Username:   "ghostuser",
				Roles:      []string{"reader"},
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		By("Waiting for the UserNotFound condition")
		Eventually(func() bool {
			b := &neo4jv1beta1.Neo4jRoleBinding{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "ghost-binding", Namespace: namespace.Name}, b); err != nil {
				return false
			}
			for _, c := range b.Status.Conditions {
				if c.Type == "UserNotFound" && c.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, clusterTimeout, interval).Should(BeTrue(), "binding must surface UserNotFound when the named user is absent")
	})
})
