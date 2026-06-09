/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package integration_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Property Sharding CI smoke test — minimum viable shape that fits inside
// GitHub-hosted ubuntu-latest runners (~5Gi usable RAM).
//
// Sizing rationale (verified empirically; see commit history):
//   - 2 servers × 1.5Gi memory limit = 3Gi total. Below 1.5Gi Neo4j Enterprise
//     itself refuses (rule 11 in CLAUDE.md establishes 1.5Gi as the Enterprise
//     CI floor).
//   - PropertyShards: 1 + GraphShard.Primaries: 1 = 2 shards total. Each
//     server hosts one shard.
//   - 250m CPU per pod (sharding minimum is 1 core but Kubernetes is
//     tolerant of bursts above request; the relax env var on the operator
//     disables the validator's 1-core hard reject).
//
// The richer sharded tests (property_sharding_backup_test.go,
// property_sharding_minio_restore_test.go, property_sharding_pvc_seed_test.go)
// exercise multi-shard topology (PropertyShards: 2 or higher) which needs
// the production 4Gi/server floor — they stay CI-skipped.
//
// What this smoke test PROVES:
//   - The operator can stand up a property-sharded cluster.
//   - `Neo4jShardedDatabase` reaches `shardingReady=true` with 1+1 shards.
//   - A sharded backup (target.kind=ShardedDatabase) completes.
//
// What this smoke test does NOT cover (intentionally — local-only tests do):
//   - PVC seed proxy / restore via seedBackupRef (needs F3 filename capture).
//   - Validate output parsing (F4).
//   - replaceExisting destructive restore.
//   - Multi-property-shard topology.
//
// Operator must be deployed with `NEO4J_SHARDING_RELAX_MEMORY_MIN=true` —
// applied via the integration-test kustomize overlay.
var _ = Describe("Property Sharding CI Smoke Test", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout = 10 * time.Minute
		shardedReadyTimeout = 5 * time.Minute
		backupJobTimeout    = 5 * time.Minute
		pollInterval        = 5 * time.Second
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		shardedDB     *neo4jv1beta1.Neo4jShardedDatabase
		backup        *neo4jv1beta1.Neo4jBackup
	)

	BeforeEach(func() {
		if !isPropertyShardingCompatible() {
			Skip("Skipping CI sharding smoke test: requires Neo4j 2025.12+")
		}
		testNamespace = createTestNamespace("sharding-smoke")

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: testNamespace},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		})).To(Succeed())

		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(pollInterval)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespaceDiagnostics(testNamespace)
		}
		var toClean []client.Object
		if backup != nil {
			toClean = append(toClean, backup)
		}
		if shardedDB != nil {
			toClean = append(toClean, shardedDB)
		}
		if cluster != nil {
			toClean = append(toClean, cluster)
		}
		for _, cr := range toClean {
			if len(cr.GetFinalizers()) > 0 {
				cr.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, cr)
			}
			_ = k8sClient.Delete(ctx, cr)
		}
		cluster, shardedDB, backup = nil, nil, nil
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	It("stands up a minimal sharded cluster + DB + backup at the CI floor", func() {
		By("Creating a 2-server property-sharding cluster at CI sizing (1.5Gi limit)")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "smoke-cluster", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Auth:     &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				Topology: neo4jv1beta1.TopologyConfiguration{Servers: 2},
				Storage:  neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
				// Sharded CI sizing: same memory as the non-sharded CI helper
				// (1Gi req / 1.5Gi limit) but BUMP CPU limit to 500m. Sharding
				// has more bootstrap work to do and 100m CPU under throttling
				// takes >10 min to form the cluster. 500m is the empirically-
				// verified floor (see commit history) that fits 2 servers
				// within GitHub-hosted runner's 2-core budget.
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1.5Gi"),
					},
				},
				PropertySharding: &neo4jv1beta1.PropertyShardingSpec{
					Enabled: true,
					Config: map[string]string{
						"internal.dbms.sharded_property_database.enabled":                     "true",
						"db.query.default_language":                                           "CYPHER_25",
						"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
			return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
		}, clusterReadyTimeout, pollInterval).Should(BeTrue(),
			"cluster.status.propertyShardingReady=true required; if false, the operator validator is likely rejecting the CI sizing (NEO4J_SHARDING_RELAX_MEMORY_MIN missing from operator env?)")

		By("Creating a minimal sharded DB: 1 graph shard + 1 property shard")
		shardedDB = &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				ClusterRef:            cluster.Name,
				Name:                  "products",
				DefaultCypherLanguage: "25",
				PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
					PropertyShards:        1,
					GraphShard:            neo4jv1beta1.DatabaseTopology{Primaries: 1},
					PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{Replicas: 1},
				},
				Wait:        true,
				IfNotExists: func() *bool { v := true; return &v }(),
			},
		}
		Expect(k8sClient.Create(ctx, shardedDB)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
			return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady
		}, shardedReadyTimeout, pollInterval).Should(BeTrue(),
			"sharded DB must reach shardingReady=true; status.message=%q", shardedDB.Status.Message)

		By("Backing up the sharded DB to a PVC")
		backup = &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "products-backup", Namespace: testNamespace},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: neo4jv1beta1.BackupTargetKindShardedDatabase, Name: "products", ClusterRef: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name:             "products-backup-pvc",
						Size:             "1Gi",
						StorageClassName: "standard",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())
		Eventually(func() string {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
			return backup.Status.Phase
		}, backupJobTimeout, pollInterval).Should(Equal("Completed"),
			"sharded backup must Complete; status.message=%q", backup.Status.Message)
		Expect(backup.Status.History).ToNot(BeEmpty())
		Expect(backup.Status.History[0].Status).To(Equal("Succeeded"))
		// Two shards (1 graph + 1 property) should appear in the manifest.
		Expect(backup.Status.History[0].ShardArtifacts).To(HaveLen(2),
			"backup should record 2 shard artifacts (1 graph + 1 property); got %+v",
			backup.Status.History[0].ShardArtifacts)
	})
})
