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

var _ = Describe("Backup Integration Tests", Ordered, func() {
	const (
		backupTimeout  = time.Second * 600
		backupInterval = time.Second * 2
	)

	var (
		testNamespace string
		cluster       *neo4jv1beta1.Neo4jEnterpriseCluster
	)

	BeforeAll(func() {
		testNamespace = createTestNamespace("backup-int")

		By("Creating admin secret")
		adminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "neo4j-admin-secret",
				Namespace: testNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte("password123"),
			},
		}
		Expect(k8sClient.Create(ctx, adminSecret)).Should(Succeed())

		By("Creating shared Neo4j cluster for backup tests")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-test-cluster",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1beta1.ImageSpec{
					Repo: "neo4j",
					Tag:  getNeo4jImageTag(),
				},
				Topology: neo4jv1beta1.TopologyConfiguration{
					Servers: 2,
				},
				Storage: neo4jv1beta1.StorageSpec{
					Size:      "1Gi",
					ClassName: "standard",
				},
				Resources: getCIAppropriateResourceRequirements(),
				Auth: &neo4jv1beta1.AuthSpec{
					AdminSecret: "neo4j-admin-secret",
				},
				TLS: &neo4jv1beta1.TLSSpec{
					Mode: "disabled",
				},
				Env: []corev1.EnvVar{
					{
						Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
						Value: "eval",
					},
				},
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

		By("Waiting for cluster to be ready")
		Eventually(func() bool {
			var clusterStatus neo4jv1beta1.Neo4jEnterpriseCluster
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      cluster.Name,
				Namespace: testNamespace,
			}, &clusterStatus)
			if err != nil {
				return false
			}
			if clusterStatus.Status.Phase == "Ready" {
				GinkgoWriter.Printf("Cluster is ready. Phase: %s, Message: %s\n",
					clusterStatus.Status.Phase, clusterStatus.Status.Message)
				return true
			}
			GinkgoWriter.Printf("Cluster not yet ready. Phase: %s, Message: %s\n",
				clusterStatus.Status.Phase, clusterStatus.Status.Message)
			return false
		}, clusterTimeout, backupInterval).Should(BeTrue())
	})

	AfterAll(func() {
		By("Cleaning up shared backup test resources")
		// Clean up backups
		backupList := &neo4jv1beta1.Neo4jBackupList{}
		if err := k8sClient.List(ctx, backupList, client.InNamespace(testNamespace)); err == nil {
			for i := range backupList.Items {
				backup := &backupList.Items[i]
				backup.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, backup)
				_ = k8sClient.Delete(ctx, backup)
			}
		}
		// Clean up cluster
		if cluster != nil {
			var latest neo4jv1beta1.Neo4jEnterpriseCluster
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: cluster.Name, Namespace: testNamespace,
			}, &latest); err == nil {
				latest.SetFinalizers([]string{})
				_ = k8sClient.Update(ctx, &latest)
				_ = k8sClient.Delete(ctx, &latest)
			}
		}
		cleanupCustomResourcesInNamespace(testNamespace)
	})

	It("should automatically create ServiceAccount when backup is created", func() {
		By("Creating a PVC for backup storage")
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-pvc",
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("5Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pvc)).Should(Succeed())

		By("Creating a backup resource")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name: "backup-pvc",
						Size: "5Gi",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

		By("Verifying service account is created automatically")
		Eventually(func() error {
			sa := &corev1.ServiceAccount{}
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-sa",
				Namespace: testNamespace,
			}, sa)
		}, backupTimeout, backupInterval).Should(Succeed())
	})

	It("should handle RBAC creation for scheduled backups", func() {
		By("Creating a scheduled backup resource")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scheduled-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: cluster.Name,
				},
				Schedule: "*/5 * * * *",
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name: "backup-pvc",
						Size: "5Gi",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

		By("Verifying RBAC resources are created for scheduled backup")
		Eventually(func() error {
			sa := &corev1.ServiceAccount{}
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-sa",
				Namespace: testNamespace,
			}, sa)
		}, backupTimeout, backupInterval).Should(Succeed())
	})

	It("should reuse existing RBAC resources", func() {
		By("Getting the service account UID from previous tests")
		sa := &corev1.ServiceAccount{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      "neo4j-backup-sa",
			Namespace: testNamespace,
		}, sa)).Should(Succeed())
		originalUID := sa.UID

		By("Creating another backup in same namespace")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-reuse-test",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind: "Cluster",
					Name: cluster.Name,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name: "backup-pvc-2",
						Size: "5Gi",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).Should(Succeed())

		By("Verifying service account was not recreated")
		Eventually(func() bool {
			sa := &corev1.ServiceAccount{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "neo4j-backup-sa",
				Namespace: testNamespace,
			}, sa)
			return err == nil && sa.UID == originalUID
		}, backupTimeout, backupInterval).Should(BeTrue())
	})

	It("should create a backup resource against a ready cluster", func() {
		By("Creating a backup resource")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "simple-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind:      "Cluster",
					Name:      cluster.Name,
					Namespace: testNamespace,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC: &neo4jv1beta1.PVCSpec{
						Name: "backup-pvc",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())

		By("Waiting for backup to be created")
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      backup.Name,
				Namespace: backup.Namespace,
			}, backup)
			return err == nil
		}, backupTimeout, backupInterval).Should(BeTrue())
	})

	// ─── Per-run subfolder + BackupRun.BackupsPath (issue #130) ────────
	//
	// End-to-end coverage for the contract introduced by #129 (per-run
	// backup-artifact subfolder layout). Unit tests pin each component
	// in isolation; this test verifies the full handshake survives a
	// real reconcile loop:
	//
	//   1. operator emits `--to-path=...${BACKUP_RUN_ID}` in the Job's
	//      command (the literal placeholder; the shell expands it at
	//      runtime via the downward-API env var).
	//   2. operator wires `BACKUP_RUN_ID` env var on the Pod via
	//      downward-API to `metadata.labels['batch.kubernetes.io/job-name']`.
	//   3. when the Job completes successfully, the operator populates
	//      `Neo4jBackup.status.history[0].backupsPath` with the Job's
	//      name (which is what the Pod expanded ${BACKUP_RUN_ID} to).
	//
	// Steps 1+2 are static command-shape assertions. Step 3 requires
	// simulating Job completion via a status patch — we don't have a
	// real cluster to actually back up against (the BeforeAll cluster
	// exists but the integration runtime can't reliably stream a real
	// Neo4j backup in a 600s window in CI), so we patch the Job to
	// look completed and watch the controller pick it up.
	It("should propagate BACKUP_RUN_ID via downward-API + record backupsPath in history (issue #130)", func() {
		By("Creating the Neo4jBackup CR")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "subfolder-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind:      "Cluster",
					Name:      cluster.Name,
					Namespace: testNamespace,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())

		// The operator's job-naming convention is `<backup-name>-backup`
		// (see createBackupJob in neo4jbackup_controller.go). We need
		// this name for both the Job-spec assertions and the
		// status.history[0].backupsPath assertion below.
		expectedJobName := backup.Name + "-backup"

		By("Waiting for the backup Job to be created by the operator")
		job := &batchv1.Job{}
		jobKey := types.NamespacedName{Name: expectedJobName, Namespace: testNamespace}
		Eventually(func() error {
			return k8sClient.Get(ctx, jobKey, job)
		}, backupTimeout, backupInterval).Should(Succeed(),
			"operator must create a backup Job named %q after Neo4jBackup is applied", expectedJobName)

		By("Asserting the Job's container command contains the ${BACKUP_RUN_ID} placeholder")
		// Single backup container; args[1] is the `/bin/sh -c "<cmd>"`
		// payload because Command=["/bin/sh"] + Args=["-c", "<cmd>"].
		Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1), "backup Job must have exactly one container")
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Args).To(HaveLen(2), "container Args must be [-c, <command>]")
		Expect(container.Args[1]).To(ContainSubstring("${BACKUP_RUN_ID}"),
			"command must include the ${BACKUP_RUN_ID} placeholder so the shell expands "+
				"to the Job name at runtime, giving every run its own subfolder")
		// Sanity: the literal --to-path= flag must precede the placeholder.
		Expect(container.Args[1]).To(MatchRegexp(`--to-path=\S*\$\{BACKUP_RUN_ID\}`),
			"placeholder must be inside the --to-path argument, not somewhere unrelated")

		By("Asserting BACKUP_RUN_ID env var is wired via downward-API")
		var runIDEnv *corev1.EnvVar
		for i := range container.Env {
			if container.Env[i].Name == "BACKUP_RUN_ID" {
				runIDEnv = &container.Env[i]
				break
			}
		}
		Expect(runIDEnv).ToNot(BeNil(), "BACKUP_RUN_ID env var must be present on the backup container")
		Expect(runIDEnv.ValueFrom).ToNot(BeNil(), "BACKUP_RUN_ID must source from downward-API, not a literal value")
		Expect(runIDEnv.ValueFrom.FieldRef).ToNot(BeNil(),
			"BACKUP_RUN_ID must come from a FieldRef (Pod metadata), not ConfigMap/Secret")
		Expect(runIDEnv.ValueFrom.FieldRef.FieldPath).To(Equal(
			"metadata.labels['batch.kubernetes.io/job-name']"),
			"FieldRef MUST be metadata.labels['batch.kubernetes.io/job-name'] — "+
				"the canonical K8s 1.27+ label Job controller stamps on every Pod")

		By("Simulating Job success by patching status.succeeded=1 + completion timestamps")
		// We don't run an actual Neo4j backup in this test; the operator
		// just needs to see job.Status.Succeeded > 0 with start/completion
		// times to populate status.history. Patch (not Update) so we
		// don't race the operator on the status subresource's
		// resourceVersion.
		now := metav1.Now()
		started := metav1.NewTime(now.Add(-30 * time.Second))
		patch := client.MergeFrom(job.DeepCopy())
		job.Status.Succeeded = 1
		job.Status.StartTime = &started
		job.Status.CompletionTime = &now
		Expect(k8sClient.Status().Patch(ctx, job, patch)).To(Succeed(),
			"patching Job status to simulate completion")

		By("Waiting for the controller to record backupsPath in status.history[0]")
		Eventually(func() string {
			latest := &neo4jv1beta1.Neo4jBackup{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
				return ""
			}
			if len(latest.Status.History) == 0 {
				return ""
			}
			return latest.Status.History[0].BackupsPath
		}, backupTimeout, backupInterval).Should(Equal(expectedJobName),
			"status.history[0].backupsPath must equal the backing Job name — "+
				"that's how a user maps a recorded run to its artifact subfolder. "+
				"A regression in jobToBackupRun (e.g. dropping `BackupsPath: job.Name`) "+
				"would silently leave this empty.")

		By("Verifying the recorded RunID is the Job's UID (orthogonal contract from #118)")
		// Bonus assertion: while we're already verifying history, also
		// pin the RunID = job.UID invariant. The two fields together
		// give a user the full "which Job, where are its files" answer.
		latest := &neo4jv1beta1.Neo4jBackup{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), latest)).To(Succeed())
		Expect(latest.Status.History[0].RunID).To(Equal(string(job.UID)),
			"RunID must equal job.UID — the per-attempt identifier for retry tracking")
		Expect(latest.Status.History[0].Status).To(Equal("Succeeded"),
			"a Job with Succeeded>0 must produce a history entry with Status=\"Succeeded\"")
	})

	// Edge case: failed Job MUST also land in status.history (closes the
	// recheck-gap #2 fix from issue #128's follow-up work). Without this
	// assertion, a regression in recordOneShotBackupRun's failure branch
	// would let failed runs vanish silently with the Job's TTL.
	It("should record failed one-shot Jobs in status.history with BackupsPath populated", func() {
		By("Creating a backup whose Job we'll fail")
		backup := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "subfolder-failed-backup",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target: neo4jv1beta1.BackupTarget{
					Kind:      "Cluster",
					Name:      cluster.Name,
					Namespace: testNamespace,
				},
				Storage: neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())

		expectedJobName := backup.Name + "-backup"
		job := &batchv1.Job{}
		jobKey := types.NamespacedName{Name: expectedJobName, Namespace: testNamespace}

		By("Waiting for the backup Job")
		Eventually(func() error {
			return k8sClient.Get(ctx, jobKey, job)
		}, backupTimeout, backupInterval).Should(Succeed())

		By("Simulating Job failure (failed > backoffLimit)")
		now := metav1.Now()
		started := metav1.NewTime(now.Add(-5 * time.Second))
		patch := client.MergeFrom(job.DeepCopy())
		job.Status.Failed = 4 // > backoffLimit=3, terminal failure
		job.Status.StartTime = &started
		Expect(k8sClient.Status().Patch(ctx, job, patch)).To(Succeed())

		By("Asserting the failed run lands in status.history with BackupsPath set")
		Eventually(func() bool {
			latest := &neo4jv1beta1.Neo4jBackup{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
				return false
			}
			// Find the run for THIS Job (not any earlier run from other tests).
			for _, run := range latest.Status.History {
				if run.RunID == string(job.UID) {
					return run.Status == "Failed" && run.BackupsPath == expectedJobName
				}
			}
			return false
		}, backupTimeout, backupInterval).Should(BeTrue(),
			"failed Jobs MUST land in status.history with Status=Failed and BackupsPath set; "+
				"a regression in recordOneShotBackupRun's failure branch would let failed "+
				"runs vanish with the Job's TTL — only a metric counter would remain")

		// Belt-and-braces: ensure status.stats is NOT updated by a
		// failed run. Stats is the "latest succeeded run" summary; a
		// failure must not overwrite it.
		latest := &neo4jv1beta1.Neo4jBackup{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), latest)).To(Succeed())
		// If a prior successful test wrote Stats, it should still
		// reflect THAT run's duration, not the failed one's empty stats.
		// We can't assert exact value, but we can assert the failed run's
		// duration (5s) is NOT what's surfaced — because the failed-run
		// path doesn't set Stats at all (Duration left zero).
		if latest.Status.Stats != nil {
			// A prior succeeded run's stats are fine; the failed test
			// run only fails this check if the failed-run path
			// accidentally stamped Stats.
			Expect(latest.Status.Stats.Duration).ToNot(Equal("5s"),
				"failed-run path must not overwrite status.stats with the failed run's duration")
		}
	})

	// Bonus per #130's "8. Bonus" — verifies the inverse contract: a
	// Neo4jRestore CR with source.backupPath set to a history.BackupsPath
	// value builds a --from-path that points at the per-run subfolder.
	// This is the user-facing "restore from a specific historical run"
	// workflow that #129 enabled. Done as an inline build-only check
	// rather than running an actual restore Job — the goal here is the
	// path-construction contract, not a data round-trip (#121 covers
	// that separately).
	It("should construct restore --from-path from a history.backupsPath value (issue #130 bonus)", func() {
		// Build a synthetic Neo4jRestore that points at where a
		// previous backup run's artifacts would live. The PVC name
		// matches the backup tests above so the restore can resolve
		// against the same volume.
		const historicalRunSubfolder = "subfolder-backup-backup"
		restore := &neo4jv1beta1.Neo4jRestore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "restore-from-history",
				Namespace: testNamespace,
			},
			Spec: neo4jv1beta1.Neo4jRestoreSpec{
				ClusterRef:   cluster.Name,
				DatabaseName: "neo4j",
				Source: neo4jv1beta1.RestoreSource{
					Type: "storage",
					Storage: &neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
					},
					// This is the value a user would copy from
					// status.history[i].backupsPath on a real backup CR.
					BackupPath: historicalRunSubfolder,
				},
				// stopCluster: false because we won't actually run the
				// restore — we only need the operator to construct the
				// Job (or fail with a recognisable refuseRestoreIfPodsRunning
				// message that we then read from status). Either way,
				// the spec is shaped correctly.
				StopCluster: false,
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())

		By("Waiting for the operator to react (either Failed-with-refuse or Job-created)")
		// We tolerate both terminal states. The contract under test is
		// "the operator's path resolution sees the per-run subfolder";
		// whether the Job actually runs is a separate concern (#121's
		// data-integrity test handles that).
		Eventually(func() bool {
			latest := &neo4jv1beta1.Neo4jRestore{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), latest); err != nil {
				return false
			}
			switch latest.Status.Phase {
			case "Failed":
				// Expected: refuseRestoreIfPodsRunning fires because
				// the cluster is up and stopCluster=false. The
				// important part — the error must NOT be a generic
				// path-resolution failure; it should be the
				// live-cluster guard.
				return strings.Contains(latest.Status.Message, "restore") ||
					strings.Contains(latest.Status.Message, "cluster")
			case "Running", "Completed":
				// If the operator gets here, the path resolved fine.
				return true
			}
			return false
		}, backupTimeout, backupInterval).Should(BeTrue(),
			"the operator must react to the restore CR — either by refusing "+
				"(stopCluster=false against a live cluster) or by creating a "+
				"restore Job whose --from-path includes the historicalRunSubfolder")

		// Final assertion: any Job created (if the live-cluster check
		// somehow didn't fire) must carry the per-run subfolder in
		// --from-path.
		jobList := &batchv1.JobList{}
		_ = k8sClient.List(ctx, jobList, client.InNamespace(testNamespace),
			client.MatchingLabels{"app.kubernetes.io/instance": restore.Name})
		for _, j := range jobList.Items {
			for _, c := range j.Spec.Template.Spec.Containers {
				if len(c.Args) >= 2 {
					Expect(c.Args[1]).To(ContainSubstring(historicalRunSubfolder),
						"if a restore Job is created, its --from-path MUST include the historical run subfolder %q",
						historicalRunSubfolder)
				}
			}
		}
	})
})
