#!/usr/bin/env bash
#
# sync-kustomize.sh
#
# Regenerates the `resources:` lists in:
#   - config/crd/kustomization.yaml      (drives kustomize / make bundle)
#   - config/samples/kustomization.yaml  (drives the CSV's alm-examples)
#
# The lists are derived from on-disk filenames so that adding a new CRD
# (and its sample) is a one-step operation: drop the file, run sync.
# Comments and other top-level keys in the kustomizations are preserved
# (yq v4 handles comment retention).

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
YQ="${YQ:-${ROOT}/bin/yq}"

if [[ ! -x "$YQ" ]]; then
    echo "error: yq not found at $YQ. Run 'make yq' first." >&2
    exit 1
fi

# Build a sorted, comma-separated YAML array of "<prefix><basename>" entries
# from the matching files. Empty if no files match.
build_array() {
    local pattern=$1 prefix=$2
    local files
    files=$(ls $pattern 2>/dev/null | xargs -n1 basename | sort | sed -E "s|^|\"${prefix}|; s|$|\"|" | paste -sd, -)
    echo "[${files}]"
}

# config/crd/kustomization.yaml — entries live under bases/.
CRD_DIR="${ROOT}/config/crd"
CRD_KUSTOMIZATION="${CRD_DIR}/kustomization.yaml"
CRD_RESOURCES=$(build_array "${CRD_DIR}/bases/*.yaml" "bases/")
"$YQ" -i ".resources = ${CRD_RESOURCES}" "$CRD_KUSTOMIZATION"
echo "Updated $CRD_KUSTOMIZATION"

# config/samples/kustomization.yaml — flat directory, no prefix.
SAMPLES_DIR="${ROOT}/config/samples"
SAMPLES_KUSTOMIZATION="${SAMPLES_DIR}/kustomization.yaml"
SAMPLES_RESOURCES=$(build_array "${SAMPLES_DIR}/neo4j_*.yaml" "")
"$YQ" -i ".resources = ${SAMPLES_RESOURCES}" "$SAMPLES_KUSTOMIZATION"
echo "Updated $SAMPLES_KUSTOMIZATION"
