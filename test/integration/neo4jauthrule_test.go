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

	certmgrv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

const (
	oidcStubName = "oidc-stub"
	// nginx terminates TLS on this port and proxies to mock-oauth2-server
	// on localhost:8080 inside the same Pod. Neo4j 2026.04 hard-requires
	// https:// for OIDC URIs, so we cannot expose the plain HTTP port.
	oidcStubTLSPort        = 8443
	oidcStubName_TLSSecret = "oidc-stub-tls"
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

var _ = Describe("Neo4jAuthRule end-to-end", Label("extended"), func() {
	// Each spec creates a fresh 2-server cluster on Neo4j 2026.04, then
	// patches the cluster's spec.config to add the ABAC provider key
	// (which forces a rolling restart). 15 minutes is the realistic
	// budget on a 4-core CI runner: ~5 min initial bootstrap + ~5 min
	// rolling restart + ~5 min for the auth-rule + drift assertions.
	const testTimeout = time.Second * 900

	var (
		testCtx          context.Context
		namespace        *corev1.Namespace
		cluster          *neo4jv1beta1.Neo4jEnterpriseCluster
		role             *neo4jv1beta1.Neo4jRole
		authRule         *neo4jv1beta1.Neo4jAuthRule
		clusterName      string
		adminPass        string
		oidcDiscoveryURL string
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

		// Stand up an in-cluster OIDC discovery stub so the cluster can boot
		// with a real (reachable) `dbms.security.oidc.test-oidc.well_known_discovery_uri`.
		// We never authenticate; Neo4j only needs to fetch the discovery doc
		// + JWKS to consider the provider configured. Without this, setting
		// `dbms.security.abac.authorization_providers` makes Neo4j 2026.04
		// wedge during boot (the rolling-restart approach we tried in earlier
		// commits did not fix this — Neo4j won't accept the provider name
		// without a corresponding `dbms.security.oidc.<name>.*` block).
		oidcDiscoveryURL = setupOIDCStub(testCtx, namespaceName)
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
		// Single-phase boot. The TLS-fronted OIDC stub deployed in BeforeEach
		// is reachable; spec.trustedCASecrets adds the cert-manager-issued CA
		// to Neo4j's JVM truststore so the https:// well_known fetch succeeds.
		// Neo4j 2026.04 hard-requires:
		//   - https:// for every dbms.security.oidc.<name>.* URI (incl.
		//     well_known_discovery_uri, jwks_uri, etc.)
		//   - the OIDC provider name (prefixed `oidc-`) to appear in BOTH
		//     dbms.security.authentication_providers AND
		//     dbms.security.authorization_providers; abac.authorization_providers
		//     is then a subset.
		By("Creating a 2-server cluster with ABAC + OIDC + trustedCASecrets")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace.Name},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Topology:  neo4jv1beta1.TopologyConfiguration{Servers: getCIAppropriateClusterSize(2)},
				Resources: getCIAppropriateResourceRequirements(),
				Storage:   neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "1Gi"},
				Auth: &neo4jv1beta1.AuthSpec{
					AuthenticationProviders: []string{"native", "oidc-test-oidc"},
					AuthorizationProviders:  []string{"native", "oidc-test-oidc"},
					AdminSecret:             "neo4j-admin-secret",
				},
				TLS: &neo4jv1beta1.TLSSpec{Mode: "disabled"},
				Env: []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
				TrustedCASecrets: []neo4jv1beta1.TrustedCASecret{
					{Name: oidcStubName_TLSSecret}, // Secret produced by the cert-manager Certificate
				},
				Config: map[string]string{
					"dbms.security.abac.authorization_providers":            "oidc-test-oidc",
					"dbms.security.oidc.test-oidc.display_name":             "Test OIDC",
					"dbms.security.oidc.test-oidc.well_known_discovery_uri": oidcDiscoveryURL,
					"dbms.security.oidc.test-oidc.audience":                 "neo4j",
					"dbms.security.oidc.test-oidc.client_id":                "neo4j",
				},
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		By("Waiting for cluster phase=Ready")
		lastClusterPhase := ""
		Eventually(func() string {
			c := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace.Name}, c); err != nil {
				GinkgoWriter.Printf("[abac-e2e] get cluster err: %v\n", err)
				return ""
			}
			if c.Status.Phase != lastClusterPhase {
				GinkgoWriter.Printf("[abac-e2e] cluster phase: %q -> %q (gen=%d obsGen=%d)\n",
					lastClusterPhase, c.Status.Phase, c.Generation, c.Status.ObservedGeneration)
				lastClusterPhase = c.Status.Phase
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
		lastRolePhase := "<unset>"
		rolePoll := 0
		Eventually(func() string {
			r := &neo4jv1beta1.Neo4jRole{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-reader", Namespace: namespace.Name}, r); err != nil {
				GinkgoWriter.Printf("[abac-e2e] get role err: %v\n", err)
				return ""
			}
			rolePoll++
			if r.Status.Phase != lastRolePhase || rolePoll == 1 || rolePoll%6 == 0 {
				GinkgoWriter.Printf("[abac-e2e] role analytics-reader (poll #%d) phase=%q gen=%d obsGen=%d appliedPrivs=%d conds=%s\n",
					rolePoll, r.Status.Phase, r.Generation, r.Status.ObservedGeneration, len(r.Status.AppliedPrivileges), conditionsSummary(r.Status.Conditions))
				lastRolePhase = r.Status.Phase
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
		lastRulePhase := "<unset>"
		rulePoll := 0
		Eventually(func() string {
			r := &neo4jv1beta1.Neo4jAuthRule{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "analytics-team", Namespace: namespace.Name}, r); err != nil {
				GinkgoWriter.Printf("[abac-e2e] get authrule err: %v\n", err)
				return ""
			}
			rulePoll++
			if r.Status.Phase != lastRulePhase || rulePoll == 1 || rulePoll%6 == 0 {
				GinkgoWriter.Printf("[abac-e2e] authrule analytics-team (poll #%d) phase=%q gen=%d obsGen=%d appliedRoles=%v conds=%s\n",
					rulePoll, r.Status.Phase, r.Generation, r.Status.ObservedGeneration, r.Status.AppliedRoles, conditionsSummary(r.Status.Conditions))
				lastRulePhase = r.Status.Phase
			}
			return r.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		podName := fmt.Sprintf("%s-server-0", clusterName)

		By("Verifying SHOW AUTH RULES reports the rule")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"CYPHER 25 SHOW AUTH RULES YIELD name, condition, enabled, roles "+
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
			"CYPHER 25 DROP AUTH RULE analytics_team",
		)
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "cypher-shell DROP AUTH RULE failed; output: %s", string(out))

		By("Waiting for the operator to recreate the rule (drift reconciliation)")
		Eventually(func() bool {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"CYPHER 25 SHOW AUTH RULES YIELD name WHERE name = 'analytics_team' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW AUTH RULES failed: %v; output: %s\n", err, string(out))
				return false
			}
			trimmed := strings.TrimSpace(string(out))
			if trimmed == "" {
				return false
			}
			lines := strings.Split(trimmed, "\n")
			return strings.TrimSpace(lines[len(lines)-1]) == "1"
		}, clusterTimeout, interval).Should(BeTrue(), "controller must recreate the dropped auth rule")

		By("Manually revoking the role grant to simulate role drift")
		Eventually(func(g Gomega) {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"CYPHER 25 REVOKE ROLE analytics_reader FROM AUTH RULE analytics_team",
			)
			out, err := cmd.CombinedOutput()
			g.Expect(err).ToNot(HaveOccurred(), "REVOKE failed; output: %s", string(out))
		}, clusterTimeout, interval).Should(Succeed())

		By("Waiting for the operator to re-grant analytics_reader to the rule")
		Eventually(func() string {
			cmd := exec.CommandContext(ctx, "kubectl", "exec",
				podName, "-n", namespace.Name, "--",
				"cypher-shell", "--format", "plain", "-u", "neo4j", "-p", adminPass,
				"CYPHER 25 SHOW AUTH RULES YIELD name, roles WHERE name = 'analytics_team' RETURN roles",
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
				"CYPHER 25 SHOW AUTH RULES YIELD name WHERE name = 'analytics_team' RETURN count(*) AS n",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("cypher-shell SHOW AUTH RULES failed: %v; output: %s\n", err, string(out))
				return false
			}
			trimmed := strings.TrimSpace(string(out))
			if trimmed == "" {
				return false
			}
			lines := strings.Split(trimmed, "\n")
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
		lastNoOIDCPhase := ""
		Eventually(func(g Gomega) {
			r := &neo4jv1beta1.Neo4jAuthRule{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "no-oidc", Namespace: namespace.Name}, r)).To(Succeed())
			if r.Status.Phase != lastNoOIDCPhase {
				GinkgoWriter.Printf("[abac-e2e] authrule no-oidc phase: %q -> %q conds=%s\n",
					lastNoOIDCPhase, r.Status.Phase, conditionsSummary(r.Status.Conditions))
				lastNoOIDCPhase = r.Status.Phase
			}
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

// conditionsSummary renders a one-line summary of status conditions: "Type=Status(Reason)".
// Used in test progress logs so we can see *why* a phase is stuck without dumping full structs.
func conditionsSummary(conds []metav1.Condition) string {
	if len(conds) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		parts = append(parts, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Reason))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// neo4jVersionSupportsAuthRules reports whether the Neo4j image tag picked by
// getNeo4jImageTag() satisfies the AUTH RULE feature gate (2026.03+).
func neo4jVersionSupportsAuthRules() bool {
	v, err := neo4j.ParseVersion(getNeo4jImageTag())
	if err != nil || v == nil {
		return false
	}
	return v.SupportsAuthRules()
}

// setupOIDCStub deploys an in-cluster TLS-fronted mock-oauth2-server in `ns`
// and returns the cluster-internal HTTPS well-known discovery URL for the
// "test-oidc" issuer. The stub is a two-container Pod:
//
//	mock-oauth2-server   — listens on plain HTTP localhost:8080, serves a
//	                       spec-compliant OIDC discovery doc + JWKS; the
//	                       issuer-id is the first URL path segment, so we
//	                       point Neo4j at /test-oidc/...
//	nginx (tls-proxy)    — listens on https:8443, terminates TLS using the
//	                       cert-manager-issued cert, proxies plain HTTP to
//	                       127.0.0.1:8080. Sets X-Forwarded-Proto: https so
//	                       mock-oauth2-server returns https:// URLs in its
//	                       discovery JSON (without this Neo4j boot fails on
//	                       jwks_uri scheme validation).
//
// Why TLS at all? Neo4j 2026.04 hard-requires https:// for every OIDC URI in
// its config. A plain-HTTP stub causes Neo4j to reject the config at parse
// time (before even attempting boot).
//
// The cert-manager Certificate uses the existing ca-cluster-issuer ClusterIssuer
// that the test environment already provisions. Neo4j is told to trust the
// resulting CA via the new spec.trustedCASecrets field on the cluster.
func setupOIDCStub(ctx context.Context, ns string) string {
	// Issue a TLS cert via cert-manager. The Secret produced (`oidc-stub-tls`)
	// contains tls.crt, tls.key, and ca.crt — the cluster references it
	// directly in spec.trustedCASecrets to add the CA to Neo4j's truststore.
	cert := &certmgrv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: oidcStubName_TLSSecret, Namespace: ns},
		Spec: certmgrv1.CertificateSpec{
			SecretName: oidcStubName_TLSSecret,
			Duration:   &metav1.Duration{Duration: 24 * time.Hour},
			IssuerRef: cmmeta.ObjectReference{
				Name: "ca-cluster-issuer",
				Kind: "ClusterIssuer",
			},
			CommonName: fmt.Sprintf("%s.%s.svc.cluster.local", oidcStubName, ns),
			DNSNames: []string{
				oidcStubName,
				fmt.Sprintf("%s.%s", oidcStubName, ns),
				fmt.Sprintf("%s.%s.svc", oidcStubName, ns),
				fmt.Sprintf("%s.%s.svc.cluster.local", oidcStubName, ns),
			},
		},
	}
	Expect(k8sClient.Create(ctx, cert)).To(Succeed())

	// nginx config for the TLS terminator. X-Forwarded-Proto: https makes
	// mock-oauth2-server emit https:// URLs in the discovery doc.
	nginxCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: oidcStubName + "-nginx-cfg", Namespace: ns},
		Data: map[string]string{
			"nginx.conf": fmt.Sprintf(`
events {}
http {
  server {
    listen %d ssl;
    ssl_certificate     /tls/tls.crt;
    ssl_certificate_key /tls/tls.key;
    location / {
      proxy_pass http://127.0.0.1:8080;
      proxy_set_header Host              $host:$server_port;
      proxy_set_header X-Forwarded-Host  $host:$server_port;
      proxy_set_header X-Forwarded-Proto https;
      proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    }
  }
}
`, oidcStubTLSPort),
		},
	}
	Expect(k8sClient.Create(ctx, nginxCfg)).To(Succeed())

	const mockImage = "ghcr.io/navikt/mock-oauth2-server:2.1.10"
	const nginxImage = "nginx:alpine"

	stubPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcStubName,
			Namespace: ns,
			Labels:    map[string]string{"app": oidcStubName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "mock-oauth2",
					Image: mockImage,
					Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
					Env:   []corev1.EnvVar{{Name: "SERVER_PORT", Value: "8080"}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("50m"),
							"memory": resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("512Mi"),
						},
					},
				},
				{
					Name:  "tls-proxy",
					Image: nginxImage,
					Ports: []corev1.ContainerPort{{ContainerPort: oidcStubTLSPort}},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "nginx-cfg", MountPath: "/etc/nginx", ReadOnly: true},
						{Name: "tls", MountPath: "/tls", ReadOnly: true},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt(oidcStubTLSPort),
							},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       2,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "nginx-cfg",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: oidcStubName + "-nginx-cfg"},
						},
					},
				},
				{
					Name: "tls",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: oidcStubName_TLSSecret,
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, stubPod)).To(Succeed())

	stubSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: oidcStubName, Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": oidcStubName},
			Ports: []corev1.ServicePort{{
				Name:       "https",
				Port:       oidcStubTLSPort,
				TargetPort: intstr.FromInt(oidcStubTLSPort),
			}},
		},
	}
	Expect(k8sClient.Create(ctx, stubSvc)).To(Succeed())

	By("Waiting for OIDC stub Pod to be Ready (cert-manager Cert + nginx + mock-oauth2)")
	Eventually(func() bool {
		p := &corev1.Pod{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: oidcStubName, Namespace: ns}, p); err != nil {
			return false
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				GinkgoWriter.Printf("[abac-e2e] OIDC stub ready (mock=%s, tls=%s)\n", mockImage, nginxImage)
				return true
			}
		}
		return false
	}, 3*time.Minute, 2*time.Second).Should(BeTrue(), "OIDC stub pod did not become Ready within 3m")

	return fmt.Sprintf("https://%s.%s.svc.cluster.local:%d/test-oidc/.well-known/openid-configuration",
		oidcStubName, ns, oidcStubTLSPort)
}
