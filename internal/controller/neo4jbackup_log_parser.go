/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// shardArtifactFilenameRegex matches per-shard `.backup` filenames in
// neo4j-admin's stdout. neo4j-admin doesn't document its log format as a
// versioned API, so the regex is intentionally permissive: it matches the
// filename token anywhere on a line and ignores surrounding prose, paths,
// timestamps, or log-level decorations.
//
// Filename shape: `{logical-name}-{shard-prefix}{NNN}-{ISO-timestamp}.backup`
// where shard-prefix is `g` (graph) or `p` (property). The ISO timestamp
// uses `:`, `.`, or `-` as separators across Neo4j versions — be liberal.
//
//	products-g000-2025-06-11T21-04-42.backup
//	orders-p012-2026-01-15T08:23:11.842.backup
//
// Capture groups:
//
//	[1] = full filename (e.g. "products-g000-2025-06-11T21-04-42.backup")
//	[2] = shard name (e.g. "products-g000") — matches the ShardName produced by expectedShardArtifactsForBackup
var shardArtifactFilenameRegex = regexp.MustCompile(
	`(([a-zA-Z][\w.-]*-(?:g|p)\d{3})-[\d.:T+\-Z]+\.backup)`,
)

// shardArtifactSizeRegex finds an integer byte count attached to a shard
// artifact line. Neo4j formats vary across versions — both bare-number
// "(12345 bytes)" and human-readable "12345 B" / "12.3 MB" forms exist.
// We parse only the bare-number form for accuracy; human-readable values
// are skipped and the Size field stays zero, signalling "not captured".
//
// Same caveat as the filename regex: best-effort, non-fatal on miss.
var shardArtifactSizeRegex = regexp.MustCompile(`(\d+)\s*bytes?\b`)

// standardArtifactFilenameRegex matches the `.backup` filename produced by
// a standard (non-sharded) database backup. Shape:
//
//	{dbname}-{ISO-timestamp}.backup
//
// e.g. `neo4j-2026-06-08T01-18-06.backup`,
//
//	`inventory-2026-06-08T01-23-44.842.backup`.
//
// We deliberately reject shard-shaped filenames (`<name>-g000-...backup`
// or `<name>-p000-...backup`) so this parser doesn't double-capture
// sharded-backup logs as standard ones. The negative lookahead Go regex
// doesn't support is emulated by requiring the segment after the dbname
// dash to start with a 4-digit year (`20\d\d`) rather than the
// `(g|p)\d{3}` shard prefix.
//
// Capture groups:
//
//	[1] = full filename
//	[2] = database name (matches Neo4jBackup.spec.target.name)
var standardArtifactFilenameRegex = regexp.MustCompile(
	`(([a-zA-Z][\w.-]*?)-(20\d\d[\d.:T+\-Z]+)\.backup)`,
)

// parseStandardArtifactFromLog scans neo4j-admin's stdout for a
// standard-DB `.backup` filename matching the requested database name.
// Returns "" if no match is found (the caller treats this as "not
// captured" — the ArtifactFilename status field stays empty).
//
// Multiple matches → LAST occurrence wins, matching the
// last-occurrence-wins semantics of the sharded parser.
//
// dbName is required because the same Job log can contain unrelated
// `.backup` references (e.g. earlier backups listed by `ls`); we anchor
// on the requested database name.
func parseStandardArtifactFromLog(logContent, dbName string) string {
	if dbName == "" {
		return ""
	}
	var winner string
	scanner := bufio.NewScanner(strings.NewReader(logContent))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		matches := standardArtifactFilenameRegex.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			filename, matchedDB := m[1], m[2]
			if matchedDB != dbName {
				continue
			}
			winner = filename
		}
	}
	return winner
}

// parseShardArtifactsFromLog scans neo4j-admin stdout and returns a map
// keyed by shard name (e.g. "products-g000") with Filename + Size set.
// Filenames are deduplicated by shard name — if a shard appears multiple
// times (retries, multi-line summaries), the LAST occurrence wins.
//
// Non-fatal in every branch: missing data leaves zero values (empty
// Filename / Size=0) for the affected shard. Caller merges into the
// expected-shard slice without overwriting unrelated entries.
func parseShardArtifactsFromLog(logContent string) map[string]neo4jv1beta1.ShardArtifact {
	out := map[string]neo4jv1beta1.ShardArtifact{}

	scanner := bufio.NewScanner(strings.NewReader(logContent))
	// neo4j-admin can emit long lines (stack traces); bump the buffer past
	// the bufio default of 64 KiB so they don't silently truncate.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		matches := shardArtifactFilenameRegex.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		for _, m := range matches {
			filename, shardName := m[1], m[2]
			a := out[shardName]
			a.ShardName = shardName
			a.Filename = filename
			// Always re-parse size from the current line. "Last wins"
			// applies symmetrically to filename and size — a retry's
			// fresher numbers should overwrite the stale ones. Lines
			// without a parseable size leave the prior Size unchanged
			// (zero from a fresh entry, or a real value from an earlier
			// match).
			if sizeMatch := shardArtifactSizeRegex.FindStringSubmatch(line); sizeMatch != nil {
				if n, err := strconv.ParseInt(sizeMatch[1], 10, 64); err == nil {
					a.Size = n
				}
			}
			out[shardName] = a
		}
	}
	return out
}

// fetchBackupPodLog locates the (single) backup Pod owned by the given
// Job, streams its `neo4j` container's stdout via the typed clientset,
// and returns the full log as a string.
//
// Returns ("", nil) — empty log, no error — in three benign cases:
//   - The reconciler's Clientset is nil (test wiring without log access).
//   - No Pod is found with the Job's name label (race against TTL GC
//     after Job.TTLSecondsAfterFinished elapses).
//   - The Pod log API call returns NotFound (Pod GC'd between the List
//     and the GetLogs call).
//
// Other errors propagate so the caller can decide whether to retry or
// give up. The shard-artifact path treats all log-fetch errors as
// non-fatal — the worst case is empty Filename/Size on the BackupRun.
func (r *Neo4jBackupReconciler) fetchBackupPodLog(ctx context.Context, jobName, namespace string) (string, error) {
	if r.Clientset == nil {
		return "", nil
	}
	logger := log.FromContext(ctx)

	// Pods spawned by a Job carry the `batch.kubernetes.io/job-name`
	// label (canonical K8s 1.27+ form, set by the Job controller). Use the
	// controller-runtime client (already cached) for the List, which
	// avoids a redundant HTTP call when the operator is operating in a
	// hot reconcile loop.
	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"batch.kubernetes.io/job-name": jobName}); err != nil {
		return "", fmt.Errorf("list pods for Job %q: %w", jobName, err)
	}
	if len(podList.Items) == 0 {
		logger.Info("No pod found for backup Job — log parsing skipped (likely TTL-GC'd)", "job", jobName)
		return "", nil
	}

	// Multiple pods can exist if the Job retried after a pod failure. Pick
	// the one that succeeded (if any) — its log has the canonical "Backup
	// completed" lines. Falling back to the most-recent pod is a defensive
	// option for older Job behaviours.
	var chosen *corev1.Pod
	for i := range podList.Items {
		p := &podList.Items[i]
		if p.Status.Phase == corev1.PodSucceeded {
			chosen = p
			break
		}
	}
	if chosen == nil {
		// Take the most-recently-created pod as a fallback. Its log may
		// be a partial / failure record; the parser tolerates missing
		// shard lines.
		chosen = &podList.Items[len(podList.Items)-1]
	}

	req := r.Clientset.CoreV1().Pods(namespace).GetLogs(chosen.Name, &corev1.PodLogOptions{
		// Backup Job spawns a single container named "backup" (see
		// createBackupJob / createBackupCronJob — both use this name).
		// Container name matters for log API since Pods could carry more
		// than one container in theory; for the backup case it's always
		// just the one.
		Container: "backup",
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Backup pod log unavailable (NotFound)", "pod", chosen.Name)
			return "", nil
		}
		return "", fmt.Errorf("open log stream for pod %q: %w", chosen.Name, err)
	}
	defer func() { _ = stream.Close() }()
	buf, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("read log stream for pod %q: %w", chosen.Name, err)
	}
	return string(buf), nil
}

// shardValidationStatusRegex matches a per-shard row in `neo4j-admin backup
// validate` output. Format per the sharded admin-operations docs:
//
//	| foo-g000 | /bucket/backups/foo-g000-2025-…-T21-04-42.backup |     OK |
//	| foo-p000 | /backups/foo-p000-…-T21-04-37.backup             | Backup is behind (3 < 5) the graph shard backup chain |
//
// Three pipe-separated columns: shard name, path, status. Status is free-text
// — "OK" alone for healthy, "Backup is behind …"/"Backup is ahead …" for lag.
// The (?m) flag anchors ^ / $ per line so the regex matches one row at a time.
//
// Capture groups:
//
//	[1] = shard name (e.g. "products-g000")
//	[2] = full status text (trimmed by the caller)
var shardValidationStatusRegex = regexp.MustCompile(
	`(?m)^\s*\|\s*([a-zA-Z][\w.-]*-(?:g|p)\d{3})\s*\|[^|]+\|\s*([^|]+?)\s*\|\s*$`,
)

// classifyShardValidationStatus maps the free-text status column to the
// canonical enum stored on `BackupRun.Validation.PerShard[].Status`.
// Recognises "OK" exactly; "Backup is ahead …" / "Backup is behind …"
// substring matches (case-insensitive — the docs vary on capitalisation
// across versions); everything else is "Unknown" so the raw text remains
// visible via RawOutput.
func classifyShardValidationStatus(statusText string) string {
	trimmed := strings.TrimSpace(statusText)
	if trimmed == "OK" {
		return "OK"
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "ahead"):
		return "Ahead"
	case strings.Contains(lower, "behind"):
		return "Behind"
	default:
		return "Unknown"
	}
}

// validateRawOutputCap is the maximum number of bytes of raw validate
// stdout stored on `BackupRun.Validation.RawOutput`. The full log can be
// large (especially with --verbose); we cap to keep the CR size sane.
// Etcd has a hard 1.5 MiB limit per object; 2 KiB per validation result
// is plenty for human-readable triage.
const validateRawOutputCap = 2048

// parseValidationFromLog scans the post-backup-validate output and
// returns a structured BackupValidationResult. The OverallStatus is
// derived from the per-shard rows: all-OK → "OK"; any Ahead/Behind →
// "Degraded"; nothing parseable or no per-shard rows → "Unknown".
//
// Non-fatal in every branch. When parsing fails or the log lacks
// validate output entirely, the returned BackupValidationResult is
// {OverallStatus: "Unknown", RawOutput: <truncated logBody>} so an
// operator can read it manually.
//
// Returns nil when validate wasn't run at all (no validate-style lines
// detected anywhere in the log) so callers can distinguish "validate
// not invoked" from "validate ran but parser couldn't classify".
func parseValidationFromLog(logBody string) *neo4jv1beta1.BackupValidationResult {
	matches := shardValidationStatusRegex.FindAllStringSubmatch(logBody, -1)
	if len(matches) == 0 {
		// Probe for an indication that validate was invoked but emitted
		// nothing parseable. Without ANY signal that validate ran, return
		// nil (the field stays empty — the typical case when the user
		// hasn't opted in).
		if !strings.Contains(logBody, "backup validate") {
			return nil
		}
		return &neo4jv1beta1.BackupValidationResult{
			OverallStatus: "Unknown",
			RawOutput:     truncateForRaw(logBody),
		}
	}

	// Last-wins per shard, mirroring the shard-artifact parser. If the
	// command emitted multiple status lines for the same shard, the most
	// recent one is the authoritative state.
	perShardByName := map[string]neo4jv1beta1.ShardValidationStatus{}
	for _, m := range matches {
		shardName, statusText := m[1], m[2]
		perShardByName[shardName] = neo4jv1beta1.ShardValidationStatus{
			ShardName: shardName,
			Status:    classifyShardValidationStatus(statusText),
		}
	}

	// Sort for deterministic output ordering — easier to diff CR YAMLs.
	names := make([]string, 0, len(perShardByName))
	for name := range perShardByName {
		names = append(names, name)
	}
	sort.Strings(names)
	perShard := make([]neo4jv1beta1.ShardValidationStatus, 0, len(names))
	overall := "OK"
	for _, name := range names {
		s := perShardByName[name]
		perShard = append(perShard, s)
		if s.Status != "OK" {
			overall = "Degraded"
		}
	}
	return &neo4jv1beta1.BackupValidationResult{
		OverallStatus: overall,
		PerShard:      perShard,
		RawOutput:     truncateForRaw(logBody),
	}
}

// truncateForRaw keeps RawOutput within validateRawOutputCap so etcd
// doesn't get bloated. Appends an explicit marker so users know they're
// looking at a partial view.
func truncateForRaw(s string) string {
	if len(s) <= validateRawOutputCap {
		return s
	}
	return s[:validateRawOutputCap] + "\n…(truncated)"
}

// mergeShardArtifactsFromLog augments an "expected" shard-artifact list
// (Filename and Size empty, only ShardName populated by
// expectedShardArtifactsForBackup) with filename + size data parsed from
// the backup Pod's log. ShardName entries that don't appear in the log
// are left as-is, so consumers always see at least the expected shard
// names even if log parsing failed partway through.
//
// Returns the merged slice. Caller assigns into BackupRun.ShardArtifacts.
func mergeShardArtifactsFromLog(expected []neo4jv1beta1.ShardArtifact, fromLog map[string]neo4jv1beta1.ShardArtifact) []neo4jv1beta1.ShardArtifact {
	if len(fromLog) == 0 {
		return expected
	}
	out := make([]neo4jv1beta1.ShardArtifact, len(expected))
	copy(out, expected)
	for i := range out {
		if a, ok := fromLog[out[i].ShardName]; ok {
			if a.Filename != "" {
				out[i].Filename = a.Filename
			}
			if a.Size > 0 {
				out[i].Size = a.Size
			}
		}
	}
	return out
}
