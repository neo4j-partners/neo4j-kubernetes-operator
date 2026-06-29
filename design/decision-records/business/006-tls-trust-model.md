# BDR-006 — TLS trust model for `Neo4j.spec.trust`

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-22 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) |
| **Helm scope** | `ssl`, `ssl.bolt`, `ssl.https`, `ssl.cluster`; group **AGG-TLS-TRUST**; `config.dbms.security.tls_reload_enabled` (coordination with [BDR-008](008-neo4j-config-surface.md)) |
| **Constraints** | `NEO-2-005`, `NEO-3-005-TLS-01..04`; AC `AC-NEO-TLS`, `AC-NEO-TLS-RELOAD`; [Neo4j SSL framework](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) |

---

## Context

Neo4j exposes three independent **SSL policies** — `bolt`, `https`, and `cluster` — each backed by certificate material under `/var/lib/neo4j/certificates/{policy}/`. In the Helm chart, the top-level `ssl` map mirrors Neo4j's `dbms.ssl.policy.*` naming (`values.yaml` L448–483).

### Helm mechanism (`values.yaml`)

```yaml
ssl:
  bolt:
    privateKey:
      secretName: neo4j-bolt-key      # Secret holding the key
      subPath: private.key            # optional — default private.key
    publicCertificate:
      secretName: neo4j-bolt-cert     # Secret holding the cert (may differ from key Secret)
      subPath: public.crt             # optional — default public.crt
  https: { ... }
  cluster: { ... }
```

| Helm path | Trigger | Operator / Neo4j effect |
|-----------|---------|-------------------------|
| `ssl.{policy}.privateKey.secretName` | set | Mount at `…/{policy}/private.key` (or `subPath`); enable policy |
| `ssl.{policy}.publicCertificate.secretName` | paired with privateKey | Mount at `…/{policy}/public.crt` (or `subPath`) |
| `ssl.bolt.privateKey` set | — | `server.bolt.tls_level: REQUIRED` |
| `ssl.https.privateKey` set | — | `server.https.enabled: true` |
| `ssl.{policy}.trustedCerts.sources` | optional | Projected volume → `…/{policy}/trusted/` (client / peer CA certs) |
| `ssl.{policy}.revokedCerts.sources` | optional | **Deferred V1.1** |
| `config.dbms.security.tls_reload_enabled` | default `"true"` in chart | → `trust.reload.enabled` |

Templates: `_ssl.tpl`, `neo4j-config.yaml` L174–191.

### mTLS (mutual TLS / client authentication)

Neo4j controls mutual authentication per policy via `dbms.ssl.policy.{policy}.client_auth`:

| Neo4j value | Meaning |
|-------------|---------|
| `NONE` | Server TLS only — clients are not authenticated by certificate |
| `OPTIONAL` | Clients may present a certificate |
| `REQUIRE` | Mutual TLS — clients must present a valid certificate |

**Trusted material:** client (or cluster peer) CAs are mounted under `/var/lib/neo4j/certificates/{policy}/trusted/` via Helm `trustedCerts.sources` (projected volume — same shape as Kubernetes `projected.sources`).

**Helm chart defaults** (`neo4j-config.yaml` injected keys):

| Policy | `client_auth` injected | mTLS |
|--------|------------------------|------|
| `bolt` | `NONE` | Off — TLS server-only |
| `https` | `NONE` | Off |
| `cluster` | `REQUIRE` | **On** — inter-member mutual auth |

Helm does **not** expose `client_auth` in `values.yaml`; bolt/https mTLS today requires ad-hoc `config.dbms.ssl.policy.bolt.client_auth: "REQUIRE"` plus `ssl.bolt.trustedCerts.sources`. Cluster mTLS is always on when the cluster SSL policy is enabled.

**Operator V1:** model `clientAuth` and `trustedCerts` in `spec.trust.certificates.{policy}` so bolt/https mTLS is first-class (not a `config` escape hatch). Cluster policy keeps operator-injected `REQUIRE` (not user-disableable in `mode: Cluster`).

### Forces

- **Security-critical, breaking contract** — BC-006.
- **Helm parity on field names** — use `secretName` + `subPath`, not `secretRef` (Kubernetes Secret reference type name is misleading here; Helm uses `secretName`).
- **cert-manager is optional** — `certManager.enabled` defaults **`false`**; BYO Secrets is the V1 default path (corporate PKI, air-gapped).
- **Cross-cutting** — `reload.enabled` ↔ BDR-008; HTTPS external port ↔ BDR-007 NET-003.
- **CRD section** — `spec.trust` (not `spec.tls`, not Helm key `ssl`).

---

## Cross-cutting rules

| Rule | Rationale |
|------|-----------|
| Operator owns `dbms.ssl.policy.*.enabled`, `*.client_auth`, and connector TLS level | Reserved in BDR-008 denylist — users set `clientAuth` on `spec.trust`, not raw `config` |
| `trust.enabled` defaults **`false`** | Explicit opt-in to TLS |
| `certManager.enabled` defaults **`false`** | BYO is default; cert-manager is opt-in |
| BYO: `privateKey.secretName` + `publicCertificate.secretName` both required per enabled policy | Mirrors Helm pairing (`_ssl.tpl` `required` guards) |
| `subPath` optional — defaults `private.key` / `public.crt` | Same as Helm `_ssl.tpl` |
| cert-manager: one `secretName` per policy — operator creates `Certificate` → Secret | cert-manager writes `tls.crt` + `tls.key`; operator maps to Neo4j paths |
| cert-manager and BYO shapes **mutually exclusive per policy** | CEL `oneOf` on policy block |
| `mode: Cluster` + `trust.enabled` → cluster policy material required | TLS-003 |
| `clientAuth: Optional` or `Require` on bolt/https → `trustedCerts.sources` non-empty | TLS-004 |
| Cluster policy enabled → operator injects `client_auth: REQUIRE`; `clientAuth: None` rejected | TLS-005 |
| `clientAuth` omitted → bolt/https default `None`; cluster default `Require` | Helm parity |

---

## Helm → operator mapping (Option B)

| Helm `ssl.{policy}` | `Neo4j.spec.trust.certificates.{policy}` |
|---------------------|------------------------------------------|
| `privateKey.secretName` | `privateKey.secretName` |
| `privateKey.subPath` | `privateKey.subPath` |
| `publicCertificate.secretName` | `publicCertificate.secretName` |
| `publicCertificate.subPath` | `publicCertificate.subPath` |
| `trustedCerts.sources` | `trustedCerts.sources` (projected volume; required for bolt/https mTLS) |
| `revokedCerts.sources` | **Deferred V1.1** |
| — (injected in `neo4j-config.yaml`) | `clientAuth`: `None` \| `Optional` \| `Require` |
| `config.dbms.security.tls_reload_enabled` | `reload.enabled` |
| — | `enabled` (master toggle) |
| — | `certManager.*` (operator-only; default off) |

---

## Options under review

### Option A — Full Helm parity including `revokedCerts` in V1

Same shape as Option B but also projects `revokedCerts.sources` in V1.

| Advantages | Disadvantages |
|------------|---------------|
| Complete Helm surface incl. CRL | Largest API + operator surface |
| | `revokedCerts` rarely used in practice |

---

### Option B — Helm-shaped BYO (`secretName` + `subPath`) + optional cert-manager — **accepted**

#### API shape

```yaml
spec:
  trust:
    enabled: true                    # default: false
    reload:
      enabled: false                 # default: false; set true for cert rotation (NEO-3-005-TLS-04)
    certManager:
      enabled: false                 # default: false
      issuerRef:
        name: letsencrypt-prod
        kind: ClusterIssuer            # Issuer | ClusterIssuer
    certificates:
      bolt:
        privateKey:
          secretName: neo4j-bolt-key
          subPath: private.key         # optional
        publicCertificate:
          secretName: neo4j-bolt-cert
          subPath: public.crt          # optional
        clientAuth: None                 # default — None | Optional | Require
        trustedCerts:                    # required when clientAuth is Optional or Require
          sources: []
      https:
        privateKey: { secretName: ..., subPath: ... }
        publicCertificate: { secretName: ..., subPath: ... }
      cluster:
        privateKey: { secretName: ..., subPath: ... }
        publicCertificate: { secretName: ..., subPath: ... }
        clientAuth: Require              # default when cluster policy enabled; cannot be None in Cluster mode
        trustedCerts:
          sources: []                    # peer CA / member certs for inter-node mTLS
```

**Per-policy `oneOf` (when policy is used):**

| Mode | Shape | When |
|------|-------|------|
| **BYO** | `privateKey` + `publicCertificate` (each with `secretName`, optional `subPath`) | `certManager.enabled: false` |
| **cert-manager** | `secretName` only (target Secret for issued cert) | `certManager.enabled: true` |

```yaml
# cert-manager shape (per policy) — no privateKey/publicCertificate blocks
certificates:
  bolt:
    secretName: neo4j-bolt-tls    # operator creates Certificate with secretName: neo4j-bolt-tls
```

#### Operator behaviour (mount paths)

Identical to Helm `_ssl.tpl`:

| Pod path | Source |
|----------|--------|
| `/var/lib/neo4j/certificates/{policy}/private.key` | `privateKey.secretName` key `subPath` (default `private.key`) **or** cert-manager `tls.key` |
| `/var/lib/neo4j/certificates/{policy}/public.crt` | `publicCertificate.secretName` key `subPath` (default `public.crt`) **or** cert-manager `tls.crt` |
| `/var/lib/neo4j/certificates/{policy}/trusted/*` | `trustedCerts.sources` projected volume (Helm `_ssl.tpl` trusted mount) |

Neo4j config injected when `privateKey` material present (BYO) or policy `secretName` populated (cert-manager):

- `dbms.ssl.policy.{policy}.enabled`
- `dbms.ssl.policy.{policy}.client_auth` ← from `clientAuth` (`None` → `NONE`, `Optional` → `OPTIONAL`, `Require` → `REQUIRE`)
- bolt `tls_level` / https `enabled` per Helm
- cluster: always `REQUIRE` when `mode: Cluster` (TLS-005), even if `clientAuth` omitted

---

### Example 1 — BYO without cert-manager (corporate PKI)

Typical production: Secrets created out-of-band (Vault, cert tooling), split key/cert Secrets, custom key names via `subPath`.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Cluster
    primaries:
      members: 3
  trust:
    enabled: true
    reload:
      enabled: true                    # hot reload when Secrets rotated in-place
    certManager:
      enabled: false                   # default — BYO
    certificates:
      bolt:
        privateKey:
          secretName: prod-neo4j-bolt-key
          subPath: server.key            # non-default key name in Secret
        publicCertificate:
          secretName: prod-neo4j-bolt-cert
          subPath: server.crt
      https:
        privateKey:
          secretName: prod-neo4j-https-key
        publicCertificate:
          secretName: prod-neo4j-https-cert
      cluster:
        privateKey:
          secretName: prod-neo4j-cluster-key
        publicCertificate:
          secretName: prod-neo4j-cluster-cert
  connectivity:
    external:
      enabled: true
      ports:
        bolt: true
        https: true
```

**Prerequisites (user-managed):**

```yaml
# Secret prod-neo4j-bolt-key — data.server.key: <PEM private key>
# Secret prod-neo4j-bolt-cert — data.server.crt: <PEM cert>
```

**Operator:** validates both Secrets exist (TLS-002), mounts with `subPath`, enables SSL policies. No `Certificate` CR created.

**Helm migration:** copy `ssl.bolt.privateKey.secretName` → `trust.certificates.bolt.privateKey.secretName` (rename root `ssl` → `trust` only).

---

### Example 2 — cert-manager enabled

Operator owns `cert-manager.io/v1` `Certificate` per configured policy; user supplies Issuer + target `secretName`.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Cluster
    primaries:
      members: 3
  trust:
    enabled: true
    reload:
      enabled: true                    # recommended with cert-manager rotation
    certManager:
      enabled: true
      issuerRef:
        name: letsencrypt-prod
        kind: ClusterIssuer
    certificates:
      bolt:
        secretName: prod-neo4j-bolt-tls
      https:
        secretName: prod-neo4j-https-tls
      cluster:
        secretName: prod-neo4j-cluster-tls
  connectivity:
    external:
      enabled: true
      type: LoadBalancer
      ports:
        bolt: true
        https: true
```

**Operator creates (per policy):**

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: prod-neo4j-bolt-tls
  ownerReferences: [ Neo4j CR ]
spec:
  secretName: prod-neo4j-bolt-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - prod.neo4j.svc.cluster.local
    - bolt.prod.example.com          # derived from connectivity + cluster DNS
  privateKey:
    algorithm: RSA
    size: 2048
```

**Flow:**

1. cert-manager issues cert → Secret `prod-neo4j-bolt-tls` with `tls.crt` + `tls.key`.
2. Operator mounts `tls.key` → `…/bolt/private.key`, `tls.crt` → `…/bolt/public.crt`.
3. Neo4j config: policy enabled, `reload.enabled` → `dbms.security.tls_reload_enabled`.
4. On renewal, cert-manager updates Secret; reload picks up new material.

**Defaults:** `certManager.enabled: false` — this example requires explicit `enabled: true`.

---

### Example 3 — BYO Bolt mTLS (client certificate authentication)

Corporate PKI: server TLS + clients must present a certificate signed by a corporate CA. Mirrors [Neo4j Kubernetes SSL doc](https://neo4j.com/docs/operations-manual/current/kubernetes/security/) pattern (`client_auth` + `trustedCerts`).

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod-mtls
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  trust:
    enabled: true
    certManager:
      enabled: false
    certificates:
      bolt:
        privateKey:
          secretName: prod-bolt-key
        publicCertificate:
          secretName: prod-bolt-cert
        clientAuth: Require
        trustedCerts:
          sources:
            - secret:
                name: corp-client-ca
                items:
                  - key: ca.crt
                    path: corp-client-ca.crt
  connectivity:
    external:
      enabled: true
      ports:
        bolt: true
```

**Prerequisites:**

```yaml
# Secret corp-client-ca — data.ca.crt: <PEM CA used to sign client certificates>
```

**Operator injects:**

- `dbms.ssl.policy.bolt.enabled: "true"`
- `server.bolt.tls_level: REQUIRED`
- `dbms.ssl.policy.bolt.client_auth: REQUIRE`
- Projected volume: `corp-client-ca.crt` → `/var/lib/neo4j/certificates/bolt/trusted/`

**Helm migration:** set `config.dbms.ssl.policy.bolt.client_auth: "REQUIRE"` → `trust.certificates.bolt.clientAuth: Require`; copy `ssl.bolt.trustedCerts.sources` → `trust.certificates.bolt.trustedCerts.sources`.

**Cluster note:** inter-member mTLS does not use bolt/https `clientAuth` — the **cluster** policy defaults to `Require` with `trustedCerts` mounting peer/member CAs (see Example 1 `certificates.cluster`).

---

### Option C — Single unified Secret for all policies

One `secretName` shared when SANs allow. Deferred — conflicts with independent FR variants TLS-01/02/03.

### Option D — cert-manager only; BYO deferred

Rejected — blocks corporate PKI and Helm migration.

---

## Comparison

| Criterion | A — full + revokedCerts | B — BYO + mTLS + cert-manager | C — unified |
|-----------|-------------------------|--------------------------------|-------------|
| Helm field names | ✅ | ✅ (`secretName`, `subPath`, `trustedCerts`) | ❌ |
| BYO split Secrets | ✅ | ✅ | ⚠️ |
| bolt/https mTLS | ✅ | ✅ (`clientAuth` + `trustedCerts`) | ⚠️ |
| cluster mTLS | ✅ | ✅ (default `Require`) | ⚠️ |
| cert-manager | ✅ | ✅ opt-in (`enabled: false` default) | ✅ |
| API size | ❌ large | ⚠️ medium | ✅ small |
| FR TLS-01/02/03 | ✅ | ✅ | ⚠️ |

---

## Decision

**We will implement Option B** — `spec.trust.certificates.{bolt,https,cluster}` mirrors Helm `ssl.{policy}` with `privateKey` / `publicCertificate` (`secretName`, `subPath`), plus **`clientAuth`** and **`trustedCerts.sources`** for mTLS. When `certManager.enabled: true`, per-policy `secretName` only (operator provisions `Certificate`). **`certManager.enabled` defaults `false`.**

Options A, C, and D are rejected or deferred. Option A residual (`revokedCerts`) → V1.1.

**V1 implementation scope:**

1. Webhook TLS-002 validates Secret existence; pairing enforced (both key + cert Secrets in BYO mode).
2. **Defaults:** `trust.enabled: false`, `certManager.enabled: false`, `reload.enabled: false`; per-policy `clientAuth` omitted → `None` (bolt/https), `Require` (cluster when enabled).
3. **Suggest `reload.enabled: true`** when using cert-manager or rotating BYO Secrets in-place.
4. **Do not use `secretRef`** — use `secretName` to match Helm and avoid confusion with `corev1.SecretReference`.
5. **mTLS in V1:** `clientAuth` + `trustedCerts` (Helm projected-volume shape); **`revokedCerts` deferred V1.1**.
6. Reserve TLS-related `neo4j.conf` keys (`dbms.ssl.policy.*`, `server.bolt.tls_level`, …) in BDR-008 denylist — including `*.client_auth`.

---

## Consequences

### Positive

- Near drop-in migration from Helm `ssl.*` — rename section to `trust`, keep `secretName`/`subPath`/`trustedCerts`.
- Split key/cert Secrets and custom `subPath` supported in V1 (corporate PKI).
- bolt/https mTLS first-class — no `config.dbms.ssl.policy.*.client_auth` escape hatch.
- cert-manager opt-in without forcing it as default.

### Negative

- Two shapes per policy (BYO vs cert-manager) — CEL `oneOf` required.
- Operator must implement cert-manager `Certificate` lifecycle when enabled.
- `trustedCerts.sources` projected-volume shape is verbose (Helm parity).

### Neutral

- `revokedCerts` still V1.1 — rare CRL use cases via `additionalMounts` until then.

---

## References

- `design/analysis/helm-fields/_index.csv` — `ssl.*` rows
- `helm-charts/neo4j/values.yaml` L448–483, `templates/_ssl.tpl`
- [`spec.md`](../../09-crd-spec/neo4j/spec.md) — `spec.trust`
- [`validation.md`](../../09-crd-spec/neo4j/validation.md) — TLS-001…006
- [BDR-007](007-service-exposure-connectivity.md), [BDR-008](008-neo4j-config-surface.md)
