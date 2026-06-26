# BDR-006 — TLS trust model for `Neo4j.spec.trust`

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) |
| **Helm scope** | `ssl`, `ssl.bolt`, `ssl.https`, `ssl.cluster`; group **AGG-TLS-TRUST**; `config.dbms.security.tls_reload_enabled` (coordination with [BDR-008](008-neo4j-config-surface.md)) |
| **Constraints** | `NEO-2-005`, `NEO-3-005-TLS-01..04`; AC `AC-NEO-TLS`, `AC-NEO-TLS-RELOAD`; [Neo4j SSL framework](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) |

---

## Context

Neo4j exposes three independent **SSL policies** — `bolt`, `https`, and `cluster` — each backed by certificate material under `/var/lib/neo4j/certificates/{policy}/`. In the Helm chart, the top-level `ssl` map mirrors Neo4j's `dbms.ssl.policy.*` naming (`values.yaml` L448–483).

### Helm mechanism

| Helm path | Trigger | Operator / Neo4j effect |
|-----------|---------|-------------------------|
| `ssl.{policy}.privateKey.secretName` | Secret present | Mount key at `…/{policy}/private.key`; enable `dbms.ssl.policy.{policy}.enabled` |
| `ssl.{policy}.publicCertificate.secretName` | Paired with privateKey | Mount cert at `…/{policy}/public.crt` |
| `ssl.bolt.privateKey` set | — | `server.bolt.tls_level: REQUIRED` |
| `ssl.https.privateKey` set | — | `server.https.enabled: true` |
| `ssl.{policy}.trustedCerts.sources` | Optional projected volume | Mount at `…/{policy}/trusted/` |
| `ssl.{policy}.revokedCerts.sources` | Optional projected volume | Mount at `…/{policy}/revoked/` |
| `config.dbms.security.tls_reload_enabled` | Default `"true"` in chart values | Hot reload on cert rotation |

Templates: `_ssl.tpl` (volumes + mounts), `neo4j-config.yaml` L174–191 (policy enablement). Go model `Ssl` struct has `Bolt` and `HTTPS` only — **`cluster` exists in values.yaml but not in `release_values.go`** (schema drift).

### Forces

- **Security-critical, breaking contract.** Which connectors require TLS, which Secrets are mounted, and whether cluster inter-member traffic is encrypted are **migration-sensitive** — clients and cluster formation break if changed casually. All four `ssl.*` index rows are **breaking** (BC-006, priority 12).
- **Helm parity vs API clarity.** Helm uses **two Secret references per policy** (private key + public cert) with optional `subPath` overrides, plus projected volumes for trust/revocation lists. That is flexible but verbose and error-prone (Helm `required` guards enforce pairing).
- **cert-manager is a first-class operator path.** `spec.md` and `20-operator-proposal.md` already sketch `trust.certManager` + simplified `certificates.*.secretRef` — cert-manager writes standard `tls.crt` / `tls.key` Secrets that the operator normalizes to Neo4j's on-disk layout.
- **Cross-cutting with config and connectivity.** `dbms.security.tls_reload_enabled` is a config key ([BDR-008](008-neo4j-config-surface.md) promotes it to a typed field under `trust.reload`); publishing `connectivity.external.ports.https` requires HTTPS TLS ([BDR-007](007-service-exposure-connectivity.md), validation NET-003).
- **Field-doc drift.** Helm analysis drafts target `Neo4j.spec.tls.*`; **`spec.md` uses `spec.trust`** — this BDR standardizes on **`spec.trust`**.

This BDR does **not** decide LDAP/mTLS client authentication (`client_auth`) — Helm hard-codes `NONE` for bolt/https in K8s; remains operator-injected default unless overridden via `spec.config` (BDR-008 denylist review).

---

## Cross-cutting rules

| Rule | Rationale |
|------|-----------|
| Operator owns `dbms.ssl.policy.*.enabled` and connector TLS level | Derived from `spec.trust` — users must not set via `spec.config` (reserved key) |
| `trust.enabled: false` is the secure-by-default starting point | Matches `spec.md`; explicit opt-in to TLS |
| `mode: Cluster` + `trust.enabled: true` → cluster policy material required | Validation TLS-003; inter-member encryption |
| cert-manager and BYO `secretRef` are **mutually exclusive per policy** | Avoid dual sources of truth for the same mount path |
| Secret key contract is stable and documented | Admission webhook validates Secret shape when BYO |
| `reload.enabled` injects `dbms.security.tls_reload_enabled` | BDR-008 reserved-key set; not user-supplied via `spec.config` when typed field set |

---

## Helm → operator mapping (target)

| Helm | `Neo4j.spec.trust` (Option B) |
|------|------------------------------|
| `ssl.bolt.privateKey` + `publicCertificate` | `certificates.bolt.secretRef` **or** cert-manager Certificate → Secret |
| `ssl.https.*` | `certificates.https.secretRef` |
| `ssl.cluster.*` | `certificates.cluster.secretRef` |
| `trustedCerts` / `revokedCerts` | **Deferred V1.1** — `additionalVolumes` escape or future `trust.certificates.*.trusted` |
| `config.dbms.security.tls_reload_enabled` | `reload.enabled` |
| (no Helm equivalent) | `certManager.enabled` + `issuerRef` |
| (no Helm equivalent) | `enabled` master toggle |

---

## Options under review

### Option A — Full Helm parity: per-policy nested `privateKey` / `publicCertificate` / `trustedCerts`

Mirror the Helm `ssl` map structure under `spec.trust.policies` (or retain `ssl` naming).

```yaml
spec:
  trust:
    enabled: true
    policies:
      bolt:
        privateKey:
          secretRef:
            name: neo4j-bolt-key
            key: private.key      # optional subPath; default private.key
        publicCertificate:
          secretRef:
            name: neo4j-bolt-cert
            key: public.crt
        trustedCerts:
          secretRefs:           # projected volume sources
            - name: corp-ca
              key: ca.crt
      https: { ... }
      cluster: { ... }
    reload:
      enabled: true
```

| Advantages | Disadvantages |
|------------|---------------|
| **Highest Helm parity** — 1:1 field names and dual-secret pattern | Verbose API; users must pair key + cert Secrets correctly (Helm pain preserved) |
| Supports `trustedCerts` / `revokedCerts` in V1 | Large OpenAPI surface; heavy CEL `oneOf` per policy |
| No cert-manager opinion — BYO only | No first-class cert-manager path without a second parallel shape |
| | Duplicates `spec.md` baseline — requires spec rewrite |

---

### Option B — Curated BYO `secretRef` + cert-manager + `reload` (current `spec.md` baseline)

Intent-based `spec.trust`: one Secret name per policy (BYO), optional cert-manager provisioning, master toggle.

```yaml
spec:
  trust:
    enabled: true
    reload:
      enabled: true              # → dbms.security.tls_reload_enabled
    certManager:
      enabled: false
      issuerRef:
        name: letsencrypt-prod
        kind: ClusterIssuer       # Issuer | ClusterIssuer
    certificates:
      bolt:
        secretRef: neo4j-bolt-tls    # BYO when certManager.enabled=false
      https:
        secretRef: neo4j-https-tls
      cluster:
        secretRef: neo4j-cluster-tls
```

**BYO Secret contract** (operator normalizes to Neo4j paths):

| Secret key (accepted) | Maps to |
|-----------------------|---------|
| `private.key` **or** `tls.key` | `…/{policy}/private.key` |
| `public.crt` **or** `tls.crt` | `…/{policy}/public.crt` |

Webhook TLS-002 validates existence + required keys when `trust.enabled` and BYO mode.

**cert-manager path** (when `certManager.enabled: true`):

1. Operator creates `cert-manager.io/v1` `Certificate` per enabled policy (dnsNames from `connectivity` + cluster internal DNS).
2. cert-manager writes `tls.crt` / `tls.key` Secret (same name as `certificates.{policy}.secretRef` or operator-owned name in status).
3. Operator mounts and injects policy enablement — same as BYO.
4. `reload.enabled: true` (default when cert-manager on) enables hot rotation.

| Advantages | Disadvantages |
|------------|---------------|
| Matches **existing `spec.md`** and `20-operator-proposal.md` | Loses Helm dual-secret + `subPath` flexibility in V1 |
| **cert-manager first-class** — aligns with operator value-add | `trustedCerts` / `revokedCerts` not in V1 — enterprise PKI edge cases need escape hatch |
| One `secretRef` per policy — simpler UX than Helm | Users with split key/cert Secrets must merge or use cert-manager |
| Clear mutual exclusion: cert-manager **or** BYO per policy | Custom key names beyond the contract need V1.1 `keys` override |
| Typed `reload.enabled` — clean BDR-008 boundary | |

---

### Option C — Single unified TLS Secret for all policies

One Secret (or cert-manager Certificate) shared across bolt, https, and cluster when SANs allow.

```yaml
spec:
  trust:
    enabled: true
    secretRef: neo4j-unified-tls    # single Secret for all policies
    policies: [bolt, https, cluster] # which policies use the unified material
    reload:
      enabled: true
```

| Advantages | Disadvantages |
|------------|---------------|
| Minimal API — one Secret to manage | Cluster certs often need different SANs / CAs than client-facing bolt/https |
| Good for dev/single-host wildcard certs | Breaks Helm model (separate secrets per policy) |
| | Forces all-or-nothing TLS — cannot enable bolt-only |
| | Conflicts with FR variants `NEO-3-005-TLS-01/02/03` tested independently |

---

### Option D — cert-manager only in V1; BYO deferred

```yaml
spec:
  trust:
    enabled: true
    certManager:
      enabled: true               # required in V1
      issuerRef: { name: ..., kind: ClusterIssuer }
    reload:
      enabled: true
```

| Advantages | Disadvantages |
|------------|---------------|
| Smallest implementation — no BYO webhook validation | **Blocks** air-gapped / corporate PKI BYO Secrets (common in production) |
| Forces GitOps-friendly cert rotation | No Helm migration path for existing `ssl.*.secretName` users |
| | Fails FR `NEO-3-005-TLS-01..03` BYO scenarios in test catalog |

---

## Comparison

| Criterion | A — Helm mirror | B — secretRef + cert-manager | C — unified | D — cert-manager only |
|-----------|-----------------|------------------------------|-------------|----------------------|
| Helm parity | ✅ highest | ⚠️ mapping table | ❌ | ❌ |
| Matches current `spec.md` | ❌ | ✅ | ❌ | ⚠️ partial |
| API minimalism | ❌ | ✅ | ✅ best | ✅ |
| cert-manager support | ⚠️ bolt-on | ✅ first-class | ✅ | ✅ only path |
| BYO corporate PKI | ✅ | ✅ | ✅ | ❌ |
| trustedCerts / revokedCerts V1 | ✅ | ❌ deferred | ❌ | ❌ |
| Operator complexity | High | Medium | Low | Low–Medium |
| Breaking risk | Medium | **Low** (locked early) | Medium | High (BYO users blocked) |
| FR / test catalog fit | ✅ | ✅ | ⚠️ | ❌ |

---

## Decision

**Not decided.** Pending reviewer sign-off.

**Proposer direction:** Adopt **Option B** — keep the `spec.trust` shape already in [`spec.md`](../../09-crd-spec/neo4j/spec.md): `enabled`, `certManager`, `certificates.{bolt,https,cluster}.secretRef`, and `reload.enabled`. The operator normalizes Secret keys, injects `dbms.ssl.policy.*` and connector settings (mirroring `neo4j-config.yaml`), and optionally owns cert-manager `Certificate` resources.

**Recommendation:**

1. **V1 = Option B.** Document the BYO Secret key contract (`private.key`/`tls.key`, `public.crt`/`tls.crt`). Webhook TLS-002 validates Secrets; CEL TLS-001/003 as in `validation.md`.
2. **Default `trust.enabled: false`** — secure starting point; enabling HTTPS external port (NET-003) requires `trust.enabled` + https material.
3. **`reload.enabled` defaults `true` when `certManager.enabled: true`**, else `false` — matches Helm's default `tls_reload_enabled: "true"` for cert-manager flows; explicit for BYO.
4. **Defer `trustedCerts` / `revokedCerts` / per-key `subPath` overrides to V1.1** — document `podTemplate` / `additionalVolumes` escape for enterprise PKI until promoted.
5. **Reserve** `dbms.ssl.policy.*`, `server.bolt.tls_level`, `server.https.enabled`, `dbms.security.tls_reload_enabled` in BDR-008 denylist when `spec.trust` fields are set.
6. **Do not** rename `spec.trust` to `spec.tls` — `trust` matches operator proposal and separates user intent from Helm's `ssl` values key.

---

## Consequences

### Positive

- Stable, documented BYO Secret contract — admission catches misconfiguration before pod start.
- cert-manager integration is the operator's differentiated path (proposal §6.9).
- Aligns with connectivity (NET-003) and config (BDR-008) boundaries.
- Simpler migration narrative than Helm's dual-secret-per-policy.

### Negative

- Helm users with **split** key/cert Secrets must consolidate or switch to cert-manager — migration guide entry required.
- Enterprise trust stores (`trustedCerts.sources`) not in V1 API — workaround via volumes until V1.1.
- Operator must implement Secret key normalization and cert-manager `Certificate` lifecycle (owner references, renewal).

### Neutral

- `neo4j.operations.ssl` (Job skip-verify flags) is operator-internal — not part of `spec.trust`.
- mTLS / `client_auth` remains `NONE` by default (Helm parity); advanced auth is config escape or V2.
- Field docs and `_index.csv` `crd_target` should use `Neo4j.spec.trust.*` not `spec.tls.*`.

---

## References

- `design/analysis/helm-fields/_index.csv` — rows `ssl`, `ssl.bolt`, `ssl.https`, `ssl.cluster`
- `design/analysis/helm-fields/aggregation-matrix.md` — `AGG-TLS-TRUST`
- `design/analysis/helm-fields/breaking-change-register.md` — BC-006
- `design/analysis/helm-fields/fields/ssl*.md`, `config.md` (CONCERN-TLS)
- `helm-charts/neo4j/templates/_ssl.tpl`, `neo4j-config.yaml`
- `helm-charts/neo4j/values.yaml` — `ssl` block L448–483
- [`spec.md`](../../09-crd-spec/neo4j/spec.md) — `spec.trust`
- [`validation.md`](../../09-crd-spec/neo4j/validation.md) — TLS-001..003, NET-003
- [BDR-001](001-single-neo4j-crd.md) — embedded trust section
- [BDR-007](007-service-exposure-connectivity.md) — port ↔ TLS coherence
- [BDR-008](008-neo4j-config-surface.md) — `reload` vs config denylist
- [ADR-001](../architecture/001-crd-validation-process.md) — CEL + webhook split
