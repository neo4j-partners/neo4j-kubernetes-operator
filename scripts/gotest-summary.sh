#!/usr/bin/env bash
#
# Parse a `go test -json` event stream (as written by `gotestsum --jsonfile`) and
# emit, to stdout and (in GitHub Actions) the job step summary:
#   1. ❌ Failed unit tests — package › test name (replaces the old grep count
#      that gave numbers but not names).
#   2. ⏱️ Slowest unit tests — by elapsed time.
#
# Best-effort: missing/empty file or no jq → prints a notice and exits 0.
#
# Usage: gotest-summary.sh [unit-tests.json] [topN]
set -uo pipefail

JSON="${1:-unit-tests.json}"
TOPN="${2:-20}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found — skipping unit-test summary."
  exit 0
fi
if [[ ! -s "$JSON" ]]; then
  echo "No go-test JSON at '$JSON' — skipping unit-test summary."
  exit 0
fi

OUT="${GITHUB_STEP_SUMMARY:-/dev/null}"

# test2json is NDJSON (one event per line); -s slurps into an array. Test-level
# pass/fail events carry .Test and .Elapsed; package-level events omit .Test.
fail_count="$(jq -rs '[ .[] | select(.Action=="fail" and (.Test != null)) ] | length' "$JSON" 2>/dev/null || echo 0)"

{
  if [[ "${fail_count:-0}" -gt 0 ]]; then
    echo "## ❌ Failed unit tests (${fail_count})"
    echo
    jq -rs '
      [ .[] | select(.Action=="fail" and (.Test != null))
        | "- `\(.Package) › \(.Test)`  _(\((.Elapsed // 0))s)_" ]
      | unique | .[]
    ' "$JSON" 2>/dev/null
    echo
  fi

  echo "## ⏱️ Slowest unit tests (top ${TOPN})"
  echo
  echo "| Seconds | Package › Test |"
  echo "|--:|:--|"
  jq -rs --argjson n "$TOPN" '
    [ .[] | select((.Action=="pass" or .Action=="fail") and (.Test != null))
      | { secs: (.Elapsed // 0), name: "\(.Package) › \(.Test)" } ]
    | sort_by(.secs) | reverse | .[:$n]
    | .[] | "| \(.secs) | \(.name) |"
  ' "$JSON" 2>/dev/null
} | tee -a "$OUT"
