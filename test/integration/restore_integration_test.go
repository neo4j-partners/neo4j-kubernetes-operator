/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Integration coverage for Neo4jRestore — closes issue #121 (test
// coverage gap). Three Describe blocks:
//
//   1. "Restore refuses live cluster" — stopCluster=false against a
//      Ready cluster must surface a clear refusal in status.phase,
//      not a silently-succeeding restore that writes to nowhere.
//      Lightweight (~30s).
//
//   2. "Restore overlap guard" — when one restore has claimed the
//      cluster (via the neo4j.com/restore-in-progress annotation),
//      a second restore on the same cluster must fail with the
//      "already in progress" message. Pre-seeds the annotation so
//      the test doesn't race on the first restore's reconcile.
//      Lightweight (~60s).
//
//   3. "Data-integrity round-trip" — Bolt CREATE sentinel → backup →
//      Bolt DELETE sentinel → restore with stopCluster=true → assert
//      STS scales 0 then back up → Bolt SHOW sentinel returns 1.
//      Catches both #117 failure modes simultaneously: the
//      cluster-controller fight on STS replicas (test fails at
//      "scale back up") and the EmptyDir-data-loss bug (test fails
//      at the final SHOW marker assertion). Heavy (~10 min); each
//      Eventually capped at backupTimeout so the whole spec fits
//      inside the 15-min envtest budget.
//
// Test #4 from the issue ("mid-restore CR delete cleans up
// annotation") is intentionally skipped here — it's
// timing-dependent in a way that's flaky on slow CI runners and the
// finalizer cleanup is covered by unit tests in
// neo4jrestore_coordination_test.go. Worth revisiting once we have
// dedicated cluster-coordination integration infrastructure.

package integration_test

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// restoreInProgressAnnotation duplicates the constant from
// internal/controller/neo4jrestore_controller.go to avoid importing
// the controller package into a test (we already pull api/v1beta1
// for the CRDs; the annotation key is conceptually part of the API
// contract). Keep this in sync with RestoreInProgressAnnotation in
// the controller.
const restoreInProgressAnnotation = "neo4j.com/restore-in-progress"

// dumpRestoreDiagnostics is invoked from DeferCleanup when the
// round-trip spec fails. It prints, in order: the restore Job(s)
// and their Pod(s) (kubectl get + describe), the restore Pod logs
// (the actual neo4j-admin output — the whole point of this helper),
// and recent namespace events. Output goes to GinkgoWriter so it
// shows up in the failure dump and CI logs. Everything is
// best-effort; we never fail the test from inside this helper.
func dumpRestoreDiagnostics(ns, restoreName string) {
	GinkgoWriter.Printf("\n========== RESTORE FAILURE DIAGNOSTICS (%s/%s) ==========\n", ns, restoreName)

	run := func(args ...string) string {
		out, _ := exec.Command("kubectl", args...).CombinedOutput()
		return string(out)
	}

	GinkgoWriter.Printf("\n--- Jobs in namespace ---\n%s", run("get", "jobs", "-n", ns, "-o", "wide"))
	GinkgoWriter.Printf("\n--- Pods in namespace ---\n%s", run("get", "pods", "-n", ns, "-o", "wide"))

	// Restore Job Pods carry the label "job-name=<restoreName>-restore"
	// (operator names the Job `<restoreName>-restore`). Capture logs
	// from every container so an init-container failure doesn't hide.
	jobName := restoreName + "-restore"
	GinkgoWriter.Printf("\n--- Describe job/%s ---\n%s", jobName,
		run("describe", "job", jobName, "-n", ns))
	GinkgoWriter.Printf("\n--- Logs from job/%s (all containers, tail=400) ---\n%s", jobName,
		run("logs", "-n", ns, "-l", "job-name="+jobName, "--all-containers=true", "--tail=400"))

	// Also dump cluster STS state — blocker #3 is "cluster doesn't
	// return to Ready after stopCluster cycle", so if the restore
	// itself succeeded but verification failed, we want to see the
	// STS replicas and any pod-level errors.
	GinkgoWriter.Printf("\n--- StatefulSets ---\n%s", run("get", "sts", "-n", ns))

	// Recent events — surfaces scheduling failures, PVC issues, etc.
	GinkgoWriter.Printf("\n--- Recent events ---\n%s",
		run("get", "events", "-n", ns, "--sort-by=.lastTimestamp"))

	GinkgoWriter.Printf("\n========== END DIAGNOSTICS ==========\n\n")
}

var _ = Describe("Restore Integration Tests", Ordered, func() {
	const (
		restoreTimeout  = time.Second * 600
		restoreInterval = time.Second * 3
		// Admin password used by the operator's auth Secret and by
		// kubectl-exec cypher-shell sessions below.
		adminPass = "password123"
	)

	var (
		ns          string
		clusterName string
		cluster     *neo4jv1beta1.Neo4jEnterpriseCluster
	)

	BeforeAll(func() {
		ns = createTestNamespace("restore-int")
		clusterName = "restore-test-cluster"

		By("Creating admin secret")
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "neo4j-admin-secret", Namespace: ns},
			Type:       corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte("neo4j"),
				"password": []byte(adminPass),
			},
		})).To(Succeed())

		By("Creating shared cluster for restore integration tests")
		cluster = &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: getNeo4jImageTag()},
				Topology: neo4jv1beta1.TopologyConfiguration{
					Servers: 2,
				},
				Storage: neo4jv1beta1.StorageSpec{
					Size:      "1Gi",
					ClassName: "standard",
				},
				Resources: getCIAppropriateResourceRequirements(),
				Auth:      &neo4jv1beta1.AuthSpec{AdminSecret: "neo4j-admin-secret"},
				TLS:       &neo4jv1beta1.TLSSpec{Mode: "disabled"},
				Env: []corev1.EnvVar{
					{Name: "NEO4J_ACCEPT_LICENSE_AGREEMENT", Value: "eval"},
				},
			},
		}
		applyCIOptimizations(cluster)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		By("Waiting for cluster Ready")
		Eventually(func() string {
			latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: clusterName, Namespace: ns,
			}, latest); err != nil {
				return ""
			}
			return latest.Status.Phase
		}, clusterTimeout, restoreInterval).Should(Equal("Ready"))

		By("Creating shared backup PVC")
		Expect(k8sClient.Create(ctx, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-backup-pvc", Namespace: ns},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("5Gi"),
					},
				},
			},
		})).To(Succeed())
	})

	AfterAll(func() {
		By("Cleaning up restore-test cluster + CRs")
		_ = removeFinalizersAndDelete(&neo4jv1beta1.Neo4jRestoreList{}, ns)
		_ = removeFinalizersAndDelete(&neo4jv1beta1.Neo4jBackupList{}, ns)
		if cluster != nil {
			latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: clusterName, Namespace: ns,
			}, latest); err == nil {
				latest.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, latest)
				_ = k8sClient.Delete(ctx, latest)
			}
		}
		cleanupCustomResourcesInNamespace(ns)
	})

	// ─── #121-2: Refuse restore against a live cluster ──────────────────
	Context("Restore refuses live cluster", func() {
		It("should refuse a stopCluster=false restore against running pods (issue #121-2)", func() {
			// We don't need a real backup to exist for this test — the
			// refusal fires BEFORE the operator tries to resolve the
			// backup source. But we DO need force=true so the flow
			// gets past `checkDatabaseExists` (which would otherwise
			// reject because the cluster's auto-created `neo4j`
			// database already exists, producing a database-exists
			// error instead of the live-cluster guard error we're
			// testing for).
			restore := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "refuse-live", Namespace: ns},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef:   clusterName,
					DatabaseName: "neo4j",
					StopCluster:  false, // the dangerous knob
					Force:        true,  // skip checkDatabaseExists so refuseRestoreIfPodsRunning is the failure
					Source: neo4jv1beta1.RestoreSource{
						Type:       "storage",
						BackupPath: "doesnt-matter.backup",
						Storage: &neo4jv1beta1.StorageLocation{
							Type: "pvc",
							PVC:  &neo4jv1beta1.PVCSpec{Name: "restore-backup-pvc"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			By("Waiting for status.phase=Failed")
			Eventually(func() string {
				latest := &neo4jv1beta1.Neo4jRestore{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), latest); err != nil {
					return ""
				}
				return latest.Status.Phase
			}, time.Second*60, restoreInterval).Should(Equal("Failed"),
				"a stopCluster=false restore against a Ready cluster must terminal-fail; "+
					"the alternative — running the restore into a fresh PVC or EmptyDir "+
					"while the cluster's real data sits untouched — was the silent-loss "+
					"bug from issue #117")

			By("Asserting the error message names the live-cluster guard")
			latest := &neo4jv1beta1.Neo4jRestore{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), latest)).To(Succeed())
			// The message from refuseRestoreIfPodsRunning is fixed-text;
			// pinning a substring catches the case where the operator
			// surfaces an unrelated error and we accidentally pass.
			Expect(latest.Status.Message).To(ContainSubstring("cannot run against a live cluster"),
				"status.message must surface the refuseRestoreIfPodsRunning guard, "+
					"not a generic validation failure")
			Expect(latest.Status.Message).To(ContainSubstring("stopCluster=true"),
				"the message should also tell the user the fix — set spec.stopCluster=true")
		})
	})

	// ─── #121-3: Overlap guard ────────────────────────────────────────
	Context("Restore overlap guard", func() {
		It("should refuse a second restore when the cluster annotation is held by another (issue #121-3)", func() {
			// Pre-seed the cluster's restore-in-progress annotation as
			// if some FIRST restore had already claimed it. This
			// removes the race window where the test second-restore
			// might be reconciled before the first restore's
			// annotation lands. The annotation is the public contract
			// the controller checks; setting it directly produces the
			// same effect as a real concurrent restore.
			By("Pre-setting the restore-in-progress annotation on the cluster")
			Eventually(func() error {
				latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: clusterName, Namespace: ns,
				}, latest); err != nil {
					return err
				}
				if latest.Annotations == nil {
					latest.Annotations = map[string]string{}
				}
				latest.Annotations[restoreInProgressAnnotation] = "phantom-restore-a"
				return k8sClient.Update(ctx, latest)
			}, time.Second*10, restoreInterval).Should(Succeed())

			defer func() {
				// Clear the annotation so subsequent tests can run.
				latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: clusterName, Namespace: ns,
				}, latest); err == nil {
					delete(latest.Annotations, restoreInProgressAnnotation)
					_ = k8sClient.Update(ctx, latest)
				}
			}()

			By("Creating a second restore that should refuse")
			restoreB := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "overlap-second", Namespace: ns},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef:   clusterName,
					DatabaseName: "neo4j",
					StopCluster:  true, // path that calls setRestoreInProgressAnnotation
					Force:        true, // skip checkDatabaseExists so the flow reaches the annotation conflict
					Source: neo4jv1beta1.RestoreSource{
						Type:       "storage",
						BackupPath: "doesnt-matter.backup",
						Storage: &neo4jv1beta1.StorageLocation{
							Type: "pvc",
							PVC:  &neo4jv1beta1.PVCSpec{Name: "restore-backup-pvc"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, restoreB)).To(Succeed())

			By("Waiting for the second restore to terminal-fail")
			Eventually(func() string {
				latest := &neo4jv1beta1.Neo4jRestore{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(restoreB), latest); err != nil {
					return ""
				}
				return latest.Status.Phase
			}, time.Second*120, restoreInterval).Should(Equal("Failed"),
				"the second restore must terminal-fail when the cluster annotation "+
					"is held by another restore; running both concurrently would race on "+
					"the STS scale-down and the database file lock")

			By("Asserting the error message names the existing holder")
			latest := &neo4jv1beta1.Neo4jRestore{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(restoreB), latest)).To(Succeed())
			Expect(latest.Status.Message).To(ContainSubstring("already has a restore in progress"),
				"message must clearly name the overlap condition; a generic 'failed to scale' "+
					"error wouldn't tell the user the issue is a concurrent restore")
			Expect(latest.Status.Message).To(ContainSubstring("phantom-restore-a"),
				"message must name the FIRST restore (the one holding the annotation) "+
					"so the user can find and resolve it")
		})
	})

	// ─── #121-1: Data-integrity round-trip ────────────────────────────
	//
	// End-to-end loop: cypher CREATE sentinel → backup → cypher DELETE
	// sentinel → restore with stopCluster=true → assert STS scales 0
	// then back up → cypher SHOW sentinel returns 1. Catches both #117
	// failure modes simultaneously:
	//   - cluster-controller fight on STS replicas (fails at "scale back
	//     up" if the controllers deadlock),
	//   - EmptyDir/silent-data-loss bug (fails at the final SHOW
	//     marker assertion if the restore's writes don't reach the
	//     cluster's data PVC).
	//
	// The fixes that made this loop work end-to-end:
	//   - File-path --from-path resolution (buildLocalRestoreFilePath):
	//     neo4j-admin restore wants a FILE path, not a directory.
	//   - Default --temp-path=/tmp/restore-tmp for PVC sources: the
	//     backup PVC is mounted ReadOnly so neo4j-admin can't extract
	//     in-place. Paired `mkdir -p` prelude ensures the dir starts
	//     empty (neo4j-admin requires that).
	//   - AlreadyExists-tolerant Job creation: concurrent reconciles
	//     during the stopCluster cycle were racing; the loser was
	//     terminal-failing the restore on a benign AlreadyExists.
	//   - Idempotent startCluster: same concurrent-reconcile pattern
	//     hit the original-replicas annotation (which the FIRST
	//     startCluster deletes); the second now treats missing as
	//     "already done" instead of failing.
	//   - Post-restore dbms.[cluster.]recreateDatabase: the restore
	//     Job writes only to server-0's PVC, but in a multi-server
	//     cluster the database's primary placement on re-bootstrap is
	//     non-deterministic. Without this step, ~30% of runs landed
	//     the primary on server-1 (stale data) and silently
	//     overwrote the restored data on re-sync. Now the operator
	//     calls recreateDatabase with server-0 explicitly listed as
	//     the seedingServer, forcing every other server to re-seed
	//     from it.
	Context("Data-integrity round-trip", func() {
		It("should restore a sentinel node written before backup, deleted before restore (issue #121-1)", func() {
			// On failure, dump restore Job Pod logs + cluster state so
			// future regressions in this loop produce useful forensics
			// without needing a separate diagnostic run.
			DeferCleanup(func() {
				if CurrentSpecReport().Failed() {
					dumpRestoreDiagnostics(ns, "roundtrip-restore")
				}
			})

			pod0 := fmt.Sprintf("%s-server-0", clusterName)

			// cypher runs an arbitrary statement via kubectl-exec
			// cypher-shell on pod-0. Returns stdout+stderr for assertion.
			cypher := func(stmt string) (string, error) {
				cmd := exec.CommandContext(ctx, "kubectl", "exec",
					pod0, "-n", ns, "--",
					"cypher-shell", "--format", "plain",
					"-u", "neo4j", "-p", adminPass,
					stmt)
				out, err := cmd.CombinedOutput()
				return string(out), err
			}

			By("Writing sentinel node via cypher-shell")
			Eventually(func() error {
				out, err := cypher(`CREATE (n:RestoreMarker {id: 'pre-backup', ts: timestamp()}) RETURN n.id`)
				if err != nil {
					GinkgoWriter.Printf("CREATE failed (cluster may still be settling): %s\n", out)
				}
				return err
			}, time.Second*120, restoreInterval).Should(Succeed(),
				"CREATE must succeed once the cluster is Ready — if this hangs, "+
					"the cluster never became writable")

			By("Taking a backup that captures the sentinel")
			backup := &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "roundtrip-backup", Namespace: ns},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: clusterName,
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "restore-backup-pvc"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, backup)).To(Succeed())

			By("Waiting for backup status.phase=Completed")
			Eventually(func() string {
				latest := &neo4jv1beta1.Neo4jBackup{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), latest); err != nil {
					return ""
				}
				return latest.Status.Phase
			}, restoreTimeout, restoreInterval).Should(Equal("Completed"),
				"the backup MUST complete before the restore round-trip can proceed — "+
					"this is the test's pre-condition, not the test itself")

			By("Deleting the sentinel via cypher-shell")
			Eventually(func() error {
				out, err := cypher(`MATCH (n:RestoreMarker {id: 'pre-backup'}) DELETE n RETURN count(n)`)
				if err != nil {
					GinkgoWriter.Printf("DELETE failed: %s\n", out)
				}
				return err
			}, time.Second*60, restoreInterval).Should(Succeed())

			By("Confirming the sentinel is gone (pre-condition for the round-trip)")
			Eventually(func() bool {
				out, _ := cypher(`MATCH (n:RestoreMarker {id: 'pre-backup'}) RETURN count(n) AS c`)
				// cypher-shell --format plain returns: header `c\n0\n` (or similar)
				return strings.Contains(out, "\n0\n")
			}, time.Second*30, restoreInterval).Should(BeTrue(),
				"DELETE must have removed the sentinel before we start the restore — "+
					"otherwise the final assertion is meaningless")

			By("Applying Neo4jRestore with stopCluster=true")
			// Re-fetch the backup — the Eventually for Completed used a
			// local `latest` variable and didn't update our outer
			// `backup`, so its Status.History would be empty here.
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(backup), backup)).To(Succeed())
			// Default to the deterministic Job-name pattern; override
			// with the recorded history entry if #129 has populated it
			// (which it should — but the assertion order here is
			// "build a working restore CR first, validate the BackupsPath
			// contract via the dedicated #130 test, not this one").
			runSubdir := backup.Name + "-backup"
			if len(backup.Status.History) > 0 && backup.Status.History[0].BackupsPath != "" {
				runSubdir = backup.Status.History[0].BackupsPath
			}
			restore := &neo4jv1beta1.Neo4jRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "roundtrip-restore", Namespace: ns},
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					ClusterRef:   clusterName,
					DatabaseName: "neo4j",
					StopCluster:  true,
					Force:        true, // overwrite the live database
					Source: neo4jv1beta1.RestoreSource{
						Type:       "storage",
						BackupPath: runSubdir,
						Storage: &neo4jv1beta1.StorageLocation{
							Type: "pvc",
							PVC:  &neo4jv1beta1.PVCSpec{Name: "restore-backup-pvc"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, restore)).To(Succeed())

			By("Asserting STS scales to 0 during the restore")
			// First failure mode from #117: cluster-controller fights
			// the restore on STS replicas. If this Eventually never
			// sees replicas==0, the controllers are deadlocked.
			stsKey := types.NamespacedName{Name: clusterName + "-server", Namespace: ns}
			Eventually(func() int32 {
				sts := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, stsKey, sts); err != nil {
					return -1
				}
				if sts.Spec.Replicas == nil {
					return -1
				}
				return *sts.Spec.Replicas
			}, time.Second*180, restoreInterval).Should(BeEquivalentTo(0),
				"STS must scale to 0 before the restore Job runs — if the "+
					"cluster controller keeps fighting back to 2 replicas, the "+
					"restore Job's neo4j-admin invocation will race the live DB "+
					"file lock and fail with a confusing error")

			By("Waiting for restore status.phase=Completed (fail-fast on Failed)")
			// Fail-fast on terminal "Failed" — the operator's
			// `Restore previously failed; not retrying` guard
			// (neo4jrestore_controller.go:174) makes the phase
			// sticky, so waiting out restoreTimeout would just
			// burn through the Job's TTLSecondsAfterFinished=300s
			// window and leave us with no Pod logs to inspect.
			// Dumping diagnostics immediately captures the
			// neo4j-admin output while the Pod still exists.
			Eventually(func(g Gomega) string {
				latest := &neo4jv1beta1.Neo4jRestore{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(restore), latest)).To(Succeed())
				if latest.Status.Phase == "Failed" {
					dumpRestoreDiagnostics(ns, "roundtrip-restore")
					StopTrying(fmt.Sprintf(
						"restore terminal-failed before we could verify data integrity. "+
							"status.message=%q — see RESTORE FAILURE DIAGNOSTICS dump above for Pod logs.",
						latest.Status.Message)).Now()
				}
				return latest.Status.Phase
			}, restoreTimeout, restoreInterval).Should(Equal("Completed"))

			By("Asserting STS scales back up to original size")
			Eventually(func() int32 {
				sts := &appsv1.StatefulSet{}
				if err := k8sClient.Get(ctx, stsKey, sts); err != nil {
					return -1
				}
				if sts.Spec.Replicas == nil {
					return -1
				}
				return *sts.Spec.Replicas
			}, time.Second*180, restoreInterval).Should(BeEquivalentTo(2),
				"STS must scale back to 2 after the restore completes — "+
					"a stuck-at-0 result was the second symptom of the #117 "+
					"cluster-controller fight")

			By("Waiting for cluster to return to Ready")
			Eventually(func() string {
				latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: clusterName, Namespace: ns,
				}, latest); err != nil {
					return ""
				}
				return latest.Status.Phase
			}, restoreTimeout, restoreInterval).Should(Equal("Ready"))

			By("Asserting the sentinel is back (the actual data-integrity check)")
			// Second failure mode from #117: restore Job writes to an
			// EmptyDir, Pod exits 0, EmptyDir evaporates, the cluster's
			// PVC data is unchanged. A "did the Job succeed" check
			// passes; this assertion is what catches it.
			Eventually(func() bool {
				out, err := cypher(`MATCH (n:RestoreMarker {id: 'pre-backup'}) RETURN count(n) AS c`)
				if err != nil {
					GinkgoWriter.Printf("verification cypher failed: %s\n", out)
					return false
				}
				// Either "\n1\n" (single restored marker) is acceptable;
				// any other count signals the restore didn't land.
				return strings.Contains(out, "\n1\n")
			}, time.Second*180, restoreInterval).Should(BeTrue(),
				"the sentinel node MUST be back after the restore. If this "+
					"fails, the restore Job ran but its writes didn't reach the "+
					"cluster's data PVC — most likely the EmptyDir failure mode "+
					"from #117 (silent data loss).")
		})
	})

	// #121-4 (mid-restore CR delete cleans up annotation) is intentionally
	// not covered here. Reasons:
	//
	//   - The fast path (delete CR while STS is at replicas=0) is
	//     timing-dependent — the test would need to race the finalizer
	//     vs the controller's reconcile loop. Flaky on slow CI.
	//
	//   - The finalizer cleanup logic is covered by unit tests in
	//     internal/controller/neo4jrestore_coordination_test.go.
	//
	//   - The "leaked annotation" failure mode is visible to operators
	//     via the cluster's status — a user noticing a stuck restore
	//     can `kubectl annotate cluster X neo4j.com/restore-in-progress-`
	//     as a manual recovery, which makes the integration-test miss
	//     less catastrophic.
	//
	// Reconsider once we have dedicated cluster-coordination integration
	// infrastructure (issue TBD).
})

// removeFinalizersAndDelete is a teardown helper that walks a list of
// CRs, strips their finalizers, and deletes them. Used in AfterAll to
// avoid leaking namespaces with stuck CRs between test runs.
func removeFinalizersAndDelete(list client.ObjectList, namespace string) error {
	if err := k8sClient.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return err
	}
	switch typed := list.(type) {
	case *neo4jv1beta1.Neo4jRestoreList:
		for i := range typed.Items {
			item := &typed.Items[i]
			item.SetFinalizers(nil)
			_ = k8sClient.Update(ctx, item)
			_ = k8sClient.Delete(ctx, item)
		}
	case *neo4jv1beta1.Neo4jBackupList:
		for i := range typed.Items {
			item := &typed.Items[i]
			item.SetFinalizers(nil)
			_ = k8sClient.Update(ctx, item)
			_ = k8sClient.Delete(ctx, item)
		}
	}
	return nil
}
