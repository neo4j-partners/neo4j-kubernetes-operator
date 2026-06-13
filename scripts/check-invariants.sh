#!/usr/bin/env bash
#
# check-invariants.sh
#
# Machine-enforces the 5 hard architectural invariants of the Neo4j Enterprise
# Operator (see AGENTS.md / docs/knowledge/). This is a CI guard: it FAILS
# (exit 1) the moment a tracked file reintroduces a banned construct, naming
# the violated invariant and the offending file so the fix is obvious.
#
# Why this exists: LLM-assisted contributors frequently "helpfully" rebuild
# patterns the project has deliberately removed (admission webhooks, a
# centralized backup StatefulSet, primary/secondary pod naming, V1 discovery),
# swap in a non-Kind cluster, or pin a community image.
# Documentation alone did not stop drift, so the invariants are now enforced.
#
# Coverage: INV-1 (no webhooks), INV-2 (Kind only — operational paths),
# INV-3 (no community image in CRD/manifest surface; the RUNTIME check is in
# internal/validation/image_validator.go), INV-4 (no V1 discovery),
# INV-5 (no backup StatefulSet / spec.backups / primary-*,secondary-* names).
#
# Scoping rules that keep this green on the clean tree (no false positives):
#   - Go-symbol checks run against *.go ONLY, and SKIP *_test.go: negative
#     tests legitimately reference banned strings (e.g. "V1_ONLY" as an
#     invalid-input fixture in internal/validation/config_validator_test.go).
#   - YAML field checks are scoped to config/ + api/ (the manifest + CRD
#     surface); prose that merely MENTIONS a banned field in CLAUDE.md, docs/,
#     or docs/knowledge/ is never scanned.
#   - .claude/worktrees/ (agent scratch worktrees) and vendor/ are excluded
#     everywhere — they are not part of the shipped source of truth.
#   - Pod-name checks target hyphenated NAME fragments ("-primary-N" /
#     "-secondary-N") as built in string literals / Sprintf, NOT the bare
#     uppercase mode-constraint enums PRIMARY/SECONDARY, which appear
#     legitimately in topology validation (internal/resources/cluster.go).
#
# Portable to macOS bash 3.2 (the repo's baseline): no associative arrays,
# no mapfile, no `grep -P`.
#
# Usage: scripts/check-invariants.sh   (run from anywhere; cd's to repo root)

set -euo pipefail

# --- Locate repo root so the script works from any CWD ----------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# Paths excluded from EVERY scan: agent scratch worktrees + (defensive) vendor.
EXCLUDE_RE='(^|/)(\.claude/worktrees|vendor)/'

violations=0

# fail <invariant-name> <message...>
# Records a violation and prints a clearly-labelled diagnostic to stderr.
fail() {
  local inv="$1"; shift
  violations=$((violations + 1))
  echo "INVARIANT VIOLATED [${inv}]: $*" >&2
}

# tracked_go_nontest
#   Lists git-tracked *.go files, excluding *_test.go and excluded paths.
#   Used by every Go-source content check below.
tracked_go_nontest() {
  git ls-files '*.go' \
    | grep -Ev '_test\.go$' \
    | grep -Ev "${EXCLUDE_RE}" || true
}

# grep_go_nontest <extended-regex>
#   Greps the non-test Go sources for a pattern; prints "file:line:match"
#   for any hit. Empty output == clean.
grep_go_nontest() {
  local pat="$1"
  local files
  files="$(tracked_go_nontest)"
  [ -z "${files}" ] && return 0
  # shellcheck disable=SC2086
  printf '%s\n' ${files} | tr '\n' '\0' | xargs -0 grep -nE "${pat}" /dev/null 2>/dev/null || true
}

# -----------------------------------------------------------------------------
# INVARIANT 1 — NO admission webhooks.
#   The operator does ALL validation inline in internal/validation/, called
#   from reconcilers. There are no webhook configs and no _webhook.go files.
#
# Check 1a: no tracked file whose path matches '_webhook.go' or 'config/webhook'.
# -----------------------------------------------------------------------------
webhook_paths="$(git ls-files \
  | grep -E '_webhook\.go$|(^|/)config/webhook(/|$)' \
  | grep -Ev "${EXCLUDE_RE}" || true)"
if [ -n "${webhook_paths}" ]; then
  while IFS= read -r f; do
    [ -n "${f}" ] && fail "no-webhooks" \
      "webhook file/dir present (banned: _webhook.go / config/webhook): ${f}"
  done <<EOF
${webhook_paths}
EOF
fi

# Check 1b: no non-test *.go references ValidatingWebhookConfiguration or
# MutatingWebhookConfiguration (the API types you'd register a webhook with).
webhook_types="$(grep_go_nontest 'ValidatingWebhookConfiguration|MutatingWebhookConfiguration')"
if [ -n "${webhook_types}" ]; then
  while IFS= read -r line; do
    [ -n "${line}" ] && fail "no-webhooks" \
      "Go source references a webhook configuration type: ${line}"
  done <<EOF
${webhook_types}
EOF
fi

# -----------------------------------------------------------------------------
# INVARIANT 5a — NO centralized backup StatefulSet / spec.backups field.
#   Backups are Job-per-Neo4jBackup-CR ONLY (rule 79). The legacy
#   {cluster}-backup StatefulSet, the spec.backups field, BuildBackupStatefulSet
#   and the standalone backup sidecar were removed and must never return.
#
# Check 5a-i: no BuildBackupStatefulSet (or any Build*BackupStatefulSet builder)
#             in non-test Go sources.
# -----------------------------------------------------------------------------
backup_sts_builder="$(grep_go_nontest 'Build[A-Za-z]*BackupStatefulSet')"
if [ -n "${backup_sts_builder}" ]; then
  while IFS= read -r line; do
    [ -n "${line}" ] && fail "no-centralized-backup" \
      "centralized backup StatefulSet builder reintroduced: ${line}"
  done <<EOF
${backup_sts_builder}
EOF
fi

# Check 5a-ii: no 'spec.backups' / a top-level 'backups:' field in the CRD +
#             manifest surface (config/ + api/). YAML uses 'backups:'; Go CRD
#             types would surface it as a `json:"backups"` tag. Scoped to
#             config/ + api/ so prose mentions elsewhere don't trip it.
backups_field_files="$(git ls-files 'config/*' 'api/*' \
  | grep -Ev "${EXCLUDE_RE}" || true)"
if [ -n "${backups_field_files}" ]; then
  # YAML/manifest form: a 'backups:' key (any indent) under spec.
  backups_yaml="$(printf '%s\n' ${backups_field_files} \
    | tr '\n' '\0' \
    | xargs -0 grep -nE '^[[:space:]]*backups:' /dev/null 2>/dev/null || true)"
  if [ -n "${backups_yaml}" ]; then
    while IFS= read -r line; do
      [ -n "${line}" ] && fail "no-spec-backups" \
        "'backups:' field reintroduced in CRD/manifest surface: ${line}"
    done <<EOF
${backups_yaml}
EOF
  fi
  # Go CRD-type form: a json tag named "backups".
  backups_jsontag="$(printf '%s\n' ${backups_field_files} \
    | tr '\n' '\0' \
    | xargs -0 grep -nE 'json:"backups[",]' /dev/null 2>/dev/null || true)"
  if [ -n "${backups_jsontag}" ]; then
    while IFS= read -r line; do
      [ -n "${line}" ] && fail "no-spec-backups" \
        "spec.backups field reintroduced in CRD Go types: ${line}"
    done <<EOF
${backups_jsontag}
EOF
  fi
fi

# -----------------------------------------------------------------------------
# INVARIANT 4 — V2_ONLY discovery.
#   The only legal discovery version is V2_ONLY. Legacy V1 discovery
#   (dbms.cluster.discovery.version set to V1 or V1_ONLY) is banned in emitted
#   config. We scan non-test Go (the config builders) — *_test.go is excluded
#   because negative tests legitimately feed "V1_ONLY" as an invalid fixture.
# -----------------------------------------------------------------------------
v1_discovery="$(grep_go_nontest 'discovery\.version[^A-Za-z0-9]*=?[^A-Za-z0-9]*"?V1(_ONLY)?"?')"
if [ -n "${v1_discovery}" ]; then
  while IFS= read -r line; do
    [ -n "${line}" ] && fail "v2-only-discovery" \
      "legacy V1 discovery version emitted (must be V2_ONLY): ${line}"
  done <<EOF
${v1_discovery}
EOF
fi

# -----------------------------------------------------------------------------
# INVARIANT 5b — Server-based pod naming (NEVER primary-*/secondary-*).
#   Pods are {cluster}-server-0..N-1 from a single StatefulSet. Constructing
#   pod names like {cluster}-primary-N / {cluster}-secondary-N is banned.
#
#   We target hyphenated NAME fragments ("-primary-" / "-secondary-") as they
#   appear in string literals or fmt.Sprintf — NOT the bare uppercase enums
#   PRIMARY/SECONDARY, which are legitimate topology mode constraints in
#   internal/resources/cluster.go. Scoped to internal/resources (the K8s
#   resource builders, where pod names are actually constructed).
# -----------------------------------------------------------------------------
resource_go="$(git ls-files 'internal/resources/*.go' \
  | grep -Ev '_test\.go$' \
  | grep -Ev "${EXCLUDE_RE}" || true)"
if [ -n "${resource_go}" ]; then
  # Match a hyphenated -primary-/-secondary- fragment inside a double-quoted
  # string literal (e.g. "-primary-", "%s-secondary-%d"). Case-sensitive lower
  # so the topology enum constants (PRIMARY/SECONDARY) are never matched.
  podname="$(printf '%s\n' ${resource_go} \
    | tr '\n' '\0' \
    | xargs -0 grep -nE '"[^"]*-(primary|secondary)-' /dev/null 2>/dev/null || true)"
  if [ -n "${podname}" ]; then
    while IFS= read -r line; do
      [ -n "${line}" ] && fail "server-based-pod-naming" \
        "primary-/secondary- pod-name construction (use {cluster}-server-N): ${line}"
    done <<EOF
${podname}
EOF
  fi
fi

# -----------------------------------------------------------------------------
# INVARIANT 2 — Kind ONLY for dev/test/CI.
#   No minikube / k3s / k3d (or other local-K8s) provisioning in the
#   OPERATIONAL surface: the Makefile, hack/, scripts/, and .github/workflows/
#   are the paths that actually create clusters. Prose mentions in docs and
#   CONTRIBUTING ("we do NOT use minikube/k3s") and per-distro storageClass
#   hints in examples/ are intentionally NOT scanned — they are not provisioners.
#   This guard script itself is excluded: it names the banned tools on purpose.
# -----------------------------------------------------------------------------
kind_only_files="$(git ls-files 'Makefile' 'hack/*' 'scripts/*' '.github/workflows/*' \
  | grep -Ev "${EXCLUDE_RE}" \
  | grep -Ev '(^|/)scripts/check-invariants\.sh$' || true)"
if [ -n "${kind_only_files}" ]; then
  other_distro="$(printf '%s\n' ${kind_only_files} \
    | tr '\n' '\0' \
    | xargs -0 grep -niE 'minikube|k3d|k3s' /dev/null 2>/dev/null || true)"
  if [ -n "${other_distro}" ]; then
    while IFS= read -r line; do
      [ -n "${line}" ] && fail "kind-only" \
        "non-Kind cluster provisioner in an operational path (Kind only): ${line}"
    done <<EOF
${other_distro}
EOF
  fi
fi

# -----------------------------------------------------------------------------
# INVARIANT 3 — Enterprise images only (static manifest/sample check).
#   The shipped CRDs/manifests/samples must never pin a Neo4j *community*
#   image. Scoped to config/ + api/ and matched ONLY in a version-tag context
#   ('[0-9][0-9.]*-community', e.g. 5.26.0-community / 2025.01.0-community) so
#   the Neo4jPlugin `source: community` enum and "git.k8s.io/community" doc
#   links are NEVER mistaken for an image edition. The RUNTIME rejection of a
#   community-tagged CR lives in internal/validation/image_validator.go
#   (isCommunityTag, pinned by image_validator_test.go); this static check
#   stops a community tag from sneaking into a sample or generated manifest.
# -----------------------------------------------------------------------------
enterprise_files="$(git ls-files 'config/*' 'api/*' \
  | grep -Ev "${EXCLUDE_RE}" || true)"
if [ -n "${enterprise_files}" ]; then
  community_img="$(printf '%s\n' ${enterprise_files} \
    | tr '\n' '\0' \
    | xargs -0 grep -niE '[0-9][0-9.]*-community' /dev/null 2>/dev/null || true)"
  if [ -n "${community_img}" ]; then
    while IFS= read -r line; do
      [ -n "${line}" ] && fail "enterprise-images" \
        "community-tagged Neo4j image in CRD/manifest surface (Enterprise only): ${line}"
    done <<EOF
${community_img}
EOF
  fi
fi

# -----------------------------------------------------------------------------
# Verdict
# -----------------------------------------------------------------------------
if [ "${violations}" -ne 0 ]; then
  echo "" >&2
  echo "check-invariants: FAILED with ${violations} invariant violation(s)." >&2
  echo "See AGENTS.md / docs/knowledge/ for why these constructs are banned." >&2
  exit 1
fi

echo "check-invariants: OK — all 5 hard invariants hold."
