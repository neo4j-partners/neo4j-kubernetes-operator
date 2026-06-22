#!/bin/bash

# Script to run Go tests while filtering macOS ARM64 linker warnings
# These warnings are known issues with Go 1.24+ on macOS ARM64 and are not harmful

set -e
set -o pipefail

# Create a temporary file to capture output
temp_output=$(mktemp)
cleanup() {
    rm -f "$temp_output"
}
trap cleanup EXIT

# Run the command and capture output
if "$@" 2>&1 | \
    grep -v "ld: warning.*has malformed LC_DYSYMTAB, expected.*undefined symbols" | \
    grep -v "ld: warning.*/go-link-.*/000013.o.*LC_DYSYMTAB" | \
    tee "$temp_output"; then

    # Check if there were any test failures in the output
    if grep -q "^FAIL" "$temp_output"; then
        exit 1
    fi
    exit 0
else
    # Command failed, preserve exit code
    exit_code=$?
    if grep -q "^FAIL" "$temp_output"; then
        exit 1
    fi
    exit $exit_code
fi
