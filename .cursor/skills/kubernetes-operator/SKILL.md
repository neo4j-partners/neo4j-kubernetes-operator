---
name: kubernetes-operator
description: >-
  Builds and audits Kubernetes Operators (reconcile loops, CRD design, controller-runtime,
  kubebuilder). Use when implementing controllers, CRDs, finalizers, status/conditions,
  or webhooks. Ships stdlib Python tools (CRD validator, reconcile linter), reference
  docs, and Go/YAML templates. Invoke with @kubernetes-operator. Complements Neo4j-specific
  skills — not a generic kubectl skill.
---

# Kubernetes Operator

Build operators that reconcile correctly. Most operator bugs are reconcile-loop bugs: missing finalizers, blocking calls, no requeue on transient errors, status drift, RBAC over-grants. This skill catches them deterministically before they reach a cluster.

**Upstream:** [alirezarezvani/claude-skills](https://github.com/alirezarezvani/claude-skills/tree/main/engineering/kubernetes-operator/skills/kubernetes-operator) (MIT). Local copy under `.cursor/skills/kubernetes-operator/`. **Removed locally:** `operator_capability_audit.py` (OperatorHub levels — not suited to this repo's V1 scope).

## When to use

- Implementing or reviewing Go controller / reconcile code under `src/`
- Auditing CRD YAML (`config/crd/`, `api/`)
- Choosing framework patterns (this project: **Go + kubebuilder** — [ADR-011](../../docs/02-technical-design/decision-records/architecture/011-implementation-language.md))
- Hardening RBAC, leader election, webhook validation
- Pre-merge operator audit on feature → `main` PRs

## When NOT to use

- Helm → CRD **design** pipeline → `@neo4j-operator-design-orchestrator`, BDR skills
- Neo4j **API vocabulary** (primary/secondary, plugins) → rule `neo4j-design-terminology`
- Plain kubectl / cluster ops without operator pattern

## Project cross-links

| Topic | Where |
|-------|--------|
| Package layering | [ADR-002](../../docs/02-technical-design/decision-records/architecture/002-package-layering.md), rule `operator-layering` |
| Reconcile pipeline order | [ADR-003](../../docs/02-technical-design/decision-records/architecture/003-neo4j-reconcile-pipeline.md) |
| Status & conditions | [ADR-004](../../docs/02-technical-design/decision-records/architecture/004-status-and-conditions.md) |
| Finalizers & deletion | [ADR-008](../../docs/02-technical-design/decision-records/architecture/008-finalizers-and-deletion.md) |
| Dev tests vs e2e matrix | [ADR-012](../../docs/02-technical-design/decision-records/architecture/012-testing-strategy.md) |

## Core principle: reconcile loop, not a script

```
observe(actual) → desired = read(spec) → diff(actual, desired) → act → update(status)
                                                                          ↓
                                                                   requeue / done
```

Operators fail when they: treat reconcile imperatively; skip requeue on transient errors; omit finalizers; mutate spec instead of status; skip status subresource; block in reconcile; omit leader election on multi-replica deploys.

## Quick start

```bash
SKILL=.cursor/skills/kubernetes-operator

# Full audit (CRD + reconcile)
"$SKILL/scripts/operator-audit.sh" .

# Individual tools
python3 "$SKILL/scripts/crd_validator.py" --crd config/crd/
python3 "$SKILL/scripts/reconcile_lint.py" --controller src/internal/controller/
```

## The 2 Python tools

All stdlib-only. Run with `--help`.

### `crd_validator.py`

```bash
python3 scripts/crd_validator.py --crd config/crd/myapp.yaml
python3 scripts/crd_validator.py --crd config/crd/ --format json
```

**Checks:** status subresource; storage/served versions; typed OpenAPI schema; conditions array; printer columns (Age + Status/Phase); Namespaced scope; singular/listKind.

### `reconcile_lint.py`

```bash
python3 scripts/reconcile_lint.py --controller src/internal/controller/neo4j_controller.go
```

**Checks:** `(ctrl.Result, error)` returns; requeue on error; no spec `Update` for status; no `time.Sleep`; context on HTTP calls; finalizer pairing; conditions usage; reconcile length ≤ 80 lines (WARN).

## Workflows

### Audit before merge (Gate 1 complement)

After [ADR-012](../../docs/02-technical-design/decision-records/architecture/012-testing-strategy.md) `go test ./src/...`:

```
1. scripts/operator-audit.sh .
2. Triage: FAIL → fix before merge; WARN → issue or fix in sprint
3. Run affected e2e scenarios in tests/ (Gate 2)
```

### Bootstrap new controller (Go + kubebuilder)

```
1. kubebuilder create api …
2. crd_validator.py on config/crd/bases/
3. Implement reconcile (see assets/reconcile_skeleton.go)
4. reconcile_lint.py on src/internal/controller/
5. Add status conditions per ADR-004
```

## References

- [references/operator_pattern.md](references/operator_pattern.md)
- [references/crd_design.md](references/crd_design.md)
- [references/reconcile_loop.md](references/reconcile_loop.md)
- [references/tooling_landscape.md](references/tooling_landscape.md)

## Asset templates

- [assets/crd_template.yaml](assets/crd_template.yaml)
- [assets/reconcile_skeleton.go](assets/reconcile_skeleton.go)

## Anti-patterns

- `time.Sleep` in reconcile → use `RequeueAfter`
- `r.Client.Update(ctx, obj)` for status → use `r.Status().Update(ctx, obj)`
- No leader election + 2+ replicas → split-brain
- No finalizer → orphan external resources
- CRD without status subresource → infinite reconcile loop
- Reconcile > 200 lines → extract domain steps per ADR-002/003
- `x-kubernetes-preserve-unknown-fields: true` on spec root
- Imperative reconcile ("if creating… if deleting…") → make actual = desired idempotently

## Verifiable success

- New CRDs pass `crd_validator.py` before merge
- Reconcile functions pass `reconcile_lint.py`
- No infinite reconcile loops in production
