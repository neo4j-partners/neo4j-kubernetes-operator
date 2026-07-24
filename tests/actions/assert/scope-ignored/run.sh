#!/usr/bin/env bash
# assert/scope-ignored — pure assertion; the CR was applied by the case_run phase
# (scope/apply-unwatched). verify.sh watches for the ABSENCE of reconciliation.
set -euo pipefail
true
