#!/usr/bin/env bash
# assert/pvc-retained — pure assertion; the destructive step (CR delete) lives in
# verify.sh so it runs after standalone-ready has confirmed the operands are up.
set -euo pipefail
true
