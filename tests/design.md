# E2E harness design

How the Gate 2 e2e harness is structured and how a suite runs. For what is tested see
[coverage.md](coverage.md); to run or add tests see [contribute.md](contribute.md).

End-to-end conformance tests run on a real Kubernetes cluster: operator install, Neo4j
deploy (Standalone or Cluster), operand assertions. Unit tests remain under `src/`
(`make test`) — this directory is **Gate 2** (ADR-012).

## Layout

```
tests/
  config/        e2e configuration (cloud, operator/neo4j cases)
  pipelines/     reusable setup/case/teardown phases
  suites/        table-driven tests (cases + pipeline refs)
  azure/         AKS + ACR provisioning for e2e
  bin/           entry points (run-e2e, setup-local-kind)
  actions/       atomic run.sh + verify.sh steps
  runner/        suite executor
  fixtures/      parameterised manifests
  results/       run diagnostics (gitignored)
```

## Suite / pipeline / action model

A **suite** is a table of cases plus pipeline overrides; a **pipeline** is the reusable
setup → case → teardown phase definition; an **action** is one atomic `run.sh` + `verify.sh`.

- Setup runs **once**, cases loop (run → assert → case_teardown), teardown runs **once**.
- `neo4j-suite` is the generic single-CR lifecycle (topology-agnostic): install operator,
  apply the CR fixture the case picks, clean it up. Suites override `case_assert` with the
  topology/feature-specific checks.
- Suites are **cloud-agnostic by default**: every suite runs on every platform, so a green
  kind run and a green AKS run cover the same behaviour. Anything platform-dependent belongs
  in the cloud profile (`tests/config/cloud/*.sh`) or in a `__CLOUD_*__` fixture placeholder,
  not in a suite that skips.
- `clouds: [...]` is therefore reserved for suites — or cases — whose *subject* is one platform,
  such as a cloud-authenticated PVC or a registry pull. Asking for one on another cloud logs
  `SKIP suite <name>` and exits 0, so both CI targets can request the whole catalogue and let
  the suite files decide. Omitting the key means every cloud.


## Assertions

Default (`E2E_ASSERT_NEO4J_READY=false`): verifies the operator is ready, the Neo4j CR is
applied, and StatefulSet, Services, Secret, and ConfigMap exist. Set
`E2E_ASSERT_NEO4J_READY=true` for full Neo4j pod readiness (requires Enterprise image pull).

## Config suite mechanics (`feature-config`)

`neo4j.conf` cases are runtime checks: the assert waits for the CR `Ready`, then connects
over bolt with `cypher-shell` from inside the `neo4j` container and runs `SHOW SETTINGS` to
read the *effective* value Neo4j resolved — the same check a user would do by hand. It asserts
containment (not equality) because Neo4j normalises some values (memory to bytes, lists as
`[..]`). `spec.config.neo4j` maps to a plain setting (`db.transaction.timeout`), and
`spec.config.jvm.additionalArguments` maps to `server.jvm.additional`.

The APOC case stays a render check (assert waits for `Installed` and reads the ConfigMap):
`apoc.*` keys land in the dedicated `<cr>-apoc-config` (key `apoc.conf`) only when APOC is
assigned via `spec.plugins`, and `SHOW SETTINGS` does not expose APOC config — runtime APOC
behaviour belongs to `feature-plugins`.

Three cases cover `jvm.useDefaults`, whose defaults come from `neo4jDefaultJVMAdditional` in
`src/internal/render/serverconfig/configmap.go` (the uncommented `server.jvm.additional` lines
of the vendored `neo4j-enterprise.conf`):

| Case | Fixture | Assertion |
|------|---------|-----------|
| `jvm-additional-args` | `neo4j-config-jvm.yaml` (`useDefaults: false`) | custom flag effective at runtime (NEO-3-003-JVM-02) **and** no Neo4j default in the ConfigMap |
| `jvm-use-defaults` | `neo4j-config-jvm-defaults.yaml` (`useDefaults: true`) | defaults rendered before the custom flag, and effective at runtime (NEO-3-003-JVM-01) |
| `jvm-override-defaults` | `neo4j-config-jvm-override.yaml` (`useDefaults: true`, colliding args) | the user value replaces the colliding default **in place** — one entry per key, position preserved, and the user value is the one the server reports |

The override case uses one collision per key shape recognised by `jvmArgKey`: a `-D` value
(`-Djdk.nio.maxCachedBufferSize`) and a `-XX` boolean flip (`OmitStackTraceInFastThrow`).
"In place" is asserted by position — the winner must still sit before the last default rather
than be appended at the end — because that ordering is what makes the precedence deterministic.

It also asserts that the drop is *reported*: a `DuplicateEntry` Warning Event (oracle reason)
and a `duplicate entry` operator log line, both naming the field, the value kept and the value
dropped. Without that, a user flag silently replacing a default would only be visible by
diffing the rendered ConfigMap.

`DuplicateEntry` is not JVM-specific — it is the reason for any spec field whose rendering
merges layers (`render.Duplicate`), `spec.config.neo4j` included. The assert therefore checks
the field name in the message, so a future source cannot make this case pass by accident.

Presence of the defaults is asserted on the rendered ConfigMap *and* at runtime, absence on the
ConfigMap only: the image ships its own `neo4j.conf`, so only the ConfigMap distinguishes what
the operator injected from what Neo4j would carry anyway.

The live config-change case (`config-restart`, NEO-2-010, formerly the separate
`feature-config-change` suite) deploys a plain Standalone, waits for `Ready`, patches
`spec.config.neo4j['db.transaction.timeout']`, then asserts three levels in order:
**render** (the ConfigMap fragment carries the new value), **rollout** (the StatefulSet
`updateRevision` changes → a controlled restart was triggered), and **runtime** (after the
pod comes back `Ready`, `SHOW SETTINGS` over bolt reports the new value). It is the only
config case that mutates a running CR, so it is heavier than the deploy-once cases.

`cluster-config-restart` (NEO-3-010-RSTR-02) is its cluster half, and asserts the property a
single member cannot show: the roll is **serial**. It patches the same setting on the
3-primary fixture and then *samples the live rollout* every 2s, requiring `readyReplicas`
never to fall below N-1 and `ClusterFormed` never to leave `True`, before checking the value
is effective on a restarted member.

Sampling rather than inspecting the spec is deliberate. The operator sets
`PodManagementPolicy: Parallel` on the pool StatefulSet — pods are created and deleted in
parallel when *scaling* — and leaves `updateStrategy` at its Kubernetes default,
`RollingUpdate`, which still updates one pod at a time. Those two settings pull in opposite
directions, and a change that made updates parallel too would drop quorum without any render
test noticing.

It is the only Cluster case in this suite and therefore runs **last**: `neo4j_case` sets
`NEO4J_POOL=primary`, and `_config_reset_case_vars` only clears that for cases that declare a
`neo4j_case`, so a bare-fixture case after it would derive the wrong StatefulSet name.

## Storage suite mechanics (`feature-storage`)

Covers the `spec.storage` surface, one case per feature (all admitted). Mount points are
verified from inside the `neo4j` container via `/proc/mounts` (no write permission required).

The three PVC-impossible cases use `assert/storage-error` and encode the accepted contract: a data
PVC that cannot bind keeps the CR **Pending**, with `StorageReady=False`, reason `PVCPending` and a
message naming the PVC and its `storageClassName` (`observePoolStorageReady` in
`src/internal/status/writer.go`). The CR must not be marked `Failed` and must not report `Ready`.
Waiting rather than failing is deliberate — nothing distinguishes a StorageClass that does not exist
from a provisioner that is slow, so the operator states the cause and lets the user fix it. Each case
polls for `STORAGE_ERROR_TIMEOUT` (default 120s) before giving up on seeing that condition.

| Case | Fixture | Assertion |
|------|---------|-----------|
| `dynamic-sc-ok` | `neo4j-storage-dynamic-sc.yaml` | data PVC Bound with `storageClassName=standard` |
| `dynamic-sc-fail` | `neo4j-storage-dynamic-sc-bad.yaml` | non-existent StorageClass → CR stays Pending, `StorageReady=False/PVCPending`, message contains `pvc` |
| `claimname-ok` | `neo4j-storage-claimname.yaml` | pod mounts a pre-created PVC via `existing.claimName` |
| `claimname-fail` | `neo4j-storage-claimname-missing.yaml` | missing PVC → CR stays Pending, `StorageReady=False/PVCPending`, message contains `pvc` |
| `vct-ok` | `neo4j-storage-vct.yaml` | `existing.volumeClaimTemplate` provisions `data-<cr>-server-0` |
| `vct-fail` | `neo4j-storage-vct-bad.yaml` | template with bad StorageClass → CR stays Pending, `StorageReady=False/PVCPending`, message contains `pvc` |
| `emptydir` | `neo4j-storage-emptydir.yaml` | inline `emptyDir` data volume mounted at `/data`, no PVC |
| `share-logs-metrics` | `neo4j-storage-share.yaml` | logs/metrics Share the data volume; `/logs` + `/metrics` mounted |
| `additional-mounts` | `neo4j-storage-additional.yaml` | `additionalMounts` (random name) mounted at its `mountPath` |

The additionalMounts volume name/path are generated per run by `deploy/neo4j` (random) and
read back by `assert/storage-additional`. The `claimname-ok` fixture bundles the PVC as a second
document; `storage/cleanup-extra` removes it (label `app.kubernetes.io/managed-by=neo4j-e2e`).

## Connectivity suite mechanics (`feature-connectivity`)

Boots a real Neo4j (`E2E_ASSERT_NEO4J_READY=true`) and probes each connector both from the
Neo4j pod itself (`localhost`) and from a separate client pod (client Service DNS):

| Protocol | Port | Probe | No-TLS expectation |
|----------|------|-------|--------------------|
| `bolt`   | 7687 | `cypher-shell bolt://`  | success |
| `neo4j`  | 7687 | `cypher-shell neo4j://` | success |
| `http`   | 7474 | raw HTTP over `/dev/tcp` | success |
| `https`  | 7473 | TCP connect | failure (connector not exposed without TLS) |

Expectations are data-driven via `EXPECT_CONN_{BOLT,NEO4J,HTTP,HTTPS}` (see
`config/neo4j/cases/standalone-connectivity.sh`); a TLS case flips `https` to `success`.

## Configuration profiles

| Profile | Behaviour |
|---------|-----------|
| Happy path (default, CI) | Fixed picks per layer |
| Matrix | All valid operator × neo4j combinations for the cloud and scenario |
| Explicit | You set `OPERATOR_CASE` and `NEO4J_CASE` |

See [config/readme.md](config/readme.md) for classic cases per domain.

## GitHub Actions

Two entry workflows, both driving the same composite action:

| Workflow | When | Runs |
|----------|------|------|
| [`ci.yml`](../.github/workflows/ci.yml) | Every PR / push to `main`, manual | `unit.yml`, one image build, then one job per suite on `local-kind` |
| [`e2e-all-platforms.yml`](../.github/workflows/e2e-all-platforms.yml) | 05:00 UTC daily, manual | `unit.yml`, then every suite on `local-kind` and on `azure-aks` in parallel |
| [`azure-cleanup.yml`](../.github/workflows/azure-cleanup.yml) | 09:00 UTC daily, manual | Deletes the Azure CI resource group if an e2e run left it behind |

[`.github/actions/e2e`](../.github/actions/e2e/action.yml) holds the platform setup — kind
cluster, or AKS create plus image push — and runs either one suite (`suite` input) or all of
them. Neither mode lists the suites: CI derives its matrix from `tests/suites/*.yaml` and the
action loops over the same glob, so adding a suite file is all it takes.

It is an action rather than a reusable workflow so that each CI check is a single name
(`CI / feature-config`); a called workflow would add a job level and the checks list truncates
the left half of `caller / callee`.

Azure teardown lives in the calling job, not the action, with `if: always()` — inside a
composite, `always()` tracks the action's status rather than the job's, so a cancelled job would
skip it. Even there it does *not* fire on a force-cancel (which bypasses `always()` by design)
nor when a runner is lost, which is what `azure-cleanup.yml` covers — it skips itself while an
e2e run is in flight, unless dispatched with `force`.

Azure CI credentials and variables are documented in [contribute.md](contribute.md).
