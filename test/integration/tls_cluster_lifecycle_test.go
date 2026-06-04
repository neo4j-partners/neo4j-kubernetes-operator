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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// End-to-end coverage for the strict-by-default intra-cluster TLS posture
// introduced by spec.tls.strictPeerValidation (PR #127). The existing
// cluster_lifecycle_test only exercises tls.mode=disabled; this is the
// first integration test that actually puts the strict path through
// cert-manager → operator → STS → Pod → mutual TLS cluster formation.
//
// What the unit tests can't cover and this one does:
//   - cert-manager actually issues a Secret with ca.crt populated.
//   - The operator's Secret items[] projection lands ca.crt at
//     /ssl/trusted/ca.crt on the running pod.
//   - Pods successfully complete the strict-mode mutual TLS handshake
//     (trust_all=false + client_auth=REQUIRE + verify_hostname=true)
//     against each other and the cluster reaches Ready.
//   - No legacy trust_all=true leakage in the rendered config.
var _ = Describe("TLS Cluster Lifecycle (strict peer validation, default)", func() {
	var (
		namespaceName string
		clusterName   string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
	)

	BeforeEach(func() {
		namespaceName = createTestNamespace("tls-strict")
		clusterName = fmt.Sprintf("tls-strict-%d", GinkgoRandomSeed())

		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: namespaceName},
			StringData: map[string]string{"username": "neo4j", "password": "admin123"},
		}
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())
	})

	AfterEach(func() {
		if cluster != nil {
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			if err := k8sClient.Delete(ctx, cluster); err != nil && !errors.IsNotFound(err) {
				GinkgoWriter.Printf("Failed to delete cluster: %v\n", err)
			}
		}
		cleanupCustomResourcesInNamespace(namespaceName)
	})

	It("forms a 3-server cluster with strict peer validation against ca-cluster-issuer", SpecTimeout(25*time.Minute), func(ctx SpecContext) {
		if !isOperatorRunning() {
			Skip("strict TLS lifecycle test requires the operator to be running in-cluster")
		}

		// strictPeerValidation is left UNSET so the kubebuilder default
		// (true) is what the cluster actually runs against — this is the
		// scenario every user hits unless they explicitly opt out.
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespaceName},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:     neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:      &neo4jv1beta1.AuthSpec{AuthenticationProviders: []string{"native"}, AdminSecret: "neo4j-admin-secret"},
				Topology:  neo4jv1beta1.TopologyConfiguration{Servers: 3},
				Storage:   neo4jv1beta1.StorageSpec{ClassName: "standard", Size: "1Gi"},
				Resources: getCIAppropriateResourceRequirements(),
				TLS: &neo4jv1beta1.TLSSpec{
					Mode:      "cert-manager",
					IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
				},
				Env: []corev1.EnvVar{{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"}},
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		By("Waiting for status.phase=Ready (cert-manager Secret + strict cluster SSL must both work)")
		Eventually(func() string {
			fresh := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespaceName}, fresh); err != nil {
				return ""
			}
			return fresh.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"))

		By("Verifying the rendered ConfigMap emits the strict cluster SSL block")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName + "-config", Namespace: namespaceName}, cm)).To(Succeed())
		conf := cm.Data["neo4j.conf"]
		Expect(conf).To(ContainSubstring("dbms.ssl.policy.cluster.trust_all=false"))
		Expect(conf).To(ContainSubstring("dbms.ssl.policy.cluster.client_auth=REQUIRE"))
		Expect(conf).To(ContainSubstring("dbms.ssl.policy.cluster.verify_hostname=true"))
		// Legacy posture must NOT appear anywhere in the rendered config.
		Expect(conf).NotTo(ContainSubstring("dbms.ssl.policy.cluster.trust_all=true"))

		By("Verifying server pod logs contain no legacy trust_all marker and no TLS handshake failures")
		clientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		podList := &corev1.PodList{}
		Expect(k8sClient.List(ctx, podList,
			client.InNamespace(namespaceName),
			client.MatchingLabels{"app.kubernetes.io/instance": clusterName, "app.kubernetes.io/component": "database"})).To(Succeed())
		Expect(podList.Items).NotTo(BeEmpty(), "expected at least one server pod for the cluster")

		var tail int64 = 500
		// Patterns we expect ABSENT under strict-mode happy path:
		//   - "trust_all=true": the legacy debug-only posture marker. The
		//     operator must not be emitting it under the default field
		//     value.
		//   - "SSLHandshakeException" / "handshake_failure" / "PKIX": JSSE
		//     symptoms of a failed mutual TLS handshake on intra-cluster
		//     traffic.
		forbidden := []string{"trust_all=true", "SSLHandshakeException", "handshake_failure", "PKIX path"}
		for _, pod := range podList.Items {
			data, err := clientset.CoreV1().Pods(namespaceName).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: "neo4j",
				TailLines: &tail,
			}).Do(ctx).Raw()
			Expect(err).NotTo(HaveOccurred(), "failed to read logs for pod %s", pod.Name)
			body := string(data)
			for _, needle := range forbidden {
				Expect(strings.Contains(body, needle)).To(BeFalse(),
					"pod %s logs must not contain %q (last %d lines)", pod.Name, needle, tail)
			}
		}
	})
})
