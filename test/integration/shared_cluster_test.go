/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package integration_test

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// One vanilla native-auth cluster shared by the config-identical, non-mutating
// e2e specs — Neo4jUser, Neo4jRole, Neo4jRoleBinding, Neo4jDatabase. They all
// need the SAME cluster (native auth, TLS off, 2 servers, default image) and only
// operate on the system/user databases; none mutate cluster-level config. Forming
// it ONCE instead of once-per-spec removes ~4 CalVer cluster formations — the
// dominant cost and the source of the per-spec SpecTimeout-vs-formation timeouts.
//
// Safe because the suite runs serially (--procs=1): specs touch the shared system
// DB sequentially, isolated by unique resource names + per-spec cleanup (each spec
// deletes only its own CRs, never the shared cluster). Provisioned lazily by the
// first spec to call useSharedNativeCluster; torn down once by AfterSuite via
// teardownSharedNativeCluster.
var (
	sharedNativeOnce sync.Once
	sharedNativeName string
	sharedNativeNS   string
	sharedNativePass string
)

// useSharedNativeCluster returns the shared cluster's (name, namespace, admin
// password), provisioning it on first call and waiting for phase=Ready. Call from
// a spec's BeforeEach (it uses Gomega assertions). The generous clusterTimeout is
// paid once here, not per spec.
//
// Failure is deliberately fail-fast, NOT retry-per-spec: sync.Once does not re-run
// after the first provisioning attempt (even if it failed), so if formation fails
// the first RBAC spec reports the real Eventually error and the rest fail fast on
// the empty-name guard below, pointing at it. This is intentional — retrying
// formation in each subsequent spec would reintroduce the N× per-spec cluster
// formations (and their CalVer timeouts) that this shared fixture exists to
// eliminate. A genuine "cluster can't form" should fail the group fast, not N×20m.
func useSharedNativeCluster(ctx context.Context) (name, namespace, adminPassword string) {
	sharedNativeOnce.Do(func() {
		ns := createTestNamespace("rbac-shared")
		pass := randomPassword(18)
		clusterName := "rbac-shared-cluster"

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: ns},
			Data:       map[string][]byte{"username": []byte("neo4j"), "password": []byte(pass)},
		})).To(Succeed())

		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
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
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: ns}, c); err != nil {
				return ""
			}
			return c.Status.Phase
		}, clusterTimeout, interval).Should(Equal("Ready"), "shared RBAC cluster must reach Ready")

		sharedNativeName, sharedNativeNS, sharedNativePass = clusterName, ns, pass
	})

	Expect(sharedNativeName).NotTo(BeEmpty(),
		"shared native cluster was not provisioned (formation failed in an earlier spec)")
	return sharedNativeName, sharedNativeNS, sharedNativePass
}

// teardownSharedNativeCluster deletes the shared cluster (clearing finalizers
// first) so its namespace can be removed. Idempotent; called once from AfterSuite
// before cleanupTestNamespaces.
func teardownSharedNativeCluster(ctx context.Context) {
	if sharedNativeName == "" {
		return
	}
	c := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: sharedNativeName, Namespace: sharedNativeNS}, c); err == nil {
		if len(c.GetFinalizers()) > 0 {
			c.SetFinalizers([]string{})
			_ = k8sClient.Update(ctx, c)
		}
		_ = k8sClient.Delete(ctx, c)
	}
	// Best-effort, NON-asserting wait so the namespace delete in
	// cleanupTestNamespaces isn't blocked on the cluster's dependents. This must
	// NOT use a Gomega assertion: this runs in AfterSuite, and a slow
	// finalizer/dependent removal on CalVer or under load would otherwise fail
	// teardown and red a suite whose specs all passed. If the cluster is still
	// around after the window, namespace GC finishes the job — we don't care here.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if k8sClient.Get(ctx, types.NamespacedName{Name: sharedNativeName, Namespace: sharedNativeNS}, &neo4jv1beta1.Neo4jEnterpriseCluster{}) != nil {
			return // gone
		}
		time.Sleep(2 * time.Second)
	}
}
