#!/usr/bin/env bash
# The job summary GitHub renders on a run's page — one table per suite, one row per case.
#
# Writes GitHub-flavoured Markdown to GITHUB_STEP_SUMMARY when Actions provides it, and does
# nothing whatsoever otherwise, so a laptop run is unaffected and no caller needs to test for CI.
#
# GITHUB_STEP_SUMMARY is per *step*, not per job, which is what makes one implementation serve both
# shapes the workflows use: ci.yml gives each suite its own job, so each gets its own summary, while
# the cloud legs run all fourteen suites inside a single step and their tables accumulate into one
# summary. That is why every table names its own suite and platform instead of relying on the job
# title to say which one it is.
#
# The point is that a failure is legible without opening the log: which case, on which platform, on
# which versions. The log stays the place to find out *why* — a summary that reproduced it would
# hit the 1 MiB per-step cap and bury the one row that matters.

# Whether there is anywhere to write. Every function below is a no-op when this is false.
summary_active() {
  [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]
}

# A cell cannot contain an unescaped pipe or a newline: either one ends the row early and GitHub
# renders the rest of the table as prose. Case comments are free text from the suite files, so they
# are the ones that would do it.
_summary_cell() {
  local text=$1
  text="${text//|/\\|}"
  printf '%s' "${text//$'\n'/ }"
}

# Seconds as a compact duration. A case can run from under a second to several minutes, and
# "1m14s" is easier to scan down a column than "74".
_summary_duration() {
  local total=$1
  if ((total < 60)); then
    printf '%ds' "${total}"
  else
    printf '%dm%02ds' "$((total / 60))" "$((total % 60))"
  fi
}

# Opens a suite's section: a heading naming the platform and the versions under test, then the
# table header the case rows will land in.
summary_suite_header() { # <suite> <total cases>
  summary_active || return 0
  local suite=$1 total=$2
  {
    printf '\n### %s — %s\n\n' "$(_summary_cell "${suite}")" "${CLOUD_ID:-unknown platform}"
    printf 'Kubernetes `%s` · Neo4j `%s` · %s case(s)\n\n' \
      "${KUBERNETES_VERSION:-?}" "${NEO4J_VERSION:-?}" "${total}"
    printf '| | Case | Time | Covers |\n'
    printf '|---|---|---|---|\n'
  } >>"${GITHUB_STEP_SUMMARY}"
}

# One case. Status is pass, fail or skip; the reason column carries the case comment, or why it was
# skipped, which is the part that saves opening the log.
summary_case_row() { # <status> <case id> <seconds> <covers>
  summary_active || return 0
  local status=$1 id=$2 seconds=$3 covers=$4
  local mark
  case "${status}" in
    pass) mark='ok' ;;
    fail) mark='**FAIL**' ;;
    skip) mark='skip' ;;
    *) mark="${status}" ;;
  esac
  printf '| %s | `%s` | %s | %s |\n' \
    "${mark}" "$(_summary_cell "${id}")" "$(_summary_duration "${seconds}")" \
    "$(_summary_cell "${covers:-—}")" >>"${GITHUB_STEP_SUMMARY}"
}

# Closes a suite's section with the counts. On failure it also names the run id, because that is
# the directory the workflow uploads as an artifact — without it the summary says a case failed and
# leaves no way to reach the evidence.
summary_suite_footer() { # <suite> <total> <passed> <failed> <skipped> <seconds>
  summary_active || return 0
  local suite=$1 total=$2 passed=$3 failed=$4 skipped=$5 seconds=$6
  # `if` rather than `((...)) && assignment`, the convention the rest of the harness follows: a
  # false test leaves the list non-zero, and as a function's last statement that returns non-zero
  # to a caller running under `set -e`. Harmless in the middle of a function like this one, fatal
  # the day someone reorders it.
  local counts="${passed} passed"
  if ((failed > 0)); then
    counts="${counts}, ${failed} failed"
  fi
  if ((skipped > 0)); then
    counts="${counts}, ${skipped} skipped"
  fi
  # A suite whose on_case_failure is `stop` abandons the remaining cases. Without this the counts
  # simply would not add up to the total announced in the header, which reads like a bug in the
  # summary rather than the suite having stopped early.
  local unrun=$((total - passed - failed - skipped))
  if ((unrun > 0)); then
    counts="${counts}, ${unrun} not run"
  fi
  {
    printf '\n%s in %s.' "${counts}" "$(_summary_duration "${seconds}")"
    if ((failed > 0)); then
      # The artifact's full name carries the suite and the run id, which this script has no reason
      # to reconstruct; the path inside it is the part that is hard to guess.
      printf ' Diagnostics for the failed case(s): `runs/%s-<case>/` in this run'"'"'s `e2e-results-…` artifact.' \
        "${RUN_ID:-unknown}"
    fi
    printf '\n'
  } >>"${GITHUB_STEP_SUMMARY}"
}

# A suite that never ran, because the suite file restricts it to other platforms. Recorded rather
# than passed over in silence: "no table for feature-tls" is indistinguishable from a suite that
# was dropped from the run by mistake.
summary_suite_skipped() { # <suite> <reason>
  summary_active || return 0
  printf '\n### %s — %s\n\nSkipped: %s\n' \
    "$(_summary_cell "$1")" "${CLOUD_ID:-unknown platform}" "$(_summary_cell "$2")" \
    >>"${GITHUB_STEP_SUMMARY}"
}
