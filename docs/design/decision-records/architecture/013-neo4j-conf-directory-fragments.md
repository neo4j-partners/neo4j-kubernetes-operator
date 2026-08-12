# ADR-013 — `neo4j.conf` as a directory of ConfigMap fragments

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-08-04 |
| **Depends on** | [BDR-008](../business/neo4j/008-neo4j-config-surface.md) — config surface · [ADR-005](005-render-conventions.md) — render conventions · [ADR-006](006-apply-and-idempotency.md) — apply & idempotency |
| **Constraints** | Neo4j image `EXTENDED_CONF` / `NEO4J_CONF`; Kubernetes multi-key ConfigMap mount semantics |

---

## Context

The operator projects Neo4j server configuration into the workload pod. On a running
pod, `neo4j.conf` is **not a file** — it is a *directory* `/config/neo4j.conf/` containing
one file per setting. This surprises operators inspecting the container, and raised the
question of whether it is an accident of the ConfigMap mount or a deliberate choice.

It is deliberate. Understanding why matters because the layout is coupled to how the
operator **updates** configuration and **reconciles** it from the CR ([ADR-006](006-apply-and-idempotency.md)).

**Forces:**

- [BDR-008](../business/neo4j/008-neo4j-config-surface.md) — `spec.config` is a passthrough map; the operator also injects owned keys
  (listeners, discovery, JVM). Owned keys and user keys must merge predictably.
- The Neo4j Helm chart renders config as **one key per setting** and relies on the image's
  `EXTENDED_CONF` mechanism to assemble fragments — V1 targets Helm parity for migration.
- Kubernetes mounts a **multi-key** ConfigMap at a `mountPath` as a **directory** with one
  file per data key. Mounting at `/config/neo4j.conf` therefore yields a directory.
- [ADR-006](006-apply-and-idempotency.md) requires idempotent, low-blast-radius updates: a single-setting change should
  produce a minimal diff, not rewrite an opaque blob.
- Other config surfaces coexist under `/config/` (`apoc.conf`, `server-logs.xml`,
  `user-logs.xml`) and must not collide.

---

## Analysis

### Option A — Directory of per-setting fragments (chosen)

`ConfigMap.Data` holds **one key per `neo4j.conf` setting**; the ConfigMap is projected at
`/config/neo4j.conf`, so Kubernetes materialises a directory of fragment files. The image
reads `NEO4J_CONF=/config/` with `EXTENDED_CONF=yes` and assembles the fragments at startup.

```go
// render/serverconfig/configmap.go — one key per setting
Data: map[string]string{
    "server.bolt.listen_address": ":7687",
    "server.memory.heap.max_size": "2G",
    "server.jvm.additional":       "-XX:+ExitOnOutOfMemoryError",
    // ...
}
```

```go
// render/workload/statefulset.go
VolumeMounts: []corev1.VolumeMount{{Name: "neo4j-conf", MountPath: "/config/neo4j.conf"}}
Env:          {"NEO4J_CONF": "/config/", "EXTENDED_CONF": "yes"}
```

| Advantages | Disadvantages |
|------------|---------------|
| Per-setting diff/patch → minimal, auditable updates from the CR | `neo4j.conf` appears as a directory (surprising on first inspection) |
| Clean merge of operator-owned keys vs `spec.config` per [ADR-006](006-apply-and-idempotency.md) | Requires the image `EXTENDED_CONF` contract |
| Composable with `apoc.conf` / logging under `/config/` | Not a single human-readable file in the pod |
| Direct Helm parity — same shape, same image mechanism | |

### Option B — Single rendered `neo4j.conf` file

Render the whole file into **one** ConfigMap key `neo4j.conf` and mount it with `subPath`
so it lands as a real file `/config/neo4j.conf`.

| Advantages | Disadvantages |
|------------|---------------|
| Intuitive: a real, readable file | Any single-key change rewrites the whole blob → coarse diff |
| No `EXTENDED_CONF` dependency | `subPath` mounts do **not** live-update on ConfigMap change |
| | Merging owned keys + `spec.config` + apoc/logging becomes manual string assembly |
| | Diverges from Helm chart shape (migration friction) |

### Option C — Config via `NEO4J_*` environment variables only

Translate settings to the image's `NEO4J_<dotted_to_underscore>` env convention.

| Advantages | Disadvantages |
|------------|---------------|
| No config volume at all | Env list explodes; poor for large surfaces and multi-line JVM block |
| | No parity with Helm; weaker diff/patch story; awkward for `spec.config` passthrough |

---

## Comparison

| Criterion | A Fragments | B Single file | C Env vars |
|-----------|-------------|---------------|------------|
| Update diff granularity | **Best** (per key) | Coarse | Coarse |
| Reconcile / merge control ([ADR-006](006-apply-and-idempotency.md)) | **Best** | Manual | Weak |
| Composability (`apoc`, logging) | **Best** | Poor | Poor |
| Filesystem clarity | Poor (directory) | **Best** | n/a |
| Helm parity / migration | **Yes** | No | No |
| Testability (render unit tests) | **Best** (map assertions) | Blob compare | Env compare |
| V1 fit | **Yes** | Possible | No |

---

## Decision

We will keep **Option A** — render `neo4j.conf` as **one ConfigMap key per setting**, project
it at `/config/neo4j.conf` (a directory of fragments), and let the Neo4j image assemble it via
`NEO4J_CONF=/config/` + `EXTENDED_CONF=yes`.

### Why: update & reconcile via the CR

The per-key layout is what makes config a **first-class reconcile target**:

- **Minimal, auditable updates.** Editing one `spec.config.neo4j` key changes exactly one
  fragment. `equality.Semantic.DeepEqual` on `ConfigMap.Data` ([ADR-006](006-apply-and-idempotency.md)) yields a precise
  diff instead of a whole-file rewrite.
- **Predictable ownership merge.** Operator-owned keys (listeners, discovery, JVM block) and
  user `spec.config` passthrough ([BDR-008](../business/neo4j/008-neo4j-config-surface.md)) live as independent keys, so the owned-key
  replace / user-key merge rule in [ADR-006](006-apply-and-idempotency.md) applies cleanly, key by key.
- **Deterministic rollout on change.** A config change bumps `ConfigChecksumAnnotation`
  (`render/serverconfig`), which is stamped on the pod template and forces a controlled
  restart — so the CR remains the single source of truth regardless of `subPath` live-reload
  quirks.
- **Composability.** `apoc.conf`, `server-logs.xml`, and `user-logs.xml` coexist under
  `/config/` without the operator hand-assembling one monolithic file.

`apoc.conf` intentionally stays a **single-key blob** (it is a small, self-contained file with
no owned/user merge concern), showing the two shapes are chosen per surface, not dogmatically.

---

## Consequences

### Positive

- Granular, low-blast-radius config updates driven entirely by the CR.
- Owned-vs-user merge and checksum-driven rollout compose naturally with [ADR-006](006-apply-and-idempotency.md).
- Byte-for-byte Helm parity eases migration and reuses the image's proven mechanism.
- Render logic is unit-testable as `map[string]string` assertions.

### Negative

- `neo4j.conf` presenting as a directory is unintuitive; operators inspecting the pod must
  look at `/config/neo4j.conf/<setting>` fragments and use `SHOW SETTINGS` for effective values.
- Hard dependency on the image `EXTENDED_CONF` contract.

### Neutral

- A future move to a single-file render (Option B) remains possible for surfaces that gain no
  benefit from per-key granularity; decide per surface, not globally.

---

## References

- [BDR-008](../business/neo4j/008-neo4j-config-surface.md) — config surface (`spec.config` / `spec.jvm` / `spec.apoc`)
- [ADR-005](005-render-conventions.md) — ConfigMap naming (`{name}-config`)
- [ADR-006](006-apply-and-idempotency.md) — owned-key replace vs user-key merge; idempotent apply
- `src/internal/render/serverconfig/configmap.go` — one key per setting
- `src/internal/render/workload/statefulset.go` — `/config/neo4j.conf` mount, `NEO4J_CONF` / `EXTENDED_CONF`
- Neo4j Docker image — `EXTENDED_CONF` extended configuration mechanism
