#!/usr/bin/env bash
#
# Parse a Ginkgo --json-report and emit, to stdout and (in GitHub Actions) the job
# step summary:
#   1. ❌ Failed specs — name, file:line, and the failure message, for every
#      failed/panicked/timed-out spec. Replaces "check the logs" with the actual
#      reason, inline.
#   2. ⏱️ Slowest specs — per-spec RunTime, slowest first. Use this to see which
#      specs are formation-latency-bound (won't shrink with CPU) vs CPU-bound
#      (will), and how much parallelism is actually available.
#
# Best-effort by design: a missing/empty/malformed report, or missing jq, prints a
# notice and exits 0 — it must never fail the build.
#
# Usage: ginkgo-report-summary.sh [report.json] [topN]
set -uo pipefail

REPORT="${1:-ginkgo-report.json}"
TOPN="${2:-30}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found — skipping Ginkgo report summary."
  exit 0
fi
if [[ ! -s "$REPORT" ]]; then
  echo "No Ginkgo JSON report at '$REPORT' — skipping report summary."
  exit 0
fi

OUT="${GITHUB_STEP_SUMMARY:-/dev/null}"

# Only LeafNodeType=="It" entries are real specs; BeforeSuite/AfterSuite/etc.
# also appear in SpecReports (empty names, ~0s) and would otherwise show as junk
# rows and inflate the count.
fail_count="$(jq -r '
  [ .[].SpecReports[]? | select(.LeafNodeType=="It" and (.State=="failed" or .State=="panicked" or .State=="timedout" or .State=="interrupted")) ] | length
' "$REPORT" 2>/dev/null || echo 0)"

{
  # ── Failed specs (only when there are any) ───────────────────────────────────
  if [[ "${fail_count:-0}" -gt 0 ]]; then
    echo "## ❌ Failed specs (${fail_count})"
    echo
    jq -r --argjson fail '["failed","panicked","timedout","interrupted"]' '
      [ .[].SpecReports[]?
        | select(.LeafNodeType == "It")
        | select(.State as $s | $fail | index($s))
        | { name:  ((((.ContainerHierarchyTexts // []) + [.LeafNodeText])
                     | map(select(. != null and . != ""))) | join(" › ")),
            state: .State,
            loc:   (((.Failure.Location.FileName   // .LeafNodeLocation.FileName   // "?"))
                    + ":" +
                    ((.Failure.Location.LineNumber // .LeafNodeLocation.LineNumber // 0) | tostring)),
            msg:   ((.Failure.Message // "(no failure message captured)") | .[0:1200]) } ]
      | .[]
      | "#### ❌ \(.name)  _(\(.state))_\n\n- **Where:** `\(.loc)`\n\n<details><summary>failure message</summary>\n\n```\n\(.msg)\n```\n\n</details>\n"
    ' "$REPORT" 2>/dev/null
    echo
  fi

  # ── Slowest specs (always) ───────────────────────────────────────────────────
  read -r total_secs ran <<<"$(jq -r '
    [ .[].SpecReports[]? | select(.LeafNodeType == "It" and (.State=="passed" or .State=="failed")) ] as $s
    | "\((([$s[].RunTime] | add) // 0) / 1000000000 | floor) \($s | length)"' "$REPORT" 2>/dev/null)"

  echo "## ⏱️ Slowest specs (top ${TOPN})"
  echo
  echo "Ran **${ran:-0}** specs, **${total_secs:-0}s** summed spec RunTime"
  echo "_(sum of per-spec time; excludes BeforeSuite/cluster setup. Serial sum ≈ wall-clock minus setup.)_"
  echo
  echo "| Seconds | State | Spec |"
  echo "|--:|:--|:--|"
  jq -r --argjson n "$TOPN" '
    [ .[].SpecReports[]?
      | select(.LeafNodeType == "It")
      | select(.State=="passed" or .State=="failed")
      | { secs:  (.RunTime / 1000000000),
          state: .State,
          name:  ((((.ContainerHierarchyTexts // []) + [.LeafNodeText])
                   | map(select(. != null and . != ""))) | join(" › ")) } ]
    | sort_by(.secs) | reverse | .[:$n]
    | .[] | "| \(.secs | floor) | \(.state) | \(.name) |"
  ' "$REPORT" 2>/dev/null
} | tee -a "$OUT"
