#!/usr/bin/env bash
# Classic: standard controller install (leader election off for single-replica e2e).

export OPERATOR_CASE_NAME=default
export OPERATOR_LEADER_ELECT="${OPERATOR_LEADER_ELECT:-false}"
export OPERATOR_IMAGE_PULL_POLICY="${OPERATOR_IMAGE_PULL_POLICY:-IfNotPresent}"
