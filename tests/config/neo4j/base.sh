#!/usr/bin/env bash
# Neo4j workload base pins — see neo4j/base.yaml

export NEO4J_API_VERSION="${NEO4J_API_VERSION:-neo4j.com/v1beta1}"
export NEO4J_KIND="${NEO4J_KIND:-Neo4j}"
export NEO4J_NAMESPACE="${NEO4J_NAMESPACE:-default}"
export NEO4J_EDITION="${NEO4J_EDITION:-enterprise}"
export NEO4J_VERSION="${NEO4J_VERSION:-2026.05.0}"
export NEO4J_LICENSE_ACCEPT="${NEO4J_LICENSE_ACCEPT:-yes}"
export NEO4J_TOPOLOGY_MODE="${NEO4J_TOPOLOGY_MODE:-Standalone}"

export NEO4J_STANDALONE_FIXTURE="${NEO4J_STANDALONE_FIXTURE:-tests/fixtures/neo4j-standalone.yaml}"

export E2E_ASSERT_TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
export E2E_ASSERT_NEO4J_READY="${E2E_ASSERT_NEO4J_READY:-false}"
