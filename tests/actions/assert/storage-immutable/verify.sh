#!/usr/bin/env bash
# assert/storage-immutable — every storage change that cannot be applied is refused at admission.
#
# These are CEL transition rules (they read oldSelf), so nothing but a live UPDATE can exercise
# them: the unit test in src/api/v1beta1 proves they compile, not that they hold. And they have to
# hold at admission rather than in the reconciler, because each of these fields decides the shape of
# a StatefulSet volumeClaimTemplate — a set Kubernetes will not let anyone replace once the
# StatefulSet exists. A change the API accepted here could never be applied, which is how a storage
# edit came to be accepted, silently dropped, and still reported Ready (BDR-005, ADR-001 amended).
#
# The shrink guard is the one worth reading twice. It compares quantities rather than strings, so
# 5Gi -> 4000Mi is caught as the reduction it is; a string comparison would wave it through. That is
# what puts the supported Kubernetes floor at 1.35 — older API servers have no CEL quantity library.
#
# A grow is attempted last as the positive control: a rule set that refused everything would pass
# every rejection check and be useless.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

storage_wait_installed

RESOURCE="neo4j/${NEO4J_CR_NAME}"
failures=0

# The sizes below are written out rather than computed, so the labels say what is being attempted.
# That only reads true while the fixture still starts at 10Gi: were it lowered to 5Gi, the "shrink
# to 9000Mi" case would be a grow and would pass by being accepted for the wrong reason.
BASELINE="$(kubectl get "${RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.storage.volumes.data.dynamic.size}')"
[[ "${BASELINE}" == "10Gi" ]] \
  || die "this assert is written against a 10Gi baseline but the CR starts at ${BASELINE} — update the sizes below together with the fixture"

# refuse <label> <expected words in the message> <merge patch>
refuse() {
  local label=$1 expect=$2 patch=$3 out
  if out="$(kubectl patch "${RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge -p "${patch}" 2>&1)"; then
    printf '  FAILED  %-46s the API server ACCEPTED it\n' "${label}" >&2
    failures=$((failures + 1))
    return
  fi
  if ! grep -qi -- "${expect}" <<<"${out}"; then
    printf '  FAILED  %-46s refused, but not by the expected rule\n            %s\n' \
      "${label}" "$(tail -1 <<<"${out}")" >&2
    failures=$((failures + 1))
    return
  fi
  printf '  ok      %-46s refused\n' "${label}"
}

# accept <label> <merge patch>
accept() {
  local label=$1 patch=$2 out
  if out="$(kubectl patch "${RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge -p "${patch}" 2>&1)"; then
    printf '  ok      %-46s accepted\n' "${label}"
    return
  fi
  printf '  FAILED  %-46s the API server REFUSED it\n            %s\n' \
    "${label}" "$(tail -1 <<<"${out}")" >&2
  failures=$((failures + 1))
}

log "Attempting each forbidden storage transition on ${RESOURCE}"

# Capacity may only grow, and the comparison is on quantities, not strings.
refuse "shrink 10Gi -> 1Gi" "cannot be decreased" \
  '{"spec":{"storage":{"volumes":{"data":{"dynamic":{"size":"1Gi"}}}}}}'
refuse "shrink hidden by units 10Gi -> 9000Mi" "cannot be decreased" \
  '{"spec":{"storage":{"volumes":{"data":{"dynamic":{"size":"9000Mi"}}}}}}'

# The provisioning shape is fixed at PVC creation: a bound claim cannot be re-provisioned.
refuse "storageClassName change" "storageClassName is immutable" \
  '{"spec":{"storage":{"volumes":{"data":{"dynamic":{"storageClassName":"some-other-class"}}}}}}'
# accessMode is refused by its enum, not by the immutability rule: the enum admits ReadWriteOnce
# and nothing else, so no other value ever reaches CEL. The immutability rule behind it is the
# backstop for the day the enum widens — widening an enum is exactly the change someone makes
# without noticing it reopens a field the StatefulSet cannot re-provision.
refuse "accessMode change" 'Unsupported value: "ReadWriteMany"' \
  '{"spec":{"storage":{"volumes":{"data":{"dynamic":{"accessMode":"ReadWriteMany"}}}}}}'
refuse "data mode Dynamic -> Existing" "mode is immutable" \
  '{"spec":{"storage":{"volumes":{"data":{"mode":"Existing","dynamic":null,"existing":{"claimName":"somewhere-else"}}}}}}'
refuse "disableSubPathExpr flip" "disableSubPathExpr is immutable" \
  '{"spec":{"storage":{"volumes":{"data":{"disableSubPathExpr":true}}}}}'

# The set of claim templates is what actually wedges a StatefulSet, so the set of roles is frozen.
refuse "add an auxiliary volume" "cannot be added or removed" \
  '{"spec":{"storage":{"volumes":{"metrics":{"mode":"Share","shareFrom":"data"}}}}}'
refuse "remove an auxiliary volume" "cannot be added or removed" \
  '{"spec":{"storage":{"volumes":{"logs":null}}}}'
refuse "auxiliary mode Share -> Dynamic" "mode is immutable" \
  '{"spec":{"storage":{"volumes":{"logs":{"mode":"Dynamic","shareFrom":null,"dynamic":{"size":"1Gi"}}}}}}'
refuse "drop spec.storage entirely" "cannot be added or removed" \
  '{"spec":{"storage":null}}'

# Positive control, last: the one storage change that is allowed must still be allowed.
accept "grow 10Gi -> 12Gi" \
  '{"spec":{"storage":{"volumes":{"data":{"dynamic":{"size":"12Gi"}}}}}}'

[[ "${failures}" -eq 0 ]] \
  || die "${failures} storage transition(s) behaved wrongly at admission"

log "every forbidden storage transition refused, and a grow still accepted"
