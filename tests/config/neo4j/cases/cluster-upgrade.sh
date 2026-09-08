#!/usr/bin/env bash
# Cluster with 3 primaries and a read pool, deployed one version behind the pin so the suite can
# roll it forward. Sourced after neo4j/base.sh, which is what lets NEO4J_VERSION be overridden here.

export NEO4J_CASE_NAME=cluster-upgrade
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-upgrade}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3

# Read before NEO4J_VERSION is rewritten below: the target is whatever this run is testing, which
# is the pin unless CI passed a version in.
export NEO4J_UPGRADE_TO="${NEO4J_UPGRADE_TO:-${NEO4J_VERSION}}"
export NEO4J_VERSION="${NEO4J_VERSION_UPGRADE_FROM}"

# The read pool the fixture declares. Its StatefulSet is the one that must finish rolling before
# the primary pool's pod template is allowed to change.
export CLUSTER_READ_MEMBERS=1

# A 4-member roll, each member stopping and starting a real database, and it must not be cut short
# by a budget sized for a single restart.
export E2E_UPGRADE_TIMEOUT="${E2E_UPGRADE_TIMEOUT:-1800}"
