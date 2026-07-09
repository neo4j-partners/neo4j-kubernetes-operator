#!/usr/bin/env bash
# Classic: image pre-loaded on kind nodes — never pull from a registry.

export OPERATOR_CASE_NAME=local-image
export OPERATOR_LEADER_ELECT="${OPERATOR_LEADER_ELECT:-false}"
export OPERATOR_IMAGE_PULL_POLICY=Never
