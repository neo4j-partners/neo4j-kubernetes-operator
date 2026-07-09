#!/usr/bin/env bash
# Classic: image pulled from a registry (ACR, etc.).

export OPERATOR_CASE_NAME=registry-image
export OPERATOR_LEADER_ELECT="${OPERATOR_LEADER_ELECT:-false}"
export OPERATOR_IMAGE_PULL_POLICY=IfNotPresent
