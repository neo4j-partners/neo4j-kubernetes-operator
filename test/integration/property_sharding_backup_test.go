/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package integration_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Property Sharding Backup Integration Tests cover Phase 1 of issue #138:
// first-class backup support for property-sharded databases via the
// `target.kind: ShardedDatabase` enum value on Neo4jBackup. Same gating
// as the rest of the property-sharding suite — CI-skipped (resource cost)
// and version-gated to 2025.12+.
//
// Run locally:
//
//	NEO4J_VERSION=2025.12-enterprise ginkgo run -focus "Property Sharding Backup" ./test/integration
var _ = Describe("Property Sharding Backup Integration Tests", Label("extended"), Serial, func() {
	const (
		clusterReadyTimeout   = 10 * time.Minute
		shardedDBReadyTimeout = 10 * time.Minute
		backupJobTimeout      = 10 * time.Minute
		pollInterval          = 5 * time.Second
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
		shardedDB     *neo4jv1beta1.Neo4jShardedDatabase
		backup        *neo4jv1beta1.Neo4jBackup
		backupPVC     *corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		if isRunningInCI() {
			Skip("Skipping property sharding backup tests in CI - resource requirements too large")
		}
		if !isPropertyShardingCompatible() {
			Skip("Skipping property sharding backup tests: requires Neo4j 2025.12+ (Infinigraph)")
		}

		testNamespace = createTestNamespace("property-sharding-backup")

		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
			Type: corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

		// Pre-create the backup target PVC so the backup Job has somewhere to
		// write — the operator does not auto-provision the artifact PVC for
		// PVC-type storage targets, only the temp staging PVC.
		backupPVC = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sharded-backup-pvc",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: func() *string { s := "standard"; return &s }(),
			},
		}
		Expect(k8sClient.Create(ctx, backupPVC)).To(Succeed())

		SetDefaultEventuallyTimeout(300 * time.Second)
		SetDefaultEventuallyPollingInterval(pollInterval)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespaceDiagnostics(testNamespace)
		}
		if backup != nil {
			if len(backup.GetFinalizers()) > 0 {
				backup.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, backup)
			}
			_ = k8sClient.Delete(ctx, backup)
			backup = nil
		}
		if shardedDB != nil {
			if len(shardedDB.GetFinalizers()) > 0 {
				shardedDB.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, shardedDB)
			}
			_ = k8sClient.Delete(ctx, shardedDB)
			shardedDB = nil
		}
		if cluster != nil {
			if len(cluster.GetFinalizers()) > 0 {
				cluster.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, cluster)
			}
			_ = k8sClient.Delete(ctx, cluster)
			cluster = nil
		}
		if testNamespace != "" {
			cleanupCustomResourcesInNamespace(testNamespace)
		}
	})

	Context("when backing up a property-sharded database", func() {
		It("emits a single neo4j-admin invocation with quoted glob and per-shard artifacts", func() {
			By("Creating a property-sharding-enabled cluster")
			cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-host-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{
						Repo: "neo4j",
						Tag:  getNeo4jImageTag(),
					},
					Auth: &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 3,
					},
					Storage: neo4jv1beta1.StorageSpec{
						Size:      "1Gi",
						ClassName: "standard",
					},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
							corev1.ResourceCPU:    resource.MustParse("2000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("8Gi"),
							corev1.ResourceCPU:    resource.MustParse("2000m"),
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

			Eventually(func() string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				return cluster.Status.Phase
			}, clusterReadyTimeout, pollInterval).Should(Equal("Ready"))

			Eventually(func() bool {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				return cluster.Status.PropertyShardingReady != nil && *cluster.Status.PropertyShardingReady
			}, clusterReadyTimeout, pollInterval).Should(BeTrue())

			By("Creating the sharded database 'products'")
			shardedDB = &neo4jv1beta1.Neo4jShardedDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "products",
					Namespace: testNamespace,
				},
				Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
					ClusterRef:            cluster.Name,
					Name:                  "products",
					DefaultCypherLanguage: "25",
					PropertySharding: neo4jv1beta1.PropertyShardingConfiguration{
						PropertyShards: 2,
						GraphShard: neo4jv1beta1.DatabaseTopology{
							Primaries: 1,
						},
						PropertyShardTopology: neo4jv1beta1.PropertyShardTopology{
							Replicas: 1,
						},
					},
					Wait:        true,
					IfNotExists: func() *bool { v := true; return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, shardedDB)).To(Succeed())

			Eventually(func() bool {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
				return shardedDB.Status.ShardingReady != nil && *shardedDB.Status.ShardingReady
			}, shardedDBReadyTimeout, pollInterval).Should(BeTrue())

			By("Creating a Neo4jBackup with target.kind=ShardedDatabase")
			backup = &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "products-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:       neo4jv1beta1.BackupTargetKindShardedDatabase,
						Name:       "products",
						ClusterRef: cluster.Name,
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1beta1.PVCSpec{
							Name: backupPVC.Name,
						},
					},
					// F4: opt into the post-backup neo4j-admin validate
					// step so we can assert the Validation field gets
					// populated (alongside the F3 ShardArtifacts filename
					// parsing that runs unconditionally for sharded).
					Options: &neo4jv1beta1.BackupOptions{
						Validate: func() *bool { v := true; return &v }(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			By("Waiting for the backup Job to be created")
			jobName := backup.Name + "-backup"
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: testNamespace}, job)
			}, 2*time.Minute, pollInterval).Should(Succeed())

			By("Verifying the Job's neo4j-admin invocation uses the quoted glob")
			Expect(job.Spec.Template.Spec.Containers).ToNot(BeEmpty())
			containerCmd := strings.Join(job.Spec.Template.Spec.Containers[0].Command, " ") + " " +
				strings.Join(job.Spec.Template.Spec.Containers[0].Args, " ")
			Expect(containerCmd).To(ContainSubstring(`"products*"`),
				"neo4j-admin database backup must be invoked with the quoted glob 'products*'")
			Expect(containerCmd).To(ContainSubstring("--remote-address-resolution=true"),
				"sharded backups must default --remote-address-resolution to true on 2025.09+ (this image is 2025.12+)")

			By("Waiting for the backup Job to complete successfully")
			Eventually(func() int32 {
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: testNamespace}, job)
				return job.Status.Succeeded
			}, backupJobTimeout, pollInterval).Should(BeNumerically(">", 0))

			By("Verifying the backup CR records a Succeeded history entry")
			Eventually(func() string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
				return backup.Status.Phase
			}, 2*time.Minute, pollInterval).Should(Equal("Completed"))

			Expect(backup.Status.History).ToNot(BeEmpty(),
				"status.history must record the sharded backup run")
			Expect(backup.Status.History[0].Status).To(Equal("Succeeded"))

			By("Verifying BackupRun.ShardArtifacts records the per-shard manifest (Phase 3b)")
			Expect(backup.Status.History[0].ShardArtifacts).To(HaveLen(3),
				"expected 1 graph + 2 property shards in the manifest")
			gotShards := make([]string, 0, 3)
			for _, a := range backup.Status.History[0].ShardArtifacts {
				gotShards = append(gotShards, a.ShardName)
			}
			Expect(gotShards).To(ConsistOf("products-g000", "products-p000", "products-p001"))

			By("Verifying Neo4jShardedDatabase.status.lastBackup was reverse-populated (Phase 3a)")
			Eventually(func() *neo4jv1beta1.ShardedDatabaseBackupReference {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(shardedDB), shardedDB)
				return shardedDB.Status.LastBackup
			}, 2*time.Minute, pollInterval).ShouldNot(BeNil(),
				"sharded DB CR status.lastBackup must be populated after a Succeeded sharded backup run")
			Expect(shardedDB.Status.LastBackup.BackupRef).To(Equal(backup.Name))
			Expect(shardedDB.Status.LastBackup.RunID).ToNot(BeEmpty(),
				"lastBackup.runID is the Job UID; must not be empty")

			By("Verifying F3 — per-shard Filename + Size populated from Pod logs")
			// ShardArtifacts may take a few polls to acquire Filename data
			// since fetching Pod logs runs inside recordOneShotBackupRun
			// AFTER the status.history append. Use Eventually so we don't
			// race the parser.
			Eventually(func() bool {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
				if len(backup.Status.History) == 0 {
					return false
				}
				for _, a := range backup.Status.History[0].ShardArtifacts {
					if a.Filename == "" {
						return false
					}
				}
				return true
			}, 3*time.Minute, pollInterval).Should(BeTrue(),
				"every ShardArtifact must have Filename populated by F3 Pod-log parsing")
			// Spot-check filename pattern + a non-zero Size (real backups
			// are well over a byte, so Size==0 would indicate a parse miss).
			for _, a := range backup.Status.History[0].ShardArtifacts {
				Expect(a.Filename).To(MatchRegexp(`^`+a.ShardName+`-.+\.backup$`),
					"Filename must match `<shardName>-<timestamp>.backup` pattern; got %q for %q",
					a.Filename, a.ShardName)
				// Size is best-effort — only the "(NNN bytes)" log format
				// is parsed. neo4j-admin may not emit that for PVC
				// backups; treat zero as acceptable but log it loudly so a
				// CI run that finds non-zero values can be celebrated.
				if a.Size == 0 {
					GinkgoT().Logf("F3: ShardArtifact %q has Size=0 (neo4j-admin didn't emit '(N bytes)' for this shard)", a.ShardName)
				}
			}

			By("Verifying F4 — neo4j-admin backup validate output captured in BackupRun.Validation")
			Eventually(func() *neo4jv1beta1.BackupValidationResult {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
				if len(backup.Status.History) == 0 {
					return nil
				}
				return backup.Status.History[0].Validation
			}, 3*time.Minute, pollInterval).ShouldNot(BeNil(),
				"BackupRun.Validation must be populated when spec.options.validate=true")
			validation := backup.Status.History[0].Validation
			// A backup that JUST succeeded should validate cleanly — every
			// shard is at the same transaction the backup captured. Accept
			// OK or Unknown (parser tolerance for log-format variations
			// between neo4j-admin versions); only treat Degraded as a real
			// signal of trouble.
			Expect(validation.OverallStatus).To(BeElementOf("OK", "Unknown"),
				"freshly-Succeeded backup should validate to OK or Unknown (parser-tolerant); got %q with RawOutput=%q",
				validation.OverallStatus, validation.RawOutput)
		})

		It("rejects a sharded backup against a cluster without propertySharding.enabled", func() {
			By("Creating a NON-sharding cluster")
			cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-sharding-cluster",
					Namespace: testNamespace,
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
					Auth:  &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
					Topology: neo4jv1beta1.TopologyConfiguration{
						Servers: 2, // Minimum cluster size
					},
					Storage: neo4jv1beta1.StorageSpec{Size: "1Gi", ClassName: "standard"},
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
							corev1.ResourceCPU:    resource.MustParse("500m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("4Gi"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			Eventually(func() string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
				return cluster.Status.Phase
			}, clusterReadyTimeout, pollInterval).Should(Equal("Ready"))

			By("Creating a sharded backup targeting that cluster")
			backup = &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "should-fail-backup",
					Namespace: testNamespace,
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:       neo4jv1beta1.BackupTargetKindShardedDatabase,
						Name:       "products",
						ClusterRef: cluster.Name,
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: backupPVC.Name},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			By("Backup should be routed to Failed with a sharding-disabled message")
			Eventually(func() string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)
				return backup.Status.Phase
			}, 2*time.Minute, pollInterval).Should(Equal("Failed"))
			Expect(backup.Status.Message).To(ContainSubstring("property sharding"))
		})
	})
})
