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
  gcp/           GKE + Artifact Registry provisioning for e2e
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
  kind run, a green AKS run and a green GKE run cover the same behaviour. Anything platform-dependent belongs
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

`cluster-config-restart` (NEO-3-010-RSTR-02) is its cluster half, and adds the two properties a
single member cannot show. The roll must be **serial** — `readyReplicas` never falls below
`members - 1`, sampled every 2s rather than read off the spec, because the operator sets
`PodManagementPolicy: Parallel` on the pool StatefulSet (pods are created and deleted in parallel
when *scaling*) while leaving `updateStrategy` at the Kubernetes default `RollingUpdate`, which
still updates one pod at a time; a change that made updates parallel too would drop quorum without
any render test noticing. And the cluster must be **formed again** afterwards, since `Ready` speaks
only for member health.

`ClusterFormed` is not required to stay `True` during the roll, and asserting that would fail a
healthy one: with `minimumMembers=3` on a 3-primary pool, one member down puts `enabledPrimaries`
below the minimum and the operator honestly reports `WaitingQuorum`, or `BoltUnavailable` while it
dials the restarting member. What may never appear is one of the error-severity reasons — the
operator giving up instead of waiting. That set is read from the generated `lib/oracle.sh` rather
than written into the assert, so a refusal added to formation later is watched without anyone
remembering to. The reasons observed are carried into every failure message, where they are the
fastest explanation available.

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

## Plugins suite mechanics (`feature-plugins`)

Runtime plugin behaviour (BDR-004). Every render decision — the `NEO4J_PLUGINS` string, the
generated ConfigMap keys, the volume shapes — is already covered by unit tests under
`src/internal/render/`, so each case here asserts something only a real container can show.

**The operator does not download anything.** It sets `NEO4J_PLUGINS` on the container and the
official Neo4j image fetches the JARs at container start. Consequences: the node needs
outbound network, and a jar-host outage turns these cases red for reasons outside the
operator. `on_case_failure: continue` keeps that from hiding the rest of the suite.

**Standalone only.** Every catalog plugin is legal in `spec.plugins`, so one topology covers
apoc, gds and bloom. In Cluster mode GDS/Bloom are CEL-forbidden on primaries and on
`secondaries.read` — that placement rule is admission-level and unit-tested, not worth a
4-pod boot here. Staying on one topology also keeps `NEO4J_POOL` at its `server` default for
every case.

**Licences are dummies, and the mount is all that is checked.** The licence case asserts that
the operator wires the Secret onto the workload at `/licenses/<pluginID>` — not the file
contents, not the file mode, and not that the plugin accepts the licence. Content and
functional checks both need real licence material, which cannot live in CI.

Worth knowing when reading `render/workload/plugin_volumes.go`: it projects the **whole**
Secret (no `items`), so the file is named after the Secret key — `/licenses/bloom/license.key`
— whereas `crd-spec/neo4j/spec.md` documents `/licenses/gds.key`. The suite does not assert
that filename; the mismatch is tracked as a doc bug instead.

**Procedure assertions avoid version strings.** A missing function makes `cypher-shell` print
`Unknown function 'apoc.version'`, which contains `apoc.version` and would pass a naive
substring check. The cases use `SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc.'
RETURN count(*) > 0 AS ok` instead: core Cypher, so it returns `FALSE` rather than erroring
when the plugin is absent. `conn_assert_cypher` in `lib/connectivity.sh` is the retrying
wrapper; `conn_run_cypher` is its non-asserting sibling, used to log the actual version for
diagnostics.

Allowlists are checked over bolt rather than in the ConfigMap, because the Neo4j image ships
its own `neo4j.conf` — a rendered key is not proof the server resolved it.

**Two documented gaps** (see coverage.md): a volume-only plugin install emits
`server.directories.plugins` but *no* `dbms.security.procedures.*`, so a manually imported
JAR loads with its procedures still restricted; and an imported JAR must live at
`<pvc-root>/plugins/`, because `render/storage/volumes.go` applies the Share subPath to
`Existing` volumes too. The import case pins both so a fix has to be deliberate.

## Configuration profiles

| Profile | Behaviour |
|---------|-----------|
| Happy path (default, CI) | Fixed picks per layer |
| Matrix | All valid operator × neo4j combinations for the cloud and scenario |
| Explicit | You set `OPERATOR_CASE` and `NEO4J_CASE` |

See [config/readme.md](config/readme.md) for classic cases per domain.

## GitHub Actions

Two entry workflows, both driving the same composite action, plus one cleanup workflow per
managed cloud:

| Workflow | When | Runs |
|----------|------|------|
| [`ci.yml`](../.github/workflows/ci.yml) | Every PR / push to `main`, manual | `unit.yml`, one image build, then one job per suite on `local-kind` |
| [`e2e-all-platforms.yml`](../.github/workflows/e2e-all-platforms.yml) | 05:00 UTC daily, manual | `unit.yml`, then every suite on `local-kind`, `azure-aks`, `gcp-gke` and `aws-eks` in parallel |
| [`cloud-cleanup.yml`](../.github/workflows/cloud-cleanup.yml) | 09:00 UTC daily, manual | One job per cloud, deleting any managed cluster an e2e run left behind |

[`.github/actions/e2e`](../.github/actions/e2e/action.yml) holds the platform setup — kind
cluster, or a managed cluster created and an image pushed to its registry — and runs either one
suite (`suite` input) or all of them. Neither mode lists the suites: CI derives its matrix from `tests/suites/*.yaml` and the
action loops over the same glob, so adding a suite file is all it takes.

It is an action rather than a reusable workflow so that each CI check is a single name
(`CI / feature-config`); a called workflow would add a job level and the checks list truncates
the left half of `caller / callee`.

Cluster teardown lives in the calling job, not the action, with `if: always()` — inside a
composite, `always()` tracks the action's status rather than the job's, so a cancelled job would
skip it. Even there it does *not* fire on a force-cancel (which bypasses `always()` by design)
nor when a runner is lost, which is what the cleanup workflows cover — each skips itself while an
e2e run is in flight, unless dispatched with `force`.

The two clouds authenticate differently, on purpose. Azure uses a service principal secret, since
`azure/login` ignores `creds` as soon as `client-id` is passed; GCP uses workload identity
federation, so the job holds no key and nothing expires. Credentials and variables for both are
documented in [contribute.md](contribute.md).
