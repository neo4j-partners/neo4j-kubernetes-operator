#!/usr/bin/env bash
#
# helm-sync-artifacthub-crds.sh
#
# Regenerates the `artifacthub.io/crds` annotation in
# charts/neo4j-operator/Chart.yaml from the CRDs on disk so that ArtifactHub
# always advertises the full set of resources the chart installs.
#
# Each CRD's description is taken from a curated mapping below — generated
# YAML schema descriptions are too verbose for ArtifactHub. To add a new
# CRD, drop a row into describe() below.
#
# Portable to macOS bash 3.2 (no associative arrays).

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
YQ="${YQ:-${ROOT}/bin/yq}"
CHART="${ROOT}/charts/neo4j-operator/Chart.yaml"
CRD_DIR="${ROOT}/config/crd/bases"

if [[ ! -x "$YQ" ]]; then
    echo "error: yq not found at $YQ. Run 'make yq' first." >&2
    exit 1
fi

# Curated descriptions per Kind. Order doesn't matter; lookup is by Kind.
# Anything missing here is reported and the script exits non-zero so a
# newly-added CRD without a description doesn't silently lose its entry.
describe() {
    case "$1" in
        Neo4jEnterpriseCluster)    echo "Manages Neo4j Enterprise cluster deployments" ;;
        Neo4jEnterpriseStandalone) echo "Manages Neo4j Enterprise standalone deployments" ;;
        Neo4jDatabase)             echo "Manages Neo4j databases within clusters" ;;
        Neo4jShardedDatabase)      echo "Manages Neo4j property-sharded databases (Infinigraph, GA in 2025.12+)" ;;
        Neo4jBackup)               echo "Manages Neo4j backup operations" ;;
        Neo4jRestore)              echo "Manages Neo4j restore operations" ;;
        Neo4jPlugin)               echo "Manages Neo4j plugin installations (APOC, GDS, Bloom, etc.)" ;;
        Neo4jUser)                 echo "Declarative Neo4j user management (passwords from Secrets, role bindings, status, external auth)" ;;
        Neo4jRole)                 echo "Declarative Neo4j role management with privilege-drift reconciliation" ;;
        Neo4jRoleBinding)          echo "Role grants for users provisioned externally (SSO/LDAP/OIDC first-login)" ;;
        Neo4jAuthRule)             echo "Attribute-based access control (ABAC) — claims-to-roles mapping evaluated at OIDC authentication time (Neo4j 2026.03+)" ;;
        *)                         return 1 ;;
    esac
}

BLOCK_FILE=$(mktemp)
trap 'rm -f "$BLOCK_FILE"' EXIT

missing=""
for f in $(ls "$CRD_DIR"/*.yaml | sort); do
    kind=$("$YQ" '.spec.names.kind' "$f")
    version=$("$YQ" '.spec.versions[0].name' "$f")
    if ! desc=$(describe "$kind"); then
        missing="${missing} ${kind}"
        continue
    fi
    # Lines start at column 0; yq re-indents under the literal-block parent.
    cat >> "$BLOCK_FILE" <<EOF
- kind: ${kind}
  version: ${version}
  description: ${desc}
EOF
done

if [[ -n "$missing" ]]; then
    echo "error: no description mapped for CRD kinds:${missing}" >&2
    echo "       add a case to describe() in $0" >&2
    exit 1
fi

# Replace the artifacthub.io/crds annotation with the new payload, in
# literal-block style so it renders as a multiline string under the key.
PAYLOAD=$(cat "$BLOCK_FILE") "$YQ" -i \
    '.annotations."artifacthub.io/crds" = strenv(PAYLOAD) | .annotations."artifacthub.io/crds" style="literal"' \
    "$CHART"

echo "Updated $CHART"
