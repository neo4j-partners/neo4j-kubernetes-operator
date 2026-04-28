#!/usr/bin/env bash
#
# check-csv-coverage.sh
#
# Verifies that every CRD in config/crd/bases/ is registered in the
# OperatorHub bundle's ClusterServiceVersion under
# customresourcedefinitions.owned. The CSV is hand-curated (display names,
# descriptions are not auto-derived) so it's easy to forget a new CRD,
# which would silently ship a broken bundle.
#
# Exits non-zero with a list of missing kinds when CSV is out of sync.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
YQ="${YQ:-${ROOT}/bin/yq}"
CRD_DIR="${ROOT}/config/crd/bases"
CSV_BASE="${ROOT}/config/manifests/bases/neo4j-kubernetes-operator.clusterserviceversion.yaml"
CSV_BUNDLE="${ROOT}/bundle/manifests/neo4j-kubernetes-operator.clusterserviceversion.yaml"

if [[ ! -x "$YQ" ]]; then
    echo "error: yq not found at $YQ. Run 'make yq' first." >&2
    exit 1
fi

# Collect Kind names from CRD definitions.
crd_kinds=$(for f in "$CRD_DIR"/*.yaml; do
    "$YQ" '.spec.names.kind' "$f"
done | sort -u)

check_csv() {
    local label=$1 csv=$2
    if [[ ! -f "$csv" ]]; then
        echo "warn: $label CSV not found at $csv (skipping)" >&2
        return 0
    fi
    local owned
    owned=$("$YQ" '.spec.customresourcedefinitions.owned[].kind' "$csv" | sort -u)
    local missing
    missing=$(comm -23 <(echo "$crd_kinds") <(echo "$owned"))
    if [[ -n "$missing" ]]; then
        echo "error: $label CSV is missing CRDs:" >&2
        echo "$missing" | sed 's/^/   - /' >&2
        echo "       File: $csv" >&2
        return 1
    fi
    echo "ok: $label CSV covers all $(echo "$crd_kinds" | wc -l | tr -d ' ') CRDs"
}

rc=0
check_csv "source"  "$CSV_BASE"   || rc=1
check_csv "bundle"  "$CSV_BUNDLE" || rc=1
exit $rc
