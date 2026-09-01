#!/usr/bin/env bash
# assert/storage-grow-refused — pure assertion; verify.sh makes the StorageClass forbid expansion,
# asks for a larger volume, and observes the operator report the refusal instead of hiding it.
set -euo pipefail
true
