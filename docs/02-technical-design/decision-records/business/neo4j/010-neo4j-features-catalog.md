# BDR-010 — `spec.features` catalog (beyond backup & monitoring)

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-22 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-007](006-service-exposure-connectivity.md) — Option E `features` + `connectivity.listeners` (accepted) · [BDR-008](008-neo4j-config-surface.md) — `spec.config` passthrough (accepted) |
| **Related** | [BDR-002](002-neo4j-crd-topology.md) (topology) · [BDR-004](004-neo4j-plugin-topology.md) (plugins) · [BDR-005](005-storage-volume-mode.md) (volumes) · [BDR-006](007-tls-trust-model.md) (trust) |

---

## Context

[BDR-007](006-service-exposure-connectivity.md) introduced `spec.features` as the home for **optional workload capabilities** that gate connectors and derived Services. V1 ships two feature groups:

| Feature | Gates | Derived effect |
|---------|-------|----------------|
| `features.backup.enabled` | `connectivity.listeners.backup` set | **admin** Service |
| `features.monitoring.prometheus.enabled` | `connectivity.listeners.metrics` set | **admin** Service + metrics scrape |
| `features.monitoring.serviceMonitor.enabled` | — | `ServiceMonitor` CR |

The Helm chart scatters the same concerns across `config.*`, top-level keys (`serviceMonitor`), and templates. [BDR-008](008-neo4j-config-surface.md) keeps `spec.config` as a passthrough map for Helm migration — but users cannot easily discover which `neo4j.conf` keys belong to backup or monitoring without reading chart templates.

This BDR defines the **catalog rule** and resolves how feature-related settings are surfaced in the CRD.

### Candidate inventory (Helm / FR scan)

| Helm / FR area | Example Helm path | Typical effect | Feature candidate? |
|----------------|-------------------|----------------|-------------------|
| Enterprise backup | `config.server.backup.enabled` | backup connector, admin port | ✅ **V1** — `features.backup` |
| Prometheus metrics | `config.server.metrics.prometheus.*` | metrics connector | ✅ **V1** — `features.monitoring.prometheus` |
| CSV / JMX / Graphite metrics | `config.server.metrics.{csv,jmx,graphite}.*` | alternate exporters | ✅ **V1** — `features.monitoring.*` (discoverability) |
| ServiceMonitor | `serviceMonitor.*` | Prometheus Operator CR | ✅ **V1** — `features.monitoring.serviceMonitor` |
| Analytics topology | `analytics.enabled` | GDS pool, internals ports | ❌ — **topology** ([BDR-002](002-neo4j-crd-topology.md)) |
| Plugins (APOC, GDS, Bloom) | `neo4j.plugins` / env | JAR install, licensing | ❌ — **plugins** ([BDR-004](004-neo4j-plugin-topology.md)) |
| TLS / mTLS | `ssl.*`, `dbms.ssl.policy.*` | certificate policies | ❌ — **trust** ([BDR-006](007-tls-trust-model.md)) |
| Log rotation | `config.dbms.logs.*`, `logging.*` | log retention | ⚠️ — V1.1 `features.logging` |
| JMX / diagnostics (beyond config flags) | sidecars, deep JVM | JVM / ops endpoints | ⚠️ — deferred |
| Operations Job | `neo4j.operations.*` | ENABLE SERVER on scale | ❌ — operator workflow ([BDR-009](009-scale-pool-ordinal-semantics.md)) |
| Import / bulk load | `volumes.import` | mount only | ❌ feature — **volumes** ([BDR-005](005-storage-volume-mode.md)); mount **implemented** |
| Backup / metrics storage | `volumes.backups`, `volumes.metrics` | PVC mounts | ❌ feature — **volumes** ([BDR-005](005-storage-volume-mode.md)); mounts **implemented** |
| Multi-cluster K8s | `services.neo4j.multiCluster` | extra LB ports | ❌ — **connectivity** ([BDR-007](006-service-exposure-connectivity.md)) |
| LDAP / auth providers | `config.dbms.security.*` | auth stack | ❌ — **auth** + **config** |
| Index / constraint auto-create | `config` flags | DDL on startup | ⚠️ — config only |

---

## Analysis

### What `features` means (definition)

A **`spec.features` entry** is appropriate when **all** of the following hold:

1. **Optional capability** — safe and supported when off; not required for minimal Standalone.
2. **Cross-cutting** — affects more than one layer (connector + Service + optional CR + config snippet).
3. **Intent, not mechanism** — user says *"I want backup"* not *"listen on 6362"* (listen port stays in `connectivity.listeners`).
4. **Stable product vocabulary** — aligns with Neo4j docs / support language (backup, monitoring, …).

What **does not** belong in `features`:

| Concern | Better home |
|---------|-------------|
| How many servers, which pools | `spec.topology` |
| Which JARs and licenses | `spec.plugins` + `spec.pluginDefinitions` |
| Certificates, mTLS | `spec.trust` |
| Service type, exposed ports | `spec.connectivity` |
| PVC roles and mount paths | `spec.volumes` |
| Unrelated `neo4j.conf` tuning | `spec.config` (passthrough + denylist per [BDR-008](008-neo4j-config-surface.md)) |

### Options for V1

#### Option A — Minimal `features` (status quo + explicit defer list)

Keep V1 as backup + monitoring `enabled` flags only. Document deferred candidates; all tuning stays in `spec.config`.

| Advantages | Disadvantages |
|------------|---------------|
| Small, reviewable API | Poor discoverability — users grep Helm templates for related `config` keys |
| Clear precedent from BDR-007 | Two mental models: "where is my CSV metrics interval?" |

#### Option B — Grouped `features` tree (typed fields only)

Nested groups under `features`; every capability gets typed first-class fields. No `spec.config` overlap for feature keys.

| Advantages | Disadvantages |
|------------|---------------|
| Full `kubectl explain` discoverability | Breaks Helm `config` drop-in for feature keys |
| Single source of truth | Large CRD surface; lags Neo4j minors for exotic keys |

#### Option C — Feature gates + colocated `neo4j.conf` mirrors — **proposer direction**

`features.*.enabled` gates the capability. **Every `neo4j.conf` key that belongs to a V1 feature** is also exposed as a typed field under the matching `features.*` group so users can configure the feature in one place.

`spec.config` **remains** a passthrough map ([BDR-008](008-neo4j-config-surface.md) Option A) for Helm migration and advanced tuning. When the same key appears in **both** `features` and `config`, validation enforces **coherence** (CFG-FEAT-*). When only one side is set, the operator merges into `neo4j.conf`.

| Advantages | Disadvantages |
|------------|---------------|
| Discoverability without giving up Helm `config` migration | Two valid paths — validation must stay strict |
| Clean split: intent (`enabled`) vs tuning (colocated fields) | Operator merge precedence rules to document |
| Aligns with BDR-008 passthrough | Slightly larger `features` schema |

**Rejected for V1:** Option A (insufficient discoverability). **Not adopted:** Option B alone (migration cliff for feature keys).

### Three-layer model (Option C)

Each V1 feature is configured through three layers — only one owner per key:

| Layer | Role | Example |
|-------|------|---------|
| **1 — Intent** | `features.<group>.enabled` | `features.backup.enabled` |
| **2 — Mechanism** | `connectivity`, `trust`, `volumes` | `connectivity.listeners.backup`, `trust.certificates.backup`, `volumes.backups` |
| **3 — Tuning** | `features.<group>.*` mirrors + optional `spec.config` | `features.monitoring.csv.interval` ↔ `server.metrics.csv.interval` |

**Merge rule:** operator projects layer 1 + 2 + 3 into `neo4j.conf`. Layer 3: `features` fields win over absent `config` keys; if both set → must match (CFG-FEAT) or admission fails.

**Port-owned keys** (`server.*.listen_address`, connector `*.enabled` tied to `connectivity.listeners`) stay on the CFG-LISTENER denylist — not duplicated under `features`.

**Trust-owned keys** (`dbms.ssl.policy.backup.*`) stay under `spec.trust` — not under `features.backup`.

**Volume-owned keys** (`server.directories.metrics` when `volumes.metrics` is set) are operator-injected from [BDR-005](005-storage-volume-mode.md) — not under `features`.

---

## V1 feature parameter inventory

Complete list of parameters tied to V1 `features.backup` and `features.monitoring`, mapped from Helm / Neo4j docs / chart templates.

### `features.backup`

| `neo4j.conf` key | CRD field | Owner if not `features` | Notes |
|------------------|-----------|---------------------------|-------|
| `server.backup.enabled` | `features.backup.enabled` | — | Enterprise only; gates backup connector + admin Service |
| `server.backup.listen_address` | — | `connectivity.listeners.backup` | CFG-LISTENER denylist — port from `connectivity.listeners.backup` |
| `dbms.ssl.policy.backup.*` | — | `spec.trust.certificates.backup` | TLS policy — [BDR-006](007-tls-trust-model.md) |

Helm non-config (not in `features`):

| Helm path | CRD home |
|-----------|----------|
| `volumes.backups` | `spec.volumes.backups` |
| `services.neo4j.ports.backup` | `connectivity.service.expose` includes `backup` |
| `services.admin` (derived) | operator-derived when backup OR prometheus OR Cluster |

**V1 `features.backup` schema** — only `enabled` (no additional tuning keys beyond enablement).

```yaml
features:
  backup:
    enabled: true
```

### `features.monitoring`

#### Prometheus (`features.monitoring.prometheus`)

| `neo4j.conf` key | CRD field | Owner if not `features` | Default (Neo4j) |
|------------------|-----------|---------------------------|-----------------|
| `server.metrics.prometheus.enabled` | `prometheus.enabled` | — | `false` |
| `server.metrics.prometheus.endpoint` | `prometheus.endpoint` | port from `connectivity.listeners.metrics` | `localhost:2004` |

CEL (existing): `connectivity.listeners.metrics` set ⇒ `features.monitoring.prometheus.enabled`.

Webhook (CFG-FEAT / CFG-LISTENER): numeric port parsed from `prometheus.endpoint` MUST match `connectivity.listeners.metrics`.

#### CSV metrics (`features.monitoring.csv`)

Requires Enterprise + `spec.volumes.metrics` for directory injection (`server.directories.metrics` — operator-owned, not in `features`).

| `neo4j.conf` key | CRD field | Default (Neo4j) |
|------------------|-----------|-----------------|
| `server.metrics.csv.enabled` | `csv.enabled` | `true` |
| `server.metrics.csv.interval` | `csv.interval` | `30s` |
| `server.metrics.csv.rotation.keep_number` | `csv.rotation.keepNumber` | `7` |
| `server.metrics.csv.rotation.size` | `csv.rotation.size` | `10.00MiB` |
| `server.metrics.csv.rotation.compression` | `csv.rotation.compression` | `NONE` |

#### JMX (`features.monitoring.jmx`)

Helm gates admin Service port exposure on this flag (`neo4j-svc.yaml`).

| `neo4j.conf` key | CRD field | Default (Neo4j) |
|------------------|-----------|-----------------|
| `server.metrics.jmx.enabled` | `jmx.enabled` | `true` |

#### Graphite (`features.monitoring.graphite`)

| `neo4j.conf` key | CRD field | Default (Neo4j) |
|------------------|-----------|-----------------|
| `server.metrics.graphite.enabled` | `graphite.enabled` | `false` |
| `server.metrics.graphite.server` | `graphite.server` | `localhost:2003` |
| `server.metrics.graphite.interval` | `graphite.interval` | `30s` |
| `server.metrics.prefix` | `graphite.prefix` | `neo4j` |

#### ServiceMonitor (`features.monitoring.serviceMonitor`)

Kubernetes CR — not `neo4j.conf`. Maps from Helm `serviceMonitor.*`.

| Helm field | CRD field | Default |
|------------|-----------|---------|
| `enabled` | `serviceMonitor.enabled` | `false` |
| `labels` | `serviceMonitor.labels` | `{}` |
| `jobLabel` | `serviceMonitor.jobLabel` | `""` |
| `interval` | `serviceMonitor.interval` | `30s` |
| `port` | `serviceMonitor.port` | `tcp-prometheus` |
| `path` | `serviceMonitor.path` | operator default scrape path |
| `namespaceSelector` | `serviceMonitor.namespaceSelector` | `{}` |
| `targetLabels` | `serviceMonitor.targetLabels` | `[]` |
| `selector` | `serviceMonitor.selector` | operator default (admin Service) |

Requires `features.monitoring.prometheus.enabled` and Prometheus Operator CRD in cluster.

### V1 `features.monitoring` example

```yaml
features:
  monitoring:
    prometheus:
      enabled: true
      endpoint: "0.0.0.0:2004"
    csv:
      enabled: true
      interval: 30s
      rotation:
        keepNumber: 7
        size: 10MiB
        compression: zip
    jmx:
      enabled: true
    graphite:
      enabled: false
    serviceMonitor:
      enabled: true
      interval: 30s
      labels:
        release: prometheus
```

### Config coexistence (CFG-FEAT)

When a key from the inventory above appears in **both** `features` and `spec.config`:

| Rule | Severity | Example message |
|------|----------|-----------------|
| CFG-FEAT-001 | Values must be identical (string-normalized: `true`/`yes`) | `spec.config server.metrics.csv.interval=60s contradicts features.monitoring.csv.interval=30s` |
| CFG-FEAT-002 | `features.*.enabled` must match `server.*.enabled` in config when both set | `features.backup.enabled=false but spec.config server.backup.enabled=true` |

Keys **only** in `spec.config` → passed through (Helm migration). Keys **only** in `features` → operator writes to `neo4j.conf`. Keys in **neither** → Neo4j / operator defaults apply.

**Amendment to BDR-008 denylist:** feature-tuning keys (`server.metrics.csv.*`, `server.metrics.graphite.*`, `server.metrics.jmx.enabled`, `server.metrics.prometheus.enabled`, `server.metrics.prometheus.endpoint`, `server.backup.enabled`) are **removed** from the strict CFG-LISTENER denylist and governed by CFG-FEAT coherence instead. Port/listen keys (`server.backup.listen_address`, connector `*.enabled` for bolt/http/https) remain CFG-LISTENER denylist.

---

## Decision

**We will implement Option C for V1** (feature gates + colocated `neo4j.conf` mirrors) with:

1. **Four-criteria definition** (above) before adding any new `features.*` group beyond backup + monitoring.
2. **Full parameter inventory** per feature group — every related `neo4j.conf` key listed in this BDR and exposed under `features` (except keys owned by `connectivity`, `trust`, or `volumes`).
3. **`spec.config` passthrough preserved** — Helm migration unchanged; CFG-FEAT coherence when both paths set the same key.
4. **V1.1 candidates** (`features.logging`, diagnostics) tracked here with FR links before implementation.

**Not in V1 `features`:** topology, plugins, trust, volumes, raw escape-hatch tuning unrelated to backup/monitoring.

---

## Consequences

### Positive

- Users find all backup/monitoring settings under `spec.features` without spelunking Helm templates.
- Helm `config` maps still work — dual-path with explicit validation.
- Reviewers have a checklist and inventory when mapping new Helm fields.

### Negative

- Operator must implement merge + CFG-FEAT webhook rules for every inventoried key.
- BDR-008 denylist excerpt must be amended (feature keys → CFG-FEAT, not CFG-LISTENER).

### Neutral

- Each new V1.1+ feature group adds: inventory table, `features` schema, CFG-FEAT rows, Helm migration row in `_index.csv`.
- `design/09-crd-spec/neo4j/spec.md` and `validation.md` carry the normative field list.

---

## References

- [BDR-007](006-service-exposure-connectivity.md) — accepted Option E
- [BDR-008](008-neo4j-config-surface.md) — accepted Option A (amended by CFG-FEAT)
- `helm-charts/neo4j/templates/neo4j-svc.yaml` — backup / prometheus / jmx / graphite gates
- `helm-charts/neo4j/templates/neo4j-config.yaml` — backup listen_address injection
- `helm-charts/neo4j/templates/neo4j-servicemonitor.yaml` — ServiceMonitor template
- [Neo4j — Expose metrics](https://neo4j.com/docs/operations-manual/current/monitoring/metrics/expose/)
- [Neo4j — Online backup server configuration](https://neo4j.com/docs/operations-manual/current/backup-restore/online-backup/)
- `design/analysis/helm-fields/_index.csv` — full Helm inventory
- `design/09-crd-spec/neo4j/spec.md` — `spec.features`
- `design/09-crd-spec/neo4j/validation.md` — CFG-LISTENER, CFG-FEAT
