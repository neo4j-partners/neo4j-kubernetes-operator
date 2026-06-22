# AGENTS.md

The front door for every contributor — human or LLM. Read this first. It states the project invariants in **canonical short form** and routes you to everything else; the detailed, enforcement-tagged version (with recovery steps) is [`docs/knowledge/invariants.md`](docs/knowledge/invariants.md). When in doubt on a hard constraint, the list here is canonical and `invariants.md` is its authoritative detail.

## Project identity

Neo4j Enterprise Operator for Kubernetes — manages Neo4j Enterprise (5.26.x LTS + 2025.x/2026.x CalVer) via the Kubebuilder framework. Go 1.26, `sigs.k8s.io/controller-runtime` v0.24.1, `k8s.io/*` v0.36.1, `neo4j-go-driver/v5` (Bolt). Two deployment CRDs: `Neo4jEnterpriseCluster` (HA, min 2 servers) and `Neo4jEnterpriseStandalone` (single-node).

## The 5 hard invariants — NEVER violate

These are non-negotiable. They are checked by `make check-invariants` (run by the agent skills and an **advisory, non-blocking** CI job) and — where `docs/knowledge/invariants.md` marks them so — by **blocking** unit tests and by **runtime** validators that reject a bad CR. Do not "fix" a guard by relaxing it; fix the change.

1. **NO admission webhooks.** No `ValidatingWebhookConfiguration` / `MutatingWebhookConfiguration`, no `*_webhook.go`. ALL validation lives in `internal/validation/` and is called inline from the reconcilers.
2. **KIND ONLY** for dev/test/CI. No minikube, k3s, or any other distribution.
3. **ENTERPRISE IMAGES ONLY** — `neo4j:<version>-enterprise` (e.g. `neo4j:5.26-enterprise`, `neo4j:2025.01.0-enterprise`). Never community images.
4. **V2_ONLY discovery** exclusively. Port 6000 (V2). Never V1 (port 5000) or K8s discovery.
5. **Server-based architecture.** A single `{cluster}-server` StatefulSet with `replicas: N`; pods are `{cluster}-server-0…N-1`. NEVER `primary-*` / `secondary-*` pod names. Backups are **Job-per-`Neo4jBackup`-CR ONLY** — no centralized `{cluster}-backup` StatefulSet, no `spec.backups` field, no `BuildBackupStatefulSet`, no standalone backup sidecar. These were removed; never reintroduce a long-running backup pod.

## Repository map

| Path | What lives here |
|---|---|
| `api/v1beta1/` | CRD Go types + kubebuilder markers (source for generated CRDs & deepcopy). |
| `internal/controller/` | Reconcilers (cluster, standalone, database, plugin, backup, restore, user/role/binding, authrule, sharded), split-brain detector, events. |
| `internal/validation/` | Inline validators (one per concern: cluster, database, image, memory, plugin, tls, topology, backup, …). The ONLY validation layer — see invariant 1. |
| `internal/resources/` | Kubernetes object builders (StatefulSet via `BuildServerStatefulSetForEnterprise`, Services, ConfigMaps, NetworkPolicy, TLS/discovery helpers). |
| `internal/neo4j/` | Bolt client + Cypher helpers + version parsing (`ParseVersion`, `IsCalver`). |
| `test/` | Ginkgo/Gomega suites in tiers: `unit`, `integration`, `e2e`. Every integration spec carries `Label("core")` or `Label("extended")`. |
| `config/` + `charts/` | GENERATED manifests (CRDs, RBAC, kustomize, Helm chart, OperatorHub bundle). Never hand-edit files carrying the GENERATED header. |
| `cmd/main.go` | Manager entrypoint — wires controllers (no webhooks). |

## Working principles (how to make changes here)

Behavioral house rules — they apply to *every* change, on top of the invariants
above. Adapted for this repo from the MIT-licensed "Karpathy guidelines"
(github.com/multica-ai/andrej-karpathy-skills). Each maps to a scar this project
actually has:

1. **Think before coding.** Surface assumptions and tradeoffs; don't pick
   silently. When the request is ambiguous or a doc disagrees with the code,
   *say so and ask* — see invariant 1's "the code wins" rule. (We chose to
   reject only `-community` image tags, not require `-enterprise`, precisely by
   surfacing that tradeoff rather than guessing.)
2. **Simplicity first.** The minimum code that solves the problem — nothing
   speculative. No unrequested fields, abstractions, or flexibility. This *is*
   the separation-of-concerns invariant (e.g. `Neo4jDatabase` must not override
   cluster-level settings) and the "never reintroduce removed plumbing" family.
3. **Surgical changes.** Touch only what the task requires; preserve adjacent
   code and style; remove only what *your* change orphaned. This is why the
   env-var path is a subset-merge (`envVarsEqual` / `mergeEnvVars`), never a
   wholesale replace — foreign keys survive.
4. **Goal-driven execution.** Turn the task into verifiable success criteria and
   loop until they pass. Unit tests prove code against itself; **live-verify**
   in a real DB / Kind cluster (`verify-journey`) and run the guards
   (`make check-knowledge`) before declaring done.

## Ground Neo4j behavior in the version-correct manual

Your training memory skews to deprecated Neo4j **4.x**. This operator supports
**only 5.26.x LTS and CalVer (2025.x/2026.x)** — no 4.x, no 5.27+ semver. When you
build or verify ANY Neo4j-specific behavior — config settings, Cypher, procedures,
topology, property sharding, end-to-end processes — read the official operations
manual for the **target version**, not memory:
- **CalVer** (2025.x / 2026.x): https://neo4j.com/docs/operations-manual/current/
- **LTS** (5.26.x): https://neo4j.com/docs/operations-manual/5/

This is the external counterpart to "grep before you cite," and it has repeatedly
caught 4.x ghosts (`dbms.mode=SINGLE`, `causal_clustering.*`,
`CALL dbms.cluster.role`, `CREATE DATABASE … OPTIONS {primaries}`) that compile and
pass unit tests but fail against a live modern database. Procedure + the full
4.x→modern mapping: the `check-neo4j-docs` skill.

## Before you commit

1. `make fmt && make lint && make test-unit` — formatting, lint, and unit tests (no cluster needed).
2. Touched `api/v1beta1/*` types or `+kubebuilder:` markers? Run `make manifests && make generate` to regenerate CRDs/RBAC/deepcopy, then `make sync-all`. **Never hand-edit generated files.**
3. `make check-drift` — the CI gate: regenerates everything and fails if anything is stale. Run it before pushing.
4. `pre-commit` runs on commit (config in `.pre-commit-config.yaml`); install with `pre-commit install`.
5. Integration work: KIND only — `make dev-up` / `make test-integration`. Never `make dev-run` (in-cluster only; out-of-cluster DNS fails).

## Pointers

- **Invariants & regression rules** (the full enforcement-tagged checklist): `docs/knowledge/`.
- **Procedures** (invokable skills): `.claude/skills/` — catalog + when-to-use in [`.claude/skills/README.md`](.claude/skills/README.md). Includes `check-neo4j-docs` (version-correct grounding), `add-crd-field` / `add-controller` / `add-inline-validator` / `regen-artifacts` (building features), `verify-journey` / `fix-knowledge-drift` / `run-extended-suite` (verify), `add-regression-rule` / `issue-hygiene` (process), `cut-release` / `retract-release` (release).
- **Detailed domain reference** (deep architecture, Cypher syntax, plugin/TLS/backup specifics): `CLAUDE.md`.
- **Contributor workflow**: `CONTRIBUTING.md` and `docs/developer_guide/` — start with `docs/developer_guide/llm-contribution.md` (the agent on-ramp) and `docs/developer_guide/QUICKSTART.md` (Day-1 dev setup).

AGENTS.md is the front door; CLAUDE.md is the deep reference. If they ever conflict, AGENTS.md's invariants win — and fix the drift.
