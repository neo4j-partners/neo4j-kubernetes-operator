# Agent Guardrails

Hard invariants for this repository. They exist because LLM-assisted changes
(and well-meaning humans) repeatedly try to reintroduce architectures Neo4j
explicitly does not support. **Violating any of these is never "fixing a bug" —
it is breaking the product contract.** The single source of truth for the
invariants is [`AGENTS.md`](../../AGENTS.md); this page adds the enforcement and
recovery view.

If you are an LLM agent: treat the "Enforced by" column as ground truth, but
know the two tiers. The **blocking** gates — `make check-drift` (generated
artifacts) and the `unit-tests` job — reject a violating change mechanically;
there is no arguing those past review. The **advisory** invariant guard
(`make check-invariants`, run by the agent skills and a non-blocking CI job)
flags a violation without gating merge — but a flag there means you have broken
the product contract, not tripped a lint nit. Fix the change, never the guard.

## Invariants

| Guardrail | What happens if violated | Enforced by | Recovery |
|---|---|---|---|
| **1. No admission webhooks.** No `ValidatingWebhookConfiguration` / `MutatingWebhookConfiguration`, no `*_webhook.go`. All validation is inline in `internal/validation/`, called from the reconcilers. | Reintroduces an architecture the project deliberately removed; webhook plumbing (cert rotation, failure policy, API-server reachability) becomes a new failure surface the operator was designed to avoid. | **Guard script** (advisory) `scripts/check-invariants.sh` — greps for `*_webhook.go` / `config/webhook/` and `Validating`/`MutatingWebhookConfiguration` in non-test Go. Run by `make check-invariants`, the agent skills, and a non-blocking CI job; plus code review. The validator packages under `internal/validation/` are the sanctioned home. | Move the check into an `internal/validation/*_validator.go` and call it inline from the reconciler. Delete any `*_webhook.go` and webhook config. |
| **2. Kind only for dev/test/CI.** No minikube, k3s, Docker-Desktop K8s, etc. | CI and every `make dev-*` / `make test-*` target assume Kind cluster names (`neo4j-operator-dev`, `neo4j-operator-test`) and a Kind image loader; other runtimes silently break image loading and cluster discovery. | **Guard script** (advisory) — greps the Makefile, `hack/`, `scripts/`, `.github/workflows/` for `minikube`/`k3s`/`k3d`; plus Makefile/CI hard-coding Kind cluster names. | Use `make dev-up` / `make test-cluster`. Remove references to other local-K8s tools. |
| **3. Enterprise images only.** `neo4j:<ver>-enterprise` (5.26.x or 2025.x+). Never community. | Cluster features (multi-database, clustering, security) don't exist on community; the operator emits Enterprise-only config and Cypher that fails. | **Runtime** — `internal/validation/image_validator.go` rejects `-community` tags inline (pinned by `image_validator_test.go`); plus an advisory static guard that no `config/`/`api/` sample pins a `…-community` image, and the `CALL dbms.components()` backstop. A *bare* tag (community on Docker Hub) is caught only by the backstop, by design. `edition_validator.go` is a no-op (edition field removed). | Pin an `-enterprise` tag. Don't weaken the validators to admit community. |
| **4. V2_ONLY discovery.** SemVer 5.26.x sets `dbms.cluster.discovery.version=V2_ONLY` and uses the v2 endpoints key; CalVer uses V2 implicitly. Port 6000, never legacy V1 (5000). | Cluster formation fails or silently uses the deprecated V1 path; pods can't find each other. | Code: startup-script builders in `internal/resources/cluster.go`; pinned by `internal/resources/cluster_startup_test.go`. | Keep the version-gated discovery config in the cluster builder; don't hand-roll discovery flags elsewhere. |
| **5. Server-based architecture, Job-per-CR backups.** One `{cluster}-server` StatefulSet with `replicas:N`; pods `{cluster}-server-0..N-1` — **never** `primary-*`/`secondary-*` names. Backups are a Job per `Neo4jBackup` CR only. **No** centralized `{cluster}-backup` StatefulSet, **no** `spec.backups` field / `BackupsSpec` type, **no** `BuildBackupStatefulSet`, **no** standalone backup sidecar (all removed — see CLAUDE.md rule 79). | Reintroduces removed, unsupported plumbing; breaks the single-StatefulSet topology the whole reconciler and DIFF-chaining backup model assume. | **test-pinned** naming (`TestBuildStatefulSetForEnterprise_WithFeatures`, blocking) + **guard script** (advisory) greps for `-primary-`/`-secondary-` pod naming, `BuildBackupStatefulSet`, and `backups:`/`spec.backups` in the CRD surface; + code review. | Use the existing `{cluster}-server` builder in `internal/resources/cluster.go` and the `Neo4jBackup`/`Neo4jRestore` controllers. Never add a long-running backup pod/sidecar. |

> The `scripts/check-invariants.sh` guard is the machine check for the grep-able
> invariants — INV-1 (webhooks), INV-2 (non-Kind provisioners), INV-4 (V1
> discovery), and INV-5 (backup StatefulSet / `spec.backups` / primary-secondary
> naming) — plus a static INV-3 check (no community-tagged image in
> `config/`+`api/`). It runs as `make check-invariants`, inside the agent skills,
> and as an **advisory, non-blocking** CI job (`Invariant Guards`) — it surfaces
> violations but does **not** gate merge, so non-agent contributors are never
> blocked by it. The hard merge gates are `check-drift` and `unit-tests`. INV-3's
> real teeth are at **runtime** (`image_validator.go` rejects `-community`,
> pinned by a unit test) and INV-4 / INV-5-naming are **test-pinned** (blocking).
> If the guard flags your change, change the approach, not the script.

## Generic gates (apply to every change)

| Gate | What happens if violated | Enforced by | Recovery |
|---|---|---|---|
| **Generated-artifact drift gate.** Any edit to `api/v1beta1/*_types.go`, a `+kubebuilder:rbac:` marker, or `config/crd/bases/*.yaml` must be followed by regeneration (CRDs, RBAC, kustomize lists, Helm chart, OperatorHub bundle). | PR is blocked; CRDs/RBAC/chart/bundle ship out of sync, producing a broken install. | **CI gate** — `make check-drift` (`sync-all` + `bundle` + `git diff --exit-code`), run by the `Generated Artifacts In Sync` job in `.github/workflows/ci.yml`. Same check available locally as a pre-commit hook via `make install-hooks`. | Run `make sync-all` (or `make ship-prep` before a release) and commit the regenerated files. Adding a new CRD also needs a description row in `scripts/helm-sync-artifacthub-crds.sh`. |
| **Lint.** `golangci-lint` must be clean. | Style/lint regressions land unnoticed. | **Local only** — `make lint`. **NOTE: lint is *not* run in CI** (only `check-drift` and `test-unit`, which itself runs `fmt` + `vet`). Run `make lint` yourself before pushing. | Fix the reported issues; re-run `make lint`. |

## Why "DO NOT trust a stale guide" matters here

This guardrails page exists because a prior `AGENTS.md` drifted into instructing
agents to *preserve* a centralized backup StatefulSet and *wire webhooks* — both
banned and long since removed from the code. **When a doc and the code
disagree, the code wins.** Before citing any symbol (file, function, test,
field) in a change or a doc, grep the tree to confirm it still exists. The five
invariants above are verified against the current `main`. Two guards keep the
docs from silently rotting again: `make check-invariants` (the constructs above)
and `make check-knowledge-drift` (every test/file citation in `docs/knowledge/`
must resolve) — and the `fix-knowledge-drift` skill repairs the latter when a
rename breaks a citation.
