/*
Copyright 2026.

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
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// These tests verify Neo4jAuthRule end-to-end. ABAC requires Neo4j 2026.03+;
// the tests detect the cluster version via NEO4J_VERSION (the same env var
// used by getNeo4jImageTag) and skip gracefully on older releases.
//
// The OIDC provider listed in dbms.security.abac.authorization_providers
// does NOT need to actually exist for the operator to install the rule —
// Neo4j only validates the provider name at user authentication time, not
// at rule creation. We use a placeholder "test-oidc" so the operator's
// OIDCProviderConfigured precondition passes; we don't actually
// authenticate any users.

var _ = Describe("Neo4jAuthRule end-to-end", func() {
	const testTimeout = time.Second * 600

	var (
		testCtx     context.Context
		namespace   *corev1.Namespace
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
		role        *neo4jv1beta1.Neo4jRole
		authRule    *neo4jv1beta1.Neo4jAuthRule
		clusterName string
		adminPass   string
	)

	BeforeEach(func() {
		testCtx = context.Background()

		if !isOperatorRunning() {
			Skip("Operator must be running in the cluster for integration tests")
		}

		// Skip ABAC scenarios on Neo4j versions that don't support AUTH RULE.
		// Pre-2026.03 clusters cause the rule to sit in AuthRuleVersionTooOld
		// rather than going Ready, which is correct operator behaviour but
		// means the happy-path scenarios below cannot complete.
		if !neo4jVersionSupportsAuthRules() {
			Skip(fmt.Sprintf("ABAC requires Neo4j 2026.03 or later; current NEO4J_VERSION is %q", getNeo4jImageTag()))
		}

		adminPass = randomPassword(18)
		namespaceName := createTestNamespace("abac-e2e")
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
		clusterName = fmt.Sprintf("abac-%d", time.Now().Unix())

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
		if authRule != nil {
			if len(authRule.GetFinalizers()) > 0 {
				authRule.SetFinalizers([]string{})
				_ = k8sClient.Update(testCtx, authRule)
			}
			_ = k8sClient.Delete(testCtx, authRule)
			authRule = nil
		}
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

	It("creates a rule, reverts drift, drops on delete", SpecTimeout(testTimeout), func(ctx SpecContext) {
		By("Creating a 2-server cluster with dbms.security.abac.authorization_providers")
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
				// The provider name does not need to resolve — Neo4j only
				// validates it at auth time. The operator just needs the key
				// present so its OIDCProviderConfigured precondition passes.
				Config: map[string]string{
					"dbms.security.abac.authorization_providers": "test-oidc",
				},
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

		By("Creating an analytics_reader Neo4jRole that the auth rule will grant")
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
		Eventually(func() string {
			r := &neo4jv1beta1.Neo4jRole{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-reader", Namespace: namespace.Name}, r); err != nil {
				return ""
			}
			return r.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		By("Creating a Neo4jAuthRule that grants analytics_reader when 'department' = 'analytics'")
		authRule = &neo4jv1beta1.Neo4jAuthRule{
			ObjectMeta: metav1.ObjectMeta{Name: "analytics-team", Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
				ClusterRef:   clusterName,
				Name:         "analytics_team",
				Condition:    "abac.oidc.user_attribute('department') = 'analytics'",
				GrantedRoles: []string{"analytics_reader"},
				EnforceRoles: true,
			},
		}
		Expect(k8sClient.Create(ctx, authRule)).To(Succeed())

		By("Waiting for the auth rule to reach status.phase=Ready")
		Eventually(func() string {
			r := &neo4jv1beta1.Neo4jAuthRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-team", Namespace: namespace.Name}, r); err != nil {
				return ""
			}
			return r.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		podName := fmt.Sprintf("%s-server-0", clusterName)

		By("Verifying SHOW AUTH RULES reports the rule")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW AUTH RULES YIELD name, condition, enabled, roles "+
					"WHERE name = 'analytics_team' "+
					"RETURN name, condition, enabled, roles",
			)
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, clusterTimeout, interval).Should(SatisfyAll(
			ContainSubstring("analytics_team"),
			ContainSubstring("analytics_reader"),
		))

		By("Waiting for full reconcile settlement before injecting drift")
		Eventually(func(g Gomega) {
			r := &neo4jv1beta1.Neo4jAuthRule{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-team", Namespace: namespace.Name}, r)).To(Succeed())
			g.Expect(r.Status.Phase).To(Equal("Ready"))
			g.Expect(r.Status.ObservedGeneration).To(Equal(r.Generation))
			g.Expect(r.Status.AppliedRoles).To(ContainElement("analytics_reader"))
		}, clusterTimeout, interval).Should(Succeed())
		// Brief settle to let any in-flight reconcile finish before we drop
		// the rule out-of-band.
		time.Sleep(5 * time.Second)

		By("Manually dropping the rule via cypher-shell to simulate drift")
		cmd := exec.CommandContext(ctx, "kubectl", "exec",
			podName, "-n", namespace.Name, "--",
			"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
			"DROP AUTH RULE analytics_team",
		)
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "cypher-shell DROP AUTH RULE failed; output: %s", string(out))

		By("Waiting for the operator to recreate the rule (drift reconciliation)")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW AUTH RULES YIELD name WHERE name = 'analytics_team' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW AUTH RULES failed: %v; output: %s\n", err, string(out))
				return false
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			return strings.TrimSpace(lines[len(lines)-1]) == "1"
		}, clusterTimeout, interval).Should(BeTrue(), "controller must recreate the dropped auth rule")

		By("Manually revoking the role grant to simulate role drift")
		Eventually(func(g Gomega) {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"REVOKE ROLE analytics_reader FROM AUTH RULE analytics_team",
			)
			out, err := cmd.CombinedOutput()
			g.Expect(err).ToNot(HaveOccurred(), "REVOKE failed; output: %s", string(out))
		}, clusterTimeout, interval).Should(Succeed())

		By("Waiting for the operator to re-grant analytics_reader to the rule")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW AUTH RULES YIELD name, roles WHERE name = 'analytics_team' RETURN roles",
			)
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, clusterTimeout, interval).Should(ContainSubstring("analytics_reader"))

		By("Deleting the Neo4jAuthRule CR")
		Expect(k8sClient.Delete(ctx, authRule)).To(Succeed())
		authRule = nil

		By("Waiting for the rule to disappear from SHOW AUTH RULES")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"SHOW AUTH RULES YIELD name WHERE name = 'analytics_team' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW AUTH RULES failed: %v; output: %s\n", err, string(out))
				return false
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			return strings.TrimSpace(lines[len(lines)-1]) == "0"
		}, clusterTimeout, interval).Should(BeTrue(), "DROP AUTH RULE must remove analytics_team from SHOW AUTH RULES")
	})

	It("waits in OIDCProviderConfigured=False when the cluster does not configure ABAC providers", SpecTimeout(testTimeout), func(ctx SpecContext) {
		By("Creating a cluster WITHOUT dbms.security.abac.authorization_providers")
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
		Eventually(func() string {
			c := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace.Name}, c); err != nil {
				return ""
			}
			return c.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		By("Creating a Neo4jAuthRule against the un-configured cluster")
		authRule = &neo4jv1beta1.Neo4jAuthRule{
			ObjectMeta: metav1.ObjectMeta{Name: "no-oidc", Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jAuthRuleSpec{
				ClusterRef:   clusterName,
				Name:         "no_oidc",
				Condition:    "abac.oidc.user_attribute('dept') = 'eng'",
				GrantedRoles: []string{"reader"},
			},
		}
		Expect(k8sClient.Create(ctx, authRule)).To(Succeed())

		By("Waiting for OIDCProviderConfigured=False on the rule's status")
		Eventually(func(g Gomega) {
			r := &neo4jv1beta1.Neo4jAuthRule{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "no-oidc", Namespace: namespace.Name}, r)).To(Succeed())
			g.Expect(r.Status.Phase).To(Equal("Pending"))
			found := false
			for _, c := range r.Status.Conditions {
				if c.Type == "OIDCProviderConfigured" && c.Status == metav1.ConditionFalse {
					found = true
				}
			}
			g.Expect(found).To(BeTrue(), "expected OIDCProviderConfigured=False, got conditions: %v", r.Status.Conditions)
		}, clusterTimeout, interval).Should(Succeed())
	})
})

// neo4jVersionSupportsAuthRules reports whether the Neo4j image tag picked by
// getNeo4jImageTag() satisfies the AUTH RULE feature gate (2026.03+).
func neo4jVersionSupportsAuthRules() bool {
	v, err := neo4j.ParseVersion(getNeo4jImageTag())
	if err != nil || v == nil {
		return false
	}
	return v.SupportsAuthRules()
}
