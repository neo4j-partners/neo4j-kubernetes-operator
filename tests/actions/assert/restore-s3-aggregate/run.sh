#!/usr/bin/env bash
# assert/restore-s3-aggregate has no side effects — all checks live in verify.sh (the Neo4jRestore is
# applied by backup/s3-aggregate). run.sh exists only to satisfy the run_action(full) contract.
true
