/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// TestParseShardArtifactsFromLog_RealWorldVariants pins the parser
// against the kinds of log lines neo4j-admin emits in practice across
// versions. Format isn't a versioned API, so the tests focus on what
// MUST be tolerated:
//
//   - filenames anywhere on a line (prose, paths, structured log JSON),
//   - timestamps with `:`, `.`, or `-` separators in the ISO-like suffix,
//   - sizes attached as "N bytes" — extract; human-readable sizes ignored,
//   - duplicate shard lines (retries / multi-line summaries) — last wins,
//   - lines without shard filenames — skip silently.
func TestParseShardArtifactsFromLog_RealWorldVariants(t *testing.T) {
	cases := []struct {
		name    string
		logBody string
		want    map[string]neo4jv1beta1.ShardArtifact
	}{
		{
			name: "single-line success messages with byte sizes",
			logBody: strings.Join([]string{
				"Backup completed: products-g000-2025-06-11T21-04-42.backup (1048576 bytes)",
				"Backup completed: products-p000-2025-06-11T21-04-47.backup (524288 bytes)",
				"Backup completed: products-p001-2025-06-11T21-04-51.backup (786432 bytes)",
			}, "\n"),
			want: map[string]neo4jv1beta1.ShardArtifact{
				"products-g000": {ShardName: "products-g000", Filename: "products-g000-2025-06-11T21-04-42.backup", Size: 1048576},
				"products-p000": {ShardName: "products-p000", Filename: "products-p000-2025-06-11T21-04-47.backup", Size: 524288},
				"products-p001": {ShardName: "products-p001", Filename: "products-p001-2025-06-11T21-04-51.backup", Size: 786432},
			},
		},
		{
			name: "filenames inside path-like prose without sizes",
			logBody: strings.Join([]string{
				"2026-01-15 08:23:11.842 INFO  Writing /backup/run-001/orders-g000-2026-01-15T08:23:11.842.backup",
				"2026-01-15 08:23:11.842 INFO  Writing /backup/run-001/orders-p005-2026-01-15T08:23:11.842.backup",
			}, "\n"),
			want: map[string]neo4jv1beta1.ShardArtifact{
				"orders-g000": {ShardName: "orders-g000", Filename: "orders-g000-2026-01-15T08:23:11.842.backup"},
				"orders-p005": {ShardName: "orders-p005", Filename: "orders-p005-2026-01-15T08:23:11.842.backup"},
			},
		},
		{
			name: "duplicates - last occurrence wins",
			logBody: strings.Join([]string{
				"Backup completed: products-g000-2025-06-11T21-04-42.backup (1024 bytes)",
				// Retry overwrote with a fresher timestamp + new size:
				"Backup completed: products-g000-2025-06-11T21-05-12.backup (2048 bytes)",
			}, "\n"),
			want: map[string]neo4jv1beta1.ShardArtifact{
				"products-g000": {ShardName: "products-g000", Filename: "products-g000-2025-06-11T21-05-12.backup", Size: 2048},
			},
		},
		{
			name: "noise-only log returns empty map",
			logBody: strings.Join([]string{
				"INFO  Starting Neo4j 2025.12.0",
				"INFO  Connected to backup endpoint",
				"INFO  Backup completed (no shard names on this line)",
			}, "\n"),
			want: map[string]neo4jv1beta1.ShardArtifact{},
		},
		{
			name:    "empty input is safe",
			logBody: "",
			want:    map[string]neo4jv1beta1.ShardArtifact{},
		},
		{
			name: "size-with-no-spaces and the singular form are both captured",
			logBody: strings.Join([]string{
				"products-g000-2025-06-11T21-04-42.backup 1 byte",
				"products-p000-2025-06-11T21-04-47.backup 999999 bytes",
			}, "\n"),
			want: map[string]neo4jv1beta1.ShardArtifact{
				"products-g000": {ShardName: "products-g000", Filename: "products-g000-2025-06-11T21-04-42.backup", Size: 1},
				"products-p000": {ShardName: "products-p000", Filename: "products-p000-2025-06-11T21-04-47.backup", Size: 999999},
			},
		},
		{
			name: "shard prefix mismatch is rejected (no s prefix)",
			logBody: strings.Join([]string{
				// Spurious shard ID like 'orders-s001' must NOT match.
				"orders-s001-2026-01-15T08:23:11.backup (1024 bytes)",
			}, "\n"),
			want: map[string]neo4jv1beta1.ShardArtifact{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseShardArtifactsFromLog(c.logBody)
			if !reflect.DeepEqual(got, c.want) {
				// Pretty-print the keys we got to make diff-debugging
				// easier — the structured map is unordered.
				keys := make([]string, 0, len(got))
				for k := range got {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				t.Errorf("parsed=%v (keys=%v)\nwant=%v", got, keys, c.want)
			}
		})
	}
}

// TestParseValidationFromLog_Matrix pins the validate-parser behaviour
// across the realistic states neo4j-admin backup validate produces.
// Format isn't versioned-stable — same caveat as the shard-artifact
// parser — so the matrix focuses on:
//
//   - All-OK rows → OverallStatus=OK,
//   - Any non-OK row → OverallStatus=Degraded,
//   - Validate-style output but no shard rows → OverallStatus=Unknown,
//   - No validate invocation at all in log → nil (field stays empty).
func TestParseValidationFromLog_Matrix(t *testing.T) {
	cases := []struct {
		name         string
		logBody      string
		wantNil      bool
		wantOverall  string
		wantPerShard []neo4jv1beta1.ShardValidationStatus
	}{
		{
			name: "all shards OK → Overall=OK (canonical table format)",
			logBody: strings.Join([]string{
				"Running: neo4j-admin backup validate --from-path=s3://b/p/ --database=\"products\"",
				"| DATABASE     | PATH                                                | STATUS |",
				"| products-g000 | /bucket/backups/products-g000-2025-06-11T21-04-42.backup |     OK |",
				"| products-p000 | /bucket/backups/products-p000-2025-06-11T21-04-37.backup |     OK |",
				"| products-p001 | /bucket/backups/products-p001-2025-06-11T21-04-40.backup |     OK |",
			}, "\n"),
			wantOverall: "OK",
			wantPerShard: []neo4jv1beta1.ShardValidationStatus{
				{ShardName: "products-g000", Status: "OK"},
				{ShardName: "products-p000", Status: "OK"},
				{ShardName: "products-p001", Status: "OK"},
			},
		},
		{
			name: "one shard Behind → Overall=Degraded (real validate output format)",
			logBody: strings.Join([]string{
				"Running: neo4j-admin backup validate --from-path=/backup/p/ --database=\"products\"",
				"| products-g000 | /backups/products-g000-T21-04-42.backup |                                                          OK |",
				"| products-p000 | /backups/products-p000-T21-04-37.backup | Backup is behind (3 < 5) the graph shard backup chain    |",
				"| products-p001 | /backups/products-p001-T21-04-40.backup |                                                          OK |",
			}, "\n"),
			wantOverall: "Degraded",
			wantPerShard: []neo4jv1beta1.ShardValidationStatus{
				{ShardName: "products-g000", Status: "OK"},
				{ShardName: "products-p000", Status: "Behind"},
				{ShardName: "products-p001", Status: "OK"},
			},
		},
		{
			name: "Ahead also counts as Degraded",
			logBody: strings.Join([]string{
				"Running: neo4j-admin backup validate --from-path=/backup/p/ --database=\"products\"",
				"| products-g000 | /backups/products-g000-T21-04-42.backup | Backup is ahead (12 > 8) of the graph shard backup chain |",
				"| products-p000 | /backups/products-p000-T21-04-37.backup |                                                       OK |",
			}, "\n"),
			wantOverall: "Degraded",
			wantPerShard: []neo4jv1beta1.ShardValidationStatus{
				{ShardName: "products-g000", Status: "Ahead"},
				{ShardName: "products-p000", Status: "OK"},
			},
		},
		{
			name:        "validate ran but no parseable shard rows → Overall=Unknown + RawOutput populated",
			logBody:     "Running: neo4j-admin backup validate --from-path=foo --database=bar\nERROR: …",
			wantOverall: "Unknown",
		},
		{
			name:    "no validate invocation at all → nil (validate wasn't run)",
			logBody: "Backup completed: products-g000-2025-06-11T21-04-42.backup",
			wantNil: true,
		},
		{
			name:    "empty log → nil",
			logBody: "",
			wantNil: true,
		},
		{
			name: "last-occurrence-wins per shard",
			logBody: strings.Join([]string{
				"Running: neo4j-admin backup validate --from-path=/backup/p/ --database=\"products\"",
				"| products-g000 | /a-T21-04-42.backup | Backup is behind (3 < 5) the graph shard backup chain |",
				// later, validate re-emitted final summary with OK:
				"| products-g000 | /a-T21-04-42.backup |                                                    OK |",
			}, "\n"),
			wantOverall: "OK",
			wantPerShard: []neo4jv1beta1.ShardValidationStatus{
				{ShardName: "products-g000", Status: "OK"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseValidationFromLog(c.logBody)
			if c.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil result, got nil")
			}
			if got.OverallStatus != c.wantOverall {
				t.Errorf("OverallStatus=%q, want %q", got.OverallStatus, c.wantOverall)
			}
			if c.wantPerShard != nil && !reflect.DeepEqual(got.PerShard, c.wantPerShard) {
				t.Errorf("PerShard=%+v\nwant %+v", got.PerShard, c.wantPerShard)
			}
			// RawOutput should always be populated when result isn't nil
			// (so users can read partial output).
			if got.OverallStatus == "Unknown" && got.RawOutput == "" {
				t.Errorf("Unknown status should include RawOutput for debugging")
			}
		})
	}
}

// TestTruncateForRaw_CapsAt2KiB pins the etcd-friendly size cap.
func TestTruncateForRaw_CapsAt2KiB(t *testing.T) {
	big := strings.Repeat("x", validateRawOutputCap*2)
	got := truncateForRaw(big)
	if len(got) > validateRawOutputCap+len("\n…(truncated)") {
		t.Errorf("len(got)=%d, expected ≤ %d", len(got), validateRawOutputCap+len("\n…(truncated)"))
	}
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("expected truncation marker; got tail %q", got[len(got)-20:])
	}

	t.Run("short input unchanged", func(t *testing.T) {
		s := "hello"
		if truncateForRaw(s) != s {
			t.Errorf("short input should pass through unchanged")
		}
	})
}

// TestMergeShardArtifactsFromLog confirms expected shards stay in their
// original slice order (so display + audit ordering is stable across
// reconciles), with Filename / Size getting filled in where the log
// matched a shard. Shards not present in the log map retain empty
// Filename/Size — the load-bearing audit field (ShardName) is preserved
// either way.
func TestMergeShardArtifactsFromLog(t *testing.T) {
	expected := []neo4jv1beta1.ShardArtifact{
		{ShardName: "products-g000"},
		{ShardName: "products-p000"},
		{ShardName: "products-p001"},
	}
	fromLog := map[string]neo4jv1beta1.ShardArtifact{
		"products-g000": {ShardName: "products-g000", Filename: "products-g000-X.backup", Size: 1024},
		"products-p001": {ShardName: "products-p001", Filename: "products-p001-Y.backup"},        // no size in log
		"orders-g000":   {ShardName: "orders-g000", Filename: "orders-g000-Z.backup", Size: 999}, // not in expected — ignored
	}

	got := mergeShardArtifactsFromLog(expected, fromLog)
	want := []neo4jv1beta1.ShardArtifact{
		{ShardName: "products-g000", Filename: "products-g000-X.backup", Size: 1024},
		{ShardName: "products-p000"}, // unchanged
		{ShardName: "products-p001", Filename: "products-p001-Y.backup"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	t.Run("nil log map → returns expected unchanged", func(t *testing.T) {
		got := mergeShardArtifactsFromLog(expected, nil)
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("got %+v, want %+v", got, expected)
		}
	})
}
