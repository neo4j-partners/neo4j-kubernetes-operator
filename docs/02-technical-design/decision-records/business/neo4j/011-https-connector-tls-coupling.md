# BDR-011 — HTTPS connector, Service exposure & TLS (incl. mTLS)

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-22 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-007](006-service-exposure-connectivity.md) — `connectivity.listeners` + `connectivity.service` (accepted) · [BDR-006](007-tls-trust-model.md) — `spec.trust` (accepted) |
| **Constraints** | `NEO-2-005`, `NEO-3-005-TLS-01..04`; AC `AC-NEO-TLS`; [Neo4j SSL framework](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) |

---

## Context

[BDR-007](006-service-exposure-connectivity.md) splits **process** (`connectivity.listeners`) from **Kubernetes routing** (`connectivity.service.expose` + optional `connectivity.service.ports`). [BDR-006](007-tls-trust-model.md) owns **certificate material and mTLS** (`spec.trust.certificates.{bolt,https,cluster}`).

Helm conflates these: setting `ssl.https.privateKey` both mounts certs **and** injects `server.https.enabled: true` (`neo4j-config.yaml`). The operator separates them — which raises coupling questions:

- Can I expose HTTPS on the LoadBalancer without mTLS?
- Does `connectivity.service.expose` include `https` imply `trust`?
- Where does **client certificate authentication** (mTLS) live — `trust` or `connectivity`?
- How does **Ingress** (`connectivity.ingress`) interact with HTTPS on the Service vs TLS termination at Ingress?

### Three orthogonal layers

| Layer | Field(s) | Question |
|-------|----------|----------|
| **1 — TLS policy** | `spec.trust` | Server certs, trust store, **mTLS** (`clientAuth`, `trustedCerts`) per SSL policy |
| **2 — Neo4j listen port** | `spec.connectivity.listeners.https` | Does Neo4j **listen** for HTTPS inside the pod? |
| **3 — K8s Service** | `spec.connectivity.service.expose` + `ports.https` | Is HTTPS **routed** on the client Service? |

**mTLS is layer 1 only** — `trust.certificates.https.clientAuth` (+ `trustedCerts`). It is **not** a Service or Ingress knob.

Bolt follows the same pattern with `trust.certificates.bolt` + `connectivity.listeners.bolt` + `service.expose` / `service.ports.bolt`.

---

## Analysis

### Helm behaviour (reference)

| Helm | Effect |
|------|--------|
| `ssl.https.privateKey` set | Mount certs; `server.https.enabled: true` |
| `ssl.https` absent | HTTPS connector off |
| `services.neo4j.ports.https.enabled` | Service port to `targetPort` (TLS still terminated **in Neo4j**) |
| `config.dbms.ssl.policy.https.client_auth` | mTLS (ad-hoc via `config` today) |
| Ingress | Chart does not own Ingress; operator adds `connectivity.ingress` |

### Proposed coupling rules (V1)

```
trust.certificates.https (material + clientAuth)
        │
        ▼ (required)
connectivity.listeners.https   (port set)
        │
        ▼ (optional)
connectivity.service.expose includes https   [optional — in-cluster only OK]
```

| Rule ID | CEL / validation | Rationale |
|---------|------------------|-----------|
| TLS-LISTENER-001 | `connectivity.listeners.https` set ⇒ `trust.enabled` + https cert material (BYO `secretName` or cert-manager) | HTTPS connector without certs is invalid |
| TLS-LISTENER-002 | `https` ∈ `service.expose` ⇒ `connectivity.listeners.https` set | Cannot route to disabled connector ([NET-002](../../09-crd-spec/neo4j/validation.md)) |
| TLS-LISTENER-003 | `connectivity.listeners.https` set ⇏ `https` ∈ `service.expose` | In-cluster HTTPS without LB port is valid |
| TLS-LISTENER-004 | mTLS only via `trust.certificates.https.clientAuth` + `trustedCerts` | Parity with bolt mTLS ([BDR-006](007-tls-trust-model.md)); not on `connectivity` |
| TLS-LISTENER-005 | `clientAuth: Require` on https ⇒ `trustedCerts.sources` non-empty | Same as bolt TLS-004 |
| TLS-LISTENER-006 | HTTP and HTTPS independent on Service | `expose` may list both `http` and `https`; cleartext HTTP does not disable HTTPS |

### mTLS scenarios

| Scenario | `trust` | `connectivity.listeners.https` | `service.expose` |
|----------|---------|-------------------------------|------------------|
| HTTPS server TLS only (browser) | `clientAuth: None` (default) | `7473` | includes `https` |
| HTTPS + corporate client certs | `clientAuth: Require`, `trustedCerts` | `7473` | includes `https` |
| TLS in-cluster, no external LB | certs + `clientAuth` as needed | `7473` | omits `https` (ClusterIP / internals) |
| Bolt mTLS, HTTP cleartext | `certificates.bolt.clientAuth: Require` | omitted / `null` | `bolt`, `http` |

**Cluster inter-member mTLS** stays on `trust.certificates.cluster` — unrelated to `service.expose`.

### Ingress vs Service HTTPS vs reverse proxy

| Pattern | `ingress` | `reverseProxy` | `service.expose` | `trust` |
|---------|-----------|----------------|------------------|---------|
| **A — Neo4j terminates TLS** (LB → pod HTTPS) | optional SANs | off | `https` | https certs on Neo4j |
| **B — Ingress terminates TLS** (browser) | `rules` → `backend: reverseProxy` | `expose: [http]` | `bolt` only (typical) | Ingress TLS Secret; Neo4j HTTP listener |
| **C — Direct Ingress → Neo4j Service** | `backend: service` | off | `http` / `https` | per BDR-006 |
| **D — TLS pass-through Ingress** | TCP/SNI to Service | optional | `https` | same as A |

V1 MVP: none of the above (ClusterIP + HTTP + Bolt only). **V1.1** documents **pattern B** (Helm `neo4j-reverse-proxy` parity) per [BDR-007](006-service-exposure-connectivity.md) Amendment F.

### Options

#### Option A — Strict chain (recommended)

Enforce TLS-LISTENER-001..006 as CEL. mTLS exclusively under `trust`.

#### Option B — Implicit https connector when trust.https certs set

Operator sets `connectivity.listeners.https` when https certificates are configured, even if user omitted the field.

| Advantages | Disadvantages |
|------------|---------------|
| Helm-like convenience | Hides explicit connector intent; surprises when removing certs |

#### Option C — `connectivity.listeners.https.tls` sub-block

Add TLS/mTLS fields under connectivity (rejected — duplicates `trust`, violates BDR-006).

**Proposer direction:** **Option A** — explicit `connectivity.listeners.https` port; strict validation chain; mTLS only in `trust`.

---

## Decision

**We will adopt Option A** — explicit `connectivity.listeners.https` port; strict validation chain (TLS-LISTENER-001..006); mTLS only in `spec.trust`.

1. Document the **three-layer diagram** in `spec.md` (`connectivity.listeners` § cross-link to `trust`).
2. Extend `validation.md` with TLS-LISTENER-* rules alongside existing NET-003.
3. Defer **Ingress-terminated TLS** (pattern B) to a future BDR after V1 ingress shape stabilises.

---

## Example — HTTPS on LB, server TLS only (no mTLS)

```yaml
spec:
  trust:
    enabled: true
    certificates:
      https:
        privateKey:
          secretName: neo4j-https-key
        publicCertificate:
          secretName: neo4j-https-cert
        clientAuth: None
  connectivity:
    listeners:
      https: 7473
    service:
      type: LoadBalancer
      expose:
        - https
      ports:
        https: 443
```

## Example — HTTPS + mTLS on same connector

```yaml
spec:
  trust:
    enabled: true
    certificates:
      https:
        privateKey:
          secretName: neo4j-https-key
        publicCertificate:
          secretName: neo4j-https-cert
        clientAuth: Require
        trustedCerts:
          sources:
            - secret:
                name: corporate-client-ca
                items:
                  - key: ca.crt
                    path: corporate-ca.crt
  connectivity:
    listeners:
      https: 7473
    service:
      expose:
        - https
```

mTLS is configured entirely under `trust.certificates.https` — `connectivity.service` forwards TCP to `connectivity.listeners.https`.

---

## Consequences

### Positive

- Clear separation: **trust** = crypto; **`connectivity.listeners`** = process; **`connectivity.service`** = K8s routing.
- mTLS parity between bolt and https policies.
- Avoids a fourth TLS vocabulary on the Service.

### Negative

- Helm users must set both `ssl.https` (→ `trust`) **and** `connectivity.listeners.https` (explicit vs Helm auto-enable).

### Neutral

- `NET-003` in validation.md should be split/refined into TLS-LISTENER-001 (connector) and TLS-LISTENER-002 (service).

---

## References

- [BDR-006](007-tls-trust-model.md) — TLS trust, mTLS, cert-manager
- [BDR-007](006-service-exposure-connectivity.md) — `connectivity.listeners` + `connectivity.service`
- `design/09-crd-spec/neo4j/spec.md` — `spec.trust`, `spec.connectivity`
- `design/09-crd-spec/neo4j/validation.md` — NET-003, TLS rules
- Helm: `neo4j-config.yaml` (https enabled), `_ssl.tpl`, `neo4j-loadbalancer.yaml`
