# Agent skills catalog

Invokable, step-by-step **procedures** for working on the Neo4j Enterprise
Operator. Each skill is a `SKILL.md` with YAML frontmatter (`name`,
`description`) — the cross-agent convention, so Claude Code and any other
agent that understands the `AGENTS.md` / `SKILL.md` format can load them.

**Skills vs. the rest of the system:**
- [`AGENTS.md`](../../AGENTS.md) — the always-on constitution (invariants +
  working principles). *What is true / how to behave.*
- [`docs/knowledge/`](../../docs/knowledge/) — enforcement-tagged invariants and
  regression rules. *Why a rule exists and what enforces it.*
- **These skills** — *how to perform a specific task*, end to end. Invoke one
  when you need to **do** the procedure rather than reason it out from scratch.

## When to reach for one

| You need to… | Skill |
|---|---|
| **Build features** | |
| Add a field to an existing CRD (struct → markers → regen → inline validation → test) | [`add-crd-field`](add-crd-field/SKILL.md) |
| Add a whole new controller/reconciler the project way (RBAC, finalizer, retry, inline validation, wired into `cmd/main.go`) | [`add-controller`](add-controller/SKILL.md) |
| Add a spec validator — inline in `internal/validation/`, **never** a webhook (invariant 1) | [`add-inline-validator`](add-inline-validator/SKILL.md) |
| Regenerate CRDs / deepcopy / RBAC / chart / bundle after a type or marker change | [`regen-artifacts`](regen-artifacts/SKILL.md) |
| **Verify & ground** | |
| Ground Neo4j config / Cypher / procedures in the **version-correct** manual (CalVer → `/current/`, LTS → `/5/`) instead of drifting to 4.x | [`check-neo4j-docs`](check-neo4j-docs/SKILL.md) |
| Fresh-eyes end-to-end: build from `main`, install per the published docs on a clean Kind cluster, walk the real user journeys | [`verify-journey`](verify-journey/SKILL.md) |
| Repair a stale `docs/knowledge/` citation (a test/path that no longer resolves) | [`fix-knowledge-drift`](fix-knowledge-drift/SKILL.md) |
| Run the extended integration tier (backup/restore, sharding, multi-node, MinIO) | [`run-extended-suite`](run-extended-suite/SKILL.md) |
| **Knowledge & process** | |
| Add a new invariant/regression rule to `docs/knowledge/` in the enforcement-tagged format | [`add-regression-rule`](add-regression-rule/SKILL.md) |
| Keep GitHub issues honest — dedupe, link PRs, close on the right trigger | [`issue-hygiene`](issue-hygiene/SKILL.md) |
| **Release** | |
| Cut a tagged `vX.Y.Z` release through the gated pipeline (human go/no-go before the tag) | [`cut-release`](cut-release/SKILL.md) |
| Safely retract a bad release (probe the grace window, re-cut or supersede) | [`retract-release`](retract-release/SKILL.md) |

## Provenance

These skills are distilled from real work on this repo — release cuts,
fresh-eyes journeys that caught docs lying, drift the knowledge base flagged,
and the version-grounding habit that repeatedly corrected 4.x-era drift. Each
skill's own `## Why this exists / provenance` section records the episode that
earned it. When you discover a durable procedure, add it here in the same shape
rather than re-deriving it next time.
