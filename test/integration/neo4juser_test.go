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

// These tests verify Neo4jUser end-to-end against a live cluster:
//  1. A Neo4jUser created with a password Secret + role binding ends up
//     visible via SHOW USERS with the right roles.
//  2. Rotating the password Secret causes the controller to re-issue
//     ALTER USER SET PASSWORD; status.passwordLastRotated advances.
//  3. Deleting the Neo4jUser drops the user from Neo4j (deletionPolicy
//     defaults to Delete).

var _ = Describe("Neo4jUser end-to-end", func() {
	const (
		testTimeout = time.Second * 600
		adminPass   = "password123"
		userPass    = "userpass456"
		newUserPass = "rotatedpass789"
	)

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
		user        *neo4jv1beta1.Neo4jUser
		creds       *corev1.Secret
		clusterName string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		namespaceName := createTestNamespace("user-e2e")
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
		clusterName = fmt.Sprintf("user-%d", time.Now().Unix())

		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: namespaceName},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte(adminPass),
			},
		}
		Expect(k8sClient.Create(testCtx, adminSecret)).To(Succeed())

		creds = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appuser-creds", Namespace: namespaceName},
			Data:       map[string][]byte{"password": []byte(userPass)},
		}
		Expect(k8sClient.Create(testCtx, creds)).To(Succeed())
	})

	AfterEach(func() {
		if user != nil {
			if len(user.GetFinalizers()) > 0 {
				user.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, user)
			}
			_ = k8sClient.Delete(testCtx, user)
			user = nil
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

	It("creates, rotates and drops a user", SpecTimeout(testTimeout), func(ctx SpecContext) {
		By("Creating a 2-server cluster")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Topology: neo4jv1beta1.TopologyConfiguration{
					Servers: getCIAppropriateClusterSize(2),
				},
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

		By("Creating a Neo4jUser bound to the reader role")
		user = &neo4jv1beta1.Neo4jUser{
			ObjectMeta: metav1.ObjectMeta{Name: "appuser", Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jUserSpec{
				ClusterRef:        clusterName,
				Username:          "appuser",
				PasswordSecretRef: &neo4jv1beta1.SecretKeyRef{Name: "appuser-creds"},
				Roles:             []string{"reader"},
			},
		}
		Expect(k8sClient.Create(ctx, user)).To(Succeed())

		By("Waiting for Neo4jUser status.phase=Ready")
		Eventually(func() string {
			u := &neo4jv1beta1.Neo4jUser{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "appuser", Namespace: namespace.Name}, u); err != nil {
				return ""
			}
			return u.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		By("Verifying SHOW USERS via cypher-shell")
		podName := fmt.Sprintf("%s-server-0", clusterName)
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "neo4j", "-p", adminPass,
				"SHOW USERS YIELD user, roles WHERE user = 'appuser' RETURN user, roles",
			)
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, clusterTimeout, interval).Should(SatisfyAll(
			ContainSubstring("appuser"),
			ContainSubstring("reader"),
		))

		By("Verifying the appuser can authenticate with the original password")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "appuser", "-p", userPass,
				"RETURN 1",
			)
			_, err := cmd.CombinedOutput()
			return err
		}, clusterTimeout, interval).Should(Succeed())

		By("Capturing initial passwordSecretHash")
		initial := &neo4jv1beta1.Neo4jUser{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "appuser", Namespace: namespace.Name}, initial)).To(Succeed())
		Expect(initial.Status.PasswordSecretHash).ToNot(BeEmpty())
		initialHash := initial.Status.PasswordSecretHash

		By("Rotating the password Secret")
		rotated := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "appuser-creds", Namespace: namespace.Name}, rotated)).To(Succeed())
		rotated.Data["password"] = []byte(newUserPass)
		Expect(k8sClient.Update(ctx, rotated)).To(Succeed())

		By("Waiting for the controller to apply the new password")
		Eventually(func() string {
			u := &neo4jv1beta1.Neo4jUser{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "appuser", Namespace: namespace.Name}, u); err != nil {
				return ""
			}
			return u.Status.PasswordSecretHash
		}, clusterTimeout, interval).ShouldNot(Equal(initialHash))

		By("Verifying the appuser can authenticate with the new password")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "appuser", "-p", newUserPass,
				"RETURN 1",
			)
			_, err := cmd.CombinedOutput()
			return err
		}, clusterTimeout, interval).Should(Succeed())

		By("Deleting the Neo4jUser CR")
		Expect(k8sClient.Delete(ctx, user)).To(Succeed())
		user = nil // AfterEach should not double-delete

		By("Waiting for the user to disappear from SHOW USERS")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "-u", "neo4j", "-p", adminPass,
				"SHOW USERS YIELD user WHERE user = 'appuser' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			text := string(out)
			// User is gone when the count line is "0" and the username does
			// not appear anywhere in the output.
			return strings.Contains(text, "\n0\n") && !strings.Contains(text, "appuser\n")
		}, clusterTimeout, interval).Should(BeTrue(), "DROP USER must remove appuser from SHOW USERS")
	})
})
