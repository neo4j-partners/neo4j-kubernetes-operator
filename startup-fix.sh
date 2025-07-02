#!/bin/bash
set -e
echo "Starting Neo4j Enterprise in Single Mode..."
echo "Skipping DNS resolution for single node"
exec /docker-entrypoint.sh neo4j
