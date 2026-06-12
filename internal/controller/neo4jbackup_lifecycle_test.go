/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Pinning tests for the #217 backup lifecycle fixes: Job-condition-based
// terminal detection (a transient pod retry must NOT mark a backup Failed),
// CronJob suspend propagation, orphaned-CronJob cleanup on schedule removal,
// and the Compress *bool default semantics (rule 66).

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func backupLifecycleScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, neo4jv1beta1.AddToScheme(s))
	require.NoError(t, batchv1.AddToScheme(s))
	return s
}

func jobWithCondition(name, ns string, condType batchv1.JobConditionType) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{Type: condType, Status: "True"}},
		},
	}
}

// TestJobToBackupRun_RetryingJobIsNotTerminal pins the core #217 fix: a Job
// with failed pod ATTEMPTS but no JobFailed condition is still retrying
// (BackoffLimit=3) and must not be recorded — recording Failed at that point
// is permanent (RunID dedup never corrects it) and breaks backupRef
// resolution for an eventually-successful run.
func TestJobToBackupRun_RetryingJobIsNotTerminal(t *testing.T) {
	retrying := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "b-backup", Namespace: "ns"},
		Status:     batchv1.JobStatus{Failed: 1}, // one failed attempt, no terminal condition
	}
	_, ok := jobToBackupRun(retrying, "/backup/b")
	assert.False(t, ok, "a retrying Job must not be recorded as a terminal run")

	failed := jobWithCondition("b-backup", "ns", batchv1.JobFailed)
	failed.Status.Failed = 4
	run, ok := jobToBackupRun(failed, "/backup/b")
	require.True(t, ok)
	assert.Equal(t, "Failed", run.Status)

	complete := jobWithCondition("b-backup", "ns", batchv1.JobComplete)
	complete.Status.Succeeded = 1
	run, ok = jobToBackupRun(complete, "/backup/b")
	require.True(t, ok)
	assert.Equal(t, "Succeeded", run.Status)
}

// TestHandleExistingBackupJob_RetryingJobStaysRunning pins the one-shot path:
// the CR must report Running (not terminal Failed) while the Job retries.
func TestHandleExistingBackupJob_RetryingJobStaysRunning(t *testing.T) {
	scheme := backupLifecycleScheme(t)
	backup := &neo4jv1beta1.Neo4jBackup{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "b-backup", Namespace: "ns"},
		Status:     batchv1.JobStatus{Failed: 2}, // retrying — no JobFailed condition
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backup, job).WithStatusSubresource(backup).Build()
	r := &Neo4jBackupReconciler{Client: fc, Recorder: record.NewFakeRecorder(10), RequeueAfter: 30 * time.Second}

	res, err := r.handleExistingBackupJob(context.Background(), backup, job)
	require.NoError(t, err)
	assert.NotZero(t, res.RequeueAfter, "still-running Jobs requeue for re-observation")

	latest := &neo4jv1beta1.Neo4jBackup{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "b", Namespace: "ns"}, latest))
	assert.Equal(t, "Running", latest.Status.Phase,
		"failed pod attempts must not flip the CR to terminal Failed while the Job retries")

	// And once the retry budget is exhausted (JobFailed condition), Failed.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: "True"}}
	_, err = r.handleExistingBackupJob(context.Background(), backup, job)
	require.NoError(t, err)
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "b", Namespace: "ns"}, latest))
	assert.Equal(t, "Failed", latest.Status.Phase)
}

// TestSuspendBackupCronJob pins #217 suspend propagation: suspending the CR
// must suspend the CronJob, and the scheduled-path mutate must resume it.
func TestSuspendBackupCronJob(t *testing.T) {
	scheme := backupLifecycleScheme(t)
	backup := &neo4jv1beta1.Neo4jBackup{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}}
	cron := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "b-backup-cron", Namespace: "ns"},
		Spec:       batchv1.CronJobSpec{Schedule: "0 2 * * *"},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backup, cron).Build()
	r := &Neo4jBackupReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

	require.NoError(t, r.suspendBackupCronJob(context.Background(), backup))

	latest := &batchv1.CronJob{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "b-backup-cron", Namespace: "ns"}, latest))
	require.NotNil(t, latest.Spec.Suspend)
	assert.True(t, *latest.Spec.Suspend, "CR suspend must propagate to the CronJob")

	// Idempotent on an already-suspended CronJob.
	require.NoError(t, r.suspendBackupCronJob(context.Background(), backup))

	// No CronJob (one-time backup) is a clean no-op.
	other := &neo4jv1beta1.Neo4jBackup{ObjectMeta: metav1.ObjectMeta{Name: "no-cron", Namespace: "ns"}}
	require.NoError(t, r.suspendBackupCronJob(context.Background(), other))
}

// TestCleanupOrphanedCronJob pins #217 schedule removal: converting a
// scheduled CR to a one-shot must delete the CronJob, or it keeps firing
// scheduled backups while the CR claims to be a one-shot.
func TestCleanupOrphanedCronJob(t *testing.T) {
	scheme := backupLifecycleScheme(t)
	backup := &neo4jv1beta1.Neo4jBackup{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}}
	cron := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "b-backup-cron", Namespace: "ns"},
		Spec:       batchv1.CronJobSpec{Schedule: "0 2 * * *"},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backup, cron).Build()
	r := &Neo4jBackupReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

	require.NoError(t, r.cleanupOrphanedCronJob(context.Background(), backup))

	latest := &batchv1.CronJob{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "b-backup-cron", Namespace: "ns"}, latest)
	assert.True(t, err != nil, "CronJob must be deleted when the schedule is removed")

	// Idempotent when nothing is left.
	require.NoError(t, r.cleanupOrphanedCronJob(context.Background(), backup))
}

// TestCompressEffective pins the rule-66 *bool semantics: nil means the
// documented default (true); an explicit false survives.
func TestCompressEffective(t *testing.T) {
	var nilOpts *neo4jv1beta1.BackupOptions
	assert.True(t, nilOpts.CompressEffective())
	assert.True(t, (&neo4jv1beta1.BackupOptions{}).CompressEffective())
	assert.True(t, (&neo4jv1beta1.BackupOptions{Compress: ptr.To(true)}).CompressEffective())
	assert.False(t, (&neo4jv1beta1.BackupOptions{Compress: ptr.To(false)}).CompressEffective(),
		"explicit false must survive — bool+omitempty+default silently re-applied true (rule 66)")
}

// TestChainConcurrency_SeesCronJobChildren pins the #217 label fix: CronJob
// children carry component=backup-cron, and the old component=backup filter
// made scheduled runs invisible to the chain concurrency guard.
func TestChainConcurrency_SeesCronJobChildren(t *testing.T) {
	scheme := backupLifecycleScheme(t)
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "hourly-diff", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jBackupSpec{ChainFromBackup: "daily-full"},
	}
	cronChild := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "daily-full-backup-cron-1738000000", Namespace: "ns",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/component":  "backup-cron", // scheduled child
				"app.kubernetes.io/part-of":    "daily-full",
			},
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backup, cronChild).Build()
	r := &Neo4jBackupReconciler{Client: fc}

	err := r.waitForChainConcurrencyClear(context.Background(), backup)
	require.Error(t, err)
	assert.ErrorIs(t, err, errChainBusy,
		"an active scheduled child in the same chain must block a chained run")
}

// TestValidateChainParent_NotFoundIsTransient pins #217: the parent CR not
// existing YET is an apply-ordering condition (Pending), not terminal.
func TestValidateChainParent_NotFoundIsTransient(t *testing.T) {
	scheme := backupLifecycleScheme(t)
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "diff", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jBackupSpec{ChainFromBackup: "missing-parent"},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backup).Build()
	r := &Neo4jBackupReconciler{Client: fc}

	err := r.validateChainParent(context.Background(), backup)
	require.Error(t, err)
	assert.ErrorIs(t, err, errBackupTransient)
}

// TestParseFindTimeArg_Units pins #217: every unit the validator accepts is
// honored — "90m" previously degraded to 90 DAYS.
func TestParseFindTimeArg_Units(t *testing.T) {
	assert.Equal(t, "-mtime +7", parseFindTimeArg("7d"))
	assert.Equal(t, "-mmin +2880", parseFindTimeArg("48h"))
	assert.Equal(t, "-mmin +90", parseFindTimeArg("90m"))
	assert.Equal(t, "-mmin +2", parseFindTimeArg("90s"))
	assert.Equal(t, "-mtime +7", parseFindTimeArg("bogus"))
}

// TestBuildRetentionScript_FileLevelChainScoped pins the #217 retention
// rewrite: pruning operates on *.backup FILES inside THIS CR's chain dir —
// the old script rm -rf'd depth-1 DIRECTORIES under /backup (entire chain
// roots, including other CRs' chains on a shared PVC).
func TestBuildRetentionScript_FileLevelChainScoped(t *testing.T) {
	script := buildRetentionScript(&neo4jv1beta1.RetentionPolicy{MaxCount: 5, MaxAge: "7d"}, "daily-prod")

	assert.Contains(t, script, "'/backup/daily-prod'", "pruning must be scoped to the CR's chain dir")
	assert.Contains(t, script, "-name '*.backup'", "pruning must target artifact FILES, not directories")
	assert.Contains(t, script, "-type f", "must never rm -rf directories")
	assert.NotContains(t, script, "-type d", "the pre-rule-40 directory model must be gone")
	assert.Contains(t, script, "NEWEST=", "maxAge must always keep the newest artifact")
	assert.Contains(t, script, "rm -f", "file-level deletion only")
	assert.NotContains(t, script, "rm -rf", "recursive deletion of chains must be gone")
}
