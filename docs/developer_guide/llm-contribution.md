# Contributing with an LLM agent

This is the on-ramp for an LLM agent (Claude Code or another agent-assisted
human) contributing to this repo. It tells you where to read, what will actually
stop you, and which procedures to invoke.

## Start here

Read [`AGENTS.md`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/AGENTS.md) **FIRST** — it is the front door: the
**canonical short list of invariants** and the home of the **Working principles**.
(The detailed, enforcement-tagged invariants — with recovery — live in
[`docs/knowledge/invariants.md`](../knowledge/invariants.md).) When prose
conflicts, the invariants win, and you trust the code and tests over any prose.
Then read this guide, then the deep reference in [`CLAUDE.md`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/CLAUDE.md).

## The 5 invariants & working principles

The five hard invariants in one line — **no admission webhooks · Kind-only
dev/test/CI · Enterprise images only · V2_ONLY discovery (port 6000) ·
server-based architecture (single `{cluster}-server` STS + Job-per-`Neo4jBackup`-CR)**.
Read the canonical short list in [`AGENTS.md`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/AGENTS.md) and the full
enforcement-tagged form — id, why, enforcement status, violation symptom,
recovery — in [`docs/knowledge/invariants.md`](../knowledge/invariants.md). Don't
re-derive them here.

The four **Working principles** from `AGENTS.md` apply to *every* change, on top
of the invariants:

- **Think before coding** — surface assumptions and tradeoffs; when a doc
  disagrees with the code, say so and ask.
- **Simplicity first** — the minimum code that solves the problem; nothing
  speculative; respect separation of concerns.
- **Surgical changes** — touch only what the task requires; preserve adjacent
  code and style (e.g. the env-var path is a subset-merge, never a wholesale
  replace).
- **Goal-driven execution** — turn the task into verifiable success criteria and
  loop until they pass.

## Guards vs gates (what will actually stop you)

Be precise about enforcement — only two CI gates block merge:

| Mechanism | Type | Blocks merge? |
|---|---|---|
| `make check-drift` (CI job *Generated Artifacts In Sync*) | BLOCKING CI gate | Yes |
| `make test-unit` (CI job *Unit Tests*) | BLOCKING CI gate | Yes |
| `make check-invariants` (CI job *Invariant Guards (advisory)*) | ADVISORY guard | No |
| `make check-knowledge-drift` (same advisory job) | ADVISORY guard | No |
| Runtime validators in `internal/validation/` | Reject a bad CR at apply | n/a (runtime) |

`make check-knowledge` runs both advisory guards together. They are advisory and
also run by the agent skills — but a bad CR is still rejected at runtime by the
inline validators, and a forbidden file/symbol reappearing will fail
`scripts/check-invariants.sh` in the advisory job (still worth fixing).

## The skills catalog

Invokable, step-by-step procedures live in `.claude/skills/`. Each is a
`SKILL.md` following the open `AGENTS.md`/`SKILL.md` convention, so any agent
(Claude Code or otherwise) can load it. Invoke one when you need to *perform* the
procedure rather than reason about it.

See [`.claude/skills/README.md`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/.claude/skills/README.md) for the grouped
catalog. The full set:

| Skill | Use when you need to… |
|---|---|
| `add-crd-field` | Add a field to an existing CRD end-to-end (struct + markers + regen + INLINE validation + test). |
| `add-controller` | Add a whole new controller/reconciler the project way (RBAC, finalizer, retry, inline validation, wired into `cmd/main.go`). |
| `add-inline-validator` | Add a spec validator inline in `internal/validation/` — NEVER an admission webhook (invariant 1). |
| `add-regression-rule` | Add a new invariant/regression rule to `docs/knowledge/` in the enforcement-tagged format. |
| `check-neo4j-docs` | Ground Neo4j config/Cypher/procedures in the version-correct manual (CalVer → `/current/`, LTS → `/5/`) instead of drifting to 4.x. |
| `cut-release` | Cut a tagged `vX.Y.Z` release through the gated pipeline (with a human go/no-go). |
| `fix-knowledge-drift` | Repair a stale `docs/knowledge/` reference (a cited test/path that no longer resolves). |
| `issue-hygiene` | Dedupe before filing, link PRs to issues, and close on the right trigger. |
| `regen-artifacts` | Regenerate generated artifacts after touching CRD types, `+kubebuilder:`, or RBAC markers. |
| `retract-release` | Safely retract a bad release via the gated retract workflow. |
| `run-extended-suite` | Run the extended integration tier (backup/restore, sharding, multi-node, MinIO). |
| `verify-journey` | Fresh-eyes verification — build from main, install per the published docs on a clean Kind cluster, walk the real user scenarios. |

## The verify-before-done loop

Unit tests prove the code against *itself* — necessary but not sufficient. Before
declaring done:

1. **Live-verify** in Kind or against a real DB — use the `verify-journey` skill
   to build from current main and walk the real scenarios.
2. Run `make check-knowledge` (the two advisory guards) so docs and code don't
   silently drift.
3. **Grep before you cite.** Anti-hallucination is non-negotiable: before
   referencing any file path, Go symbol, test name, or make target, confirm it
   exists in the tree (`grep`/read). A fabricated citation is the worst failure.
4. **Read the manual before you assert Neo4j behavior.** Training memory skews to
   deprecated 4.x. For any config setting, Cypher, procedure, or topology, check
   the version-correct operations manual — CalVer → `/current/`, LTS 5.26 → `/5/`
   — not memory. The external sibling of "grep before you cite"; see the
   `check-neo4j-docs` skill.

## Generated artifacts

Large parts of `config/`, `charts/`, and `api/v1beta1/zz_generated.deepcopy.go`
are generated — each carries a `# This file is GENERATED. DO NOT EDIT.` header.
**Never hand-edit them.** When you touch CRD types or `+kubebuilder:` markers,
regenerate everything with `make sync-all` (the `regen-artifacts` skill walks the
full sequence). `make check-drift` is the blocking gate: it reruns the generators
and fails on any diff.
