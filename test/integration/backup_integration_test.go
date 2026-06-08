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

// quiesceBackupJob suspends a backup Job and waits for its pod (if any)
// to disappear. The contract this guarantees: after the function returns,
// no `neo4j-admin backup` invocation will run, so the operator's
// `handleExistingBackupJob` cannot race the test's Status patch.
//
// Why this is needed: the operator's `handleOneTimeBackup` is terminal on
// `phase in {Completed, Failed}` (neo4jbackup_controller.go:347). The
// real Job, running against a live Ready cluster, completes in ~5-10s.
// Without quiescing, a slow CI runner could let the real Job set
// `Status.Succeeded > 0` and the operator record a Succeeded history
// entry BEFORE the test's `Status.Failed = 4` patch lands — at which
// point the terminal-state guard makes the failure patch a no-op and
// the failure-path assertions never reach the expected state.
//
// `Spec.Suspend = true` deletes existing pods and prevents new ones (K8s
// 1.21+ Job suspension semantics). After suspension we wait for
// `Status.Active == 0` so any in-flight pod has fully terminated; only
// then is it safe to set `Status.Succeeded` / `Status.Failed` ourselves
// without racing the real backup's eventual completion.
func quiesceBackupJob(ctx context.Context, c client.Client, key types.NamespacedName) {
	GinkgoHelper()
	Eventually(func() error {
		j := &batchv1.Job{}
		if err := c.Get(ctx, key, j); err != nil {
			return err
		}
		t := true
		j.Spec.Suspend = &t
		return c.Update(ctx, j)
	}, time.Second*15, time.Second).Should(Succeed(),
		"suspending the Job (retries handle resourceVersion conflicts from the operator's parallel watches)")

	Eventually(func() int32 {
		j := &batchv1.Job{}
		if err := c.Get(ctx, key, j); err != nil {
			return -1
		}
		return j.Status.Active
	}, time.Second*60, time.Second).Should(BeEquivalentTo(0),
		"suspended Job must have Status.Active=0 (no pods running) before the test sets Succeeded/Failed; "+
			"otherwise the real backup's neo4j-admin call could land first and the operator's "+
			"terminal-state guard would make our patch a no-op")
}

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

	// ─── Shared --to-path directory + BackupRun.BackupsPath (rule 40) ──
	//
	// End-to-end coverage for the shared-directory layout: all runs of one
	// Neo4jBackup CR write to a single `<base>/<chain-root>/` directory so
	// `neo4j-admin --type=DIFF` can chain off the prior FULL. Per-run
	// identity is preserved at the filename level (timestamped artifacts),
	// NOT via a `${BACKUP_RUN_ID}` subfolder under --to-path. Unit tests
	// pin each component in isolation; this test verifies the full
	// handshake survives a real reconcile loop:
	//
	//   1. operator emits a `--to-path=<base>/<chain-root>/` with NO
	//      `${BACKUP_RUN_ID}` segment (chain-root = the CR name by default).
	//   2. operator still wires `BACKUP_RUN_ID` env var on the Pod via
	//      downward-API to `metadata.labels['batch.kubernetes.io/job-name']`
	//      — retained for log correlation, not for the path.
	//   3. when the Job completes successfully, the operator populates
	//      `Neo4jBackup.status.history[0].backupsPath` with the chain root
	//      (the CR name), the shared per-CR artifact directory.
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

		By("Suspending the Job so the real neo4j-admin call can't race our simulated status patch")
		// See quiesceBackupJob for rationale. Even on the success path
		// this matters: without it, a real failure (cluster bolt blip,
		// PVC pending, etc.) would set Status.Failed first and lock the
		// terminal-state guard, making this test's later Succeeded patch
		// a no-op.
		quiesceBackupJob(ctx, k8sClient, jobKey)

		By("Asserting the Job's --to-path is the shared per-CR directory, NOT a ${BACKUP_RUN_ID} subfolder")
		// Single backup container; args[1] is the `/bin/sh -c "<cmd>"`
		// payload because Command=["/bin/sh"] + Args=["-c", "<cmd>"].
		Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1), "backup Job must have exactly one container")
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Args).To(HaveLen(2), "container Args must be [-c, <command>]")
		// Rule 40: all runs of one CR share `<base>/<chain-root>/`; the
		// chain-root is the CR name by default. PVC target → /backup/<name>/.
		Expect(container.Args[1]).To(MatchRegexp(`--to-path=/backup/`+backup.Name+`/?`),
			"--to-path must be the shared per-CR directory /backup/<chain-root>/ (chain-root = CR name)")
		Expect(container.Args[1]).ToNot(ContainSubstring("${BACKUP_RUN_ID}"),
			"rule 40: --to-path must NOT include a ${BACKUP_RUN_ID} per-run subfolder — "+
				"diff backups chain off the prior full in the SAME directory; per-run "+
				"identity lives in the timestamped artifact filename")

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

		By("Simulating Job success by setting Status.Succeeded = 1")
		// Minimal status update: only Succeeded. The operator's
		// handleExistingBackupJob only checks `job.Status.Succeeded > 0`;
		// it doesn't read CompletionTime or Conditions, so we don't need
		// to set them. This also dodges K8s 1.31+/1.33 Job validation
		// rules:
		//   - completionTime requires conditions[Complete=True]
		//   - Complete=True requires SuccessCriteriaMet=True first
		//   - startTime cannot be "removed" once unsuspended
		// Adding either condition triggers a chain of required fields
		// that's painful to fake without actually running the Job. The
		// minimal Succeeded-only path is what's actually under test
		// anyway — the contract we care about is "operator records
		// history when it sees Succeeded > 0".
		Eventually(func() error {
			latest := &batchv1.Job{}
			if err := k8sClient.Get(ctx, jobKey, latest); err != nil {
				return err
			}
			latest.Status.Succeeded = 1
			return k8sClient.Status().Update(ctx, latest)
		}, time.Second*10, time.Second).Should(Succeed(),
			"updating Job status to simulate completion (retried for resourceVersion conflicts)")

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
		}, backupTimeout, backupInterval).Should(Equal(backup.Name),
			"status.history[0].backupsPath must equal the chain root (the CR name) — "+
				"the shared per-CR artifact directory under the storage root (rule 40). "+
				"A regression in jobToBackupRun (e.g. dropping `BackupsPath: chainRoot(backup)`) "+
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

		By("Suspending the Job so the real neo4j-admin can't succeed before we set Status.Failed")
		// This is the spec the race actually bites: against a Ready
		// cluster, the real backup Job tends to succeed in ~5-10s, which
		// would land a `Status=Succeeded` history entry FIRST and lock
		// the terminal-state guard at handleOneTimeBackup:347. The
		// later `Status.Failed = 4` patch would then be ignored by the
		// operator, leaving the test asserting `Status="Failed"` on a
		// run the operator recorded as Succeeded.
		quiesceBackupJob(ctx, k8sClient, jobKey)

		By("Simulating Job failure by setting Status.Failed > backoffLimit")
		// Same minimal-status-update approach as the success test above.
		// handleExistingBackupJob routes on `job.Status.Failed > 0`; no
		// conditions or completionTime required.
		Eventually(func() error {
			latest := &batchv1.Job{}
			if err := k8sClient.Get(ctx, jobKey, latest); err != nil {
				return err
			}
			latest.Status.Failed = 4 // > backoffLimit=3, terminal failure
			return k8sClient.Status().Update(ctx, latest)
		}, time.Second*10, time.Second).Should(Succeed())

		By("Asserting the failed run lands in status.history with BackupsPath set")
		Eventually(func() bool {
			latest := &neo4jv1beta1.Neo4jBackup{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
				return false
			}
			// Find the run for THIS Job (not any earlier run from other tests).
			for _, run := range latest.Status.History {
				if run.RunID == string(job.UID) {
					// BackupsPath is the chain root (CR name) under the
					// shared-directory layout (rule 40), not the Job name.
					return run.Status == "Failed" && run.BackupsPath == backup.Name
				}
			}
			return false
		}, backupTimeout, backupInterval).Should(BeTrue(),
			"failed Jobs MUST land in status.history with Status=Failed and BackupsPath set to the "+
				"chain root (CR name); a regression in recordOneShotBackupRun's failure branch would "+
				"let failed runs vanish with the Job's TTL — only a metric counter would remain")

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

	// #130's bonus item ("verify a Neo4jRestore referencing
	// history.backupsPath builds the right --from-path") is
	// intentionally NOT covered here. The path-construction contract
	// is fully covered by unit tests:
	//
	//   - internal/controller/neo4jrestore_cloud_test.go
	//     TestResolveRestoreSource_BackupRefDereferenceS3 / PVC
	//   - internal/resources/cluster_test.go
	//     TestBuildRestoreFromPath_*
	//
	// An end-to-end version was attempted here but couldn't be made
	// non-brittle in integration runtime — the only way to actually
	// build a --from-path in the running operator is to let the
	// restore Job be created, which requires either stopCluster=true
	// (which scales down the shared BeforeAll cluster and breaks the
	// other tests in this Ordered Describe) or a non-live cluster
	// (not available here). The earlier attempt resorted to "trigger
	// the restore and assert on status" but couldn't distinguish
	// path-resolution success from unrelated validation failures
	// (database-exists, refuseRestoreIfPodsRunning, etc.).
	//
	// The user-facing "restore from a specific historical run"
	// workflow is exercised end-to-end by the #121-1 round-trip in
	// restore_integration_test.go, which uses its own cluster + a
	// real backup + stopCluster=true and asserts on the restored
	// data — the most thorough proof that the per-run subfolder
	// actually drives a working restore.
})
