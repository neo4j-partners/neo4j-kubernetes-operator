/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

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
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-a")},
			Status: batchv1.JobStatus{
				Succeeded:      1,
				StartTime:      &start,
				CompletionTime: &end,
			},
		}
		run, ok := jobToBackupRun(job)
		if !ok {
			t.Fatalf("expected ok=true for a succeeded Job")
		}
		if run.RunID != "uid-a" {
			t.Errorf("RunID: got %q, want %q", run.RunID, "uid-a")
		}
		if run.Status != "Succeeded" {
			t.Errorf("Status: got %q, want Succeeded", run.Status)
		}
		if run.Stats == nil || run.Stats.Duration != "45s" {
			t.Errorf("Stats.Duration: got %+v, want 45s", run.Stats)
		}
	})

	t.Run("failed Job → BackupRun without Duration", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-b")},
			Status:     batchv1.JobStatus{Failed: 3, StartTime: &start},
		}
		run, ok := jobToBackupRun(job)
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
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("uid-c")},
			Status:     batchv1.JobStatus{StartTime: &start},
		}
		if _, ok := jobToBackupRun(job); ok {
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
		want    bool
	}{
		{
			name: "match found",
			history: []neo4jv1beta1.BackupRun{
				mk("uid-a"), mk("uid-b"),
			},
			run:  mk("uid-b"),
			want: true,
		},
		{
			name:    "no match",
			history: []neo4jv1beta1.BackupRun{mk("uid-a")},
			run:     mk("uid-b"),
			want:    false,
		},
		{
			name:    "empty history",
			history: nil,
			run:     mk("uid-a"),
			want:    false,
		},
		{
			name: "incoming run with empty RunID is never a duplicate",
			// Avoids false positives against historic entries pre-upgrade
			// that have RunID="" — those would otherwise all "match" each
			// other and break the append path the first time around.
			history: []neo4jv1beta1.BackupRun{mk(""), mk("uid-a")},
			run:     mk(""),
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := backupRunAlreadyRecorded(tc.history, tc.run)
			if got != tc.want {
				t.Errorf("backupRunAlreadyRecorded() = %v, want %v", got, tc.want)
			}
		})
	}
}
