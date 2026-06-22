# Hard Invariants

> **This is a load-bearing file.** These five invariants are the project's
> constitution-level constraints. Violating any of them produces a broken or
> banned architecture. `AGENTS.md` carries the canonical short list (the front
> door); **this file is the authoritative detailed source** — rule, why,
> enforcement status, violation symptom, and **recovery** for each.
>
> **Enforcement-status vocabulary:**
> - **guard-checked (`scripts/check-invariants.sh`)** — the guard script greps the
>   tree and flags a violation. Run three ways: `make check-invariants` locally,
>   the agent skills under `.claude/skills/`, and an **advisory, non-blocking** CI
>   job (`Invariant Guards` in `ci.yml`). It surfaces violations; it does **not**
>   gate merge — so a contributor who isn't using an agent is never blocked by it.
> - **test-pinned (`<test>`)** — a Go test asserts the behavior; `make test-unit`
>   fails when violated. This **is** a blocking CI gate (the `unit-tests` job).
> - **runtime-enforced** — a validator in `internal/validation/` rejects the bad CR
>   inline during reconcile (a `Failed`/`Invalid` status), and/or the running
>   operator refuses at boot. The CR never takes effect.
> - **PROSE-ONLY — at risk** — enforced only by convention and the *absence* of
>   the forbidden construct; nothing actively rejects a violation.
>
> **Honesty note:** the `scripts/check-invariants.sh` guard now **exists** and
> covers INV-1, INV-2, INV-4, and INV-5 (grep) plus a static config/sample check
> for INV-3. It is **advisory** (non-blocking CI + skills + `make check-invariants`),
> NOT a required merge gate — the blocking CI gates are `check-drift` (generated
> artifacts) and `unit-tests`. INV-3 additionally gained a **runtime** check
> (`image_validator.go` rejects `-community` tags, pinned by
> `image_validator_test.go` under the blocking `unit-tests` job) plus the
> `CALL dbms.components()` backstop. INV-4 and the INV-5 pod-naming half are
> **test-pinned** (blocking). **Highest residual risk:** a bare Docker Hub tag
> (`neo4j:5.26.0`, which *is* the community image) is not rejected statically —
> only the explicit `-community` marker is — so that case relies on the runtime
> edition backstop, by deliberate choice (see INV-3).

---

## Recovery — how to fix a violation

Quick reference (folded in from the former AGENT-GUARDRAILS table); per-invariant detail follows below.

| Inv | Recovery |
|---|---|
| **INV-1** | Move the check into an `internal/validation/*_validator.go` and call it inline from the reconciler. Delete any `*_webhook.go` / webhook config. |
| **INV-2** | Use `make dev-up` / `make test-integration`. Remove references to other local-K8s tools. |
| **INV-3** | Pin an `-enterprise` tag. Don't weaken the validators to admit community. |
| **INV-4** | Keep the version-gated discovery config in the cluster builder (`buildVersionSpecificDiscoveryConfig`); don't hand-roll discovery flags elsewhere. |
| **INV-5** | Use the existing `{cluster}-server` builder in `internal/resources/cluster.go` and the `Neo4jBackup`/`Neo4jRestore` controllers. Never add a long-running backup pod/sidecar. |

---

## INV-1: NO-WEBHOOKS

- **id:** `NO-WEBHOOKS`
- **rule:** The operator MUST NOT use admission webhooks. No
  `ValidatingWebhookConfiguration` or `MutatingWebhookConfiguration` manifests,
  no `*_webhook.go` files, no webhook server wiring in `cmd/main.go`. **All**
  validation lives in `internal/validation/` (e.g. `cluster_validator.go`,
  `image_validator.go`, `plugin_validator.go`, …) and is called **inline** from
  the reconcilers.
- **WHY:** Webhooks add a TLS-served admission endpoint that must be reachable by
  the API server, fail closed on cert/network problems, and create a
  bootstrapping dependency the project deliberately avoids. Controller-side
  validation runs in the reconcile loop where the operator already has full
  context, degrades gracefully (a bad CR goes to a `Failed`/`Invalid` status
  instead of blocking the whole API group), and needs no cert plumbing. This was
  a foundational design decision, not an accident.
- **enforcement status:** guard-checked (`scripts/check-invariants.sh`) — greps
  for `*_webhook.go` / `config/webhook/` paths and
  `Validating`/`MutatingWebhookConfiguration` references in non-test Go.
  Advisory (non-blocking CI + agent skills + `make check-invariants`), not a
  merge gate. No test or runtime check rejects a webhook, so the guard is the
  only automated signal — if it is bypassed nothing else complains until the
  webhook plumbing breaks at install time.
- **violation symptom:** Kubebuilder's `create webhook` scaffolding, or hand-added
  webhook code, compiles and passes all current tests — there is no failing
  signal. The damage shows up later: cert-manager/CA dependency at install time,
  CRs silently rejected at admission when the webhook pod is down, and a
  `config/webhook/` directory that `make manifests` starts regenerating. An agent
  "adding validation" via a webhook will think it succeeded.

---

## INV-2: KIND-ONLY

- **id:** `KIND-ONLY`
- **rule:** Kind is the **only** supported Kubernetes distribution for
  development, test, and CI. Never minikube, k3s, k3d, Docker Desktop K8s, or a
  real cloud cluster in any dev/test/CI path. Clusters: dev =
  `neo4j-operator-dev`, test = `neo4j-operator-test`. Use the `make` targets
  (`dev-up`, `test-integration`, `operator-setup`); never hand-roll a different
  provisioner.
- **WHY:** Every Makefile target, the CI workflows, the resource-shrinking logic
  (`getCIAppropriateResourceRequirements()` / `applyCIOptimizations()`), and the
  cert-manager `ca-cluster-issuer` setup assume Kind's networking, image-load
  semantics (`kind load`), and node layout. Another distro silently changes pod
  DNS, storage classes, and load-balancer behavior, producing flaky failures that
  look like operator bugs but are environment drift.
- **enforcement status:** guard-checked (`scripts/check-invariants.sh`) — greps
  the Makefile, `hack/`, `scripts/`, and `.github/workflows/` (the operational
  paths that provision clusters) for `minikube`/`k3s`/`k3d`. Advisory
  (non-blocking). Prose mentions in docs/CONTRIBUTING and per-distro
  storageClass hints in `examples/` are intentionally not scanned — they are not
  provisioners.
- **violation symptom:** An agent adds a `minikube start` / `k3d cluster create`
  path to a script or workflow and it "works on my machine" but the operator
  can't load its image, discovery RBAC behaves differently, or integration tests
  time out on image pulls. No existing test or script complains — the new path
  just exists alongside the Kind path and rots.

---

## INV-3: ENTERPRISE-IMAGES

- **id:** `ENTERPRISE-IMAGES`
- **rule:** Only Neo4j **Enterprise** images may be used:
  `neo4j:5.26-enterprise`, `neo4j:2025.01.0-enterprise`, etc. Never a community
  image. The operator assumes Enterprise features (clustering, RBAC, online
  backup) unconditionally; there is no `edition` field in any CRD.
- **WHY:** The operator emits Enterprise-only Cypher and config (clustering
  topology, `SHOW SERVERS`, role/privilege management, `neo4j-admin database
  backup`). Against a community image these silently fail or the pod won't form a
  cluster at all. The product is an *Enterprise* operator by definition.
- **enforcement status:** runtime-enforced + guard-checked.
  `internal/validation/image_validator.go` rejects any image whose tag
  explicitly marks it community (`isCommunityTag` — matches `community`
  case-insensitively, e.g. `5.26.0-community`), returning a clean
  `spec.image.tag` validation error; pinned by `image_validator_test.go` under
  the blocking `unit-tests` job. `scripts/check-invariants.sh` adds a static
  check that no CRD/manifest in `config/`+`api/` pins a `…-community` image
  (advisory). The running operator's `CALL dbms.components()` check remains the
  backstop. The numeric **version** is still validated via `neo4j.ParseVersion`
  / `version.IsSupported()` (5.26.x or 2025.x+). `edition_validator.go` remains
  an intentional no-op (the edition field was removed). **Known gap (by
  design):** the validator rejects only the explicit `-community` marker — it
  does NOT require an `-enterprise` suffix — so a bare `neo4j:5.26.0` (the
  *community* image on Docker Hub) passes static/inline validation and is caught
  only by the runtime edition backstop. This avoids false-rejecting legitimately
  retagged Enterprise images in a private registry.
- **violation symptom:** A CR with `image: { repo: neo4j, tag: 5.26-community }`
  is now rejected up front by `image_validator.go` with a clear `spec.image.tag`
  error. The remaining trap is a *bare* tag — `image: { repo: neo4j, tag: 5.26.0 }`
  — which on Docker Hub is the community image yet passes inline validation; it
  is caught only later when the running operator's `CALL dbms.components()`
  reports `community`, surfacing as a degraded status rather than a clean
  up-front rejection.

---

## INV-4: V2_ONLY-DISCOVERY

- **id:** `V2_ONLY-DISCOVERY`
- **rule:** Cluster discovery uses the V2 protocol exclusively (LIST resolver,
  static pod FQDNs, port **6000**). For SemVer 5.26.x the operator MUST emit
  `dbms.cluster.discovery.version=V2_ONLY` explicitly; for CalVer 2025.x+
  (including 2026.x+) the flag is omitted because V2 is the only protocol — the
  CalVer key is `dbms.cluster.endpoints` instead of `dbms.cluster.discovery.v2.endpoints`.
  Never use the deprecated V1 protocol (port 5000) or any 4.x
  `causal_clustering.*` discovery config.
- **WHY:** V1 discovery is deprecated and removed in the supported versions; the
  static-FQDN LIST resolver is the only reliable mechanism in Kind where pod IPs
  are ephemeral. Emitting `V2_ONLY` on 5.26 but *omitting* it on CalVer is
  load-bearing: CalVer rejects the flag, and 5.26 won't reliably pick V2 without
  it.
- **enforcement status:** test-pinned (`TestListDiscoveryConfiguration` in
  `internal/resources/cluster_startup_test.go`; blocking `unit-tests` job) and
  additionally guard-checked — `scripts/check-invariants.sh` greps non-test Go
  for an emitted `discovery.version=V1`/`V1_ONLY` (advisory). The
  version-specific block is built by `buildVersionSpecificDiscoveryConfig()`
  (`internal/resources/cluster.go`), which branches on `isCalverImage(tag)`; the
  test asserts 5.26 emits `dbms.cluster.discovery.version=V2_ONLY` and CalVer
  does not.
- **violation symptom:** Changing the discovery block so 5.26 drops the `V2_ONLY`
  flag, or so CalVer adds it, fails `TestListDiscoveryConfiguration` under
  `make test-unit`. If the test were bypassed, the live symptom is clusters that
  never form — pods log discovery errors and `SHOW SERVERS` shows isolated
  single-member views (see split-brain detection).

---

## INV-5: SERVER-ARCH

- **id:** `SERVER-ARCH`
- **rule:** A cluster is a **single** StatefulSet named `{cluster}-server` with
  `replicas: N`; pods are `{cluster}-server-0 … {cluster}-server-N-1`. Servers
  self-organize into primary/secondary roles — **never** name pods or
  StatefulSets `primary-*` / `secondary-*`. Backups are **Job-per-`Neo4jBackup`-CR
  exclusively**: there is NO centralized `{cluster}-backup` StatefulSet, NO
  long-running backup pod or sidecar, NO `spec.backups` field, and NO
  `BuildBackupStatefulSet` / `BackupsSpec` / `buildBackupSidecarContainer`
  symbols (CLAUDE.md rule 79 — all REMOVED, never reintroduce).
- **WHY:** The server-based model lets Neo4j's own cluster machinery assign
  primary/secondary roles dynamically; pinning roles to pod names re-creates the
  banned 4.x `core/read-replica` topology and breaks server
  mode-constraint hints. The centralized backup StatefulSet was a long-running
  pod that held a lease on backup storage and a sidecar that bloated every Neo4j
  pod; both were deliberately deleted in favor of ephemeral Jobs that run, write
  one artifact, and exit. Reintroducing either resurrects retired, conflicting
  code paths.
- **enforcement status:** test-pinned for the naming half
  (`TestBuildStatefulSetForEnterprise_WithFeatures` in
  `internal/resources/cluster_test.go` asserts the StatefulSet name is
  `{cluster}-server-0` and pod FQDNs follow `{cluster}-server-N`;
  `TestListDiscoveryConfiguration` exercises the same naming via discovery
  endpoints — both under the blocking `unit-tests` job) AND guard-checked for
  the removed-backup half — `scripts/check-invariants.sh` now greps for
  `BuildBackupStatefulSet`, a `backups:`/`spec.backups` field in the CRD surface
  (`config/`+`api/`), and `-primary-`/`-secondary-` pod-name construction in
  `internal/resources/` (advisory). (Previously the removed-backup symbols were
  kept gone by file/symbol absence only.)
- **violation symptom:** Renaming the StatefulSet or introducing `primary-*` /
  `secondary-*` pods fails the naming assertions in
  `TestBuildStatefulSetForEnterprise_WithFeatures` under `make test-unit`.
  Re-adding a `spec.backups` field or a `BuildBackupStatefulSet` builder,
  however, **compiles and passes all current tests** — the live symptom is a
  resurrected long-running backup pod competing with the Neo4jBackup Job path for
  the same storage, and `make manifests` regenerating CRD fields that were
  intentionally deleted.
