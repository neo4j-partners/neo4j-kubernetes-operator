#!/usr/bin/env bash
# Run all kubernetes-operator audit tools and produce a combined report.
# Usage: operator-audit.sh [operator-repo-root]
set -euo pipefail

ROOT="${1:-.}"
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXIT=0

echo "=== Operator audit: $ROOT ==="
echo ""

CRD_PATH="$ROOT/config/crd"
if [[ ! -d "$CRD_PATH" ]]; then
  CRD_PATH="$ROOT/config/crd/bases"
fi
if [[ -d "$CRD_PATH" ]]; then
  echo "--- CRD validator ---"
  python3 "$SKILL_DIR/scripts/crd_validator.py" --crd "$CRD_PATH" || EXIT=1
  echo ""
else
  echo "--- CRD validator: SKIP (no config/crd/) ---"
  echo ""
fi

CTRL_PATH="$ROOT/src/internal/controller"
if [[ ! -d "$CTRL_PATH" ]]; then
  CTRL_PATH="$ROOT/internal/controller"
fi
if [[ ! -d "$CTRL_PATH" ]]; then
  CTRL_PATH="$ROOT/controllers"
fi
if [[ -d "$CTRL_PATH" ]] || [[ -f "$CTRL_PATH" ]]; then
  echo "--- Reconcile linter ---"
  python3 "$SKILL_DIR/scripts/reconcile_lint.py" --controller "$CTRL_PATH" || EXIT=1
  echo ""
else
  echo "--- Reconcile linter: SKIP (no controller dir) ---"
  echo ""
fi

exit "$EXIT"
