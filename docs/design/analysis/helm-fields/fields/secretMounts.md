# `secretMounts`

## Client need

Mount arbitrary Kubernetes Secrets as files into the Neo4j pod — e.g. S3 credentials for **seedURI** restore from private buckets, cloud storage keys, custom trust material not covered by `spec.trust`.

## Neo4j documentation

- [Restore](https://neo4j.com/docs/operations-manual/current/backup-restore/restore-backup/) — seed / restore
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `_secretMounts.tpl` — iterates map; creates paired `volumes[]` + `volumeMounts[]` on StatefulSet
- **Go model**: `release_values.go` — `SecretMounts map[string]SecretMount`
- **K8s resources**: Secret volumes with optional `items` projection and `defaultMode`
- **Neo4j mechanism**: Files at `mountPath` for `neo4j-admin` seed/restore or custom integrations — **not** wired automatically to `config`.

**Map key** (e.g. `s3-credentials`) becomes volume **name** in the pod.

Example Helm shape:

```yaml
secretMounts:
  s3-credentials:
    secretName: my-s3-secret
    mountPath: /var/secrets/s3
    items:
      - key: access-key
        path: access-key
    defaultMode: 0600
```

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE-ESCAPE | ad-hoc secret file mounts | `additionalVolumes`, `trust.certificates`, `auth`, `pluginDefinitions` |

**Not the same as:**

| Mechanism | CRD section | Purpose |
|-----------|-------------|---------|
| `ssl.*` | `spec.trust` | TLS policies (bolt/https/cluster) |
| `neo4j.passwordFromSecret` | `spec.auth` | DB auth password |
| `apoc_credentials` | `spec.pluginDefinitions` | APOC credentials |
| `Neo4jRestore.spec.source.credentials` | Restore CRD | Job-scoped restore creds (V1) |

## CRD mapping

- **Target**: `Neo4j.spec.storage.secretMounts` (map, Helm-shaped; under `storage` per BDR-005 Option E layout on the CR)
- **Notes**: [BDR-005](../../../decision-records/business/neo4j/005-storage-volume-mode.md) § Escape hatches. Overlap with `Neo4jRestore` credentials — both may be needed (persistent pod mount vs one-shot Job).
- **NEO-005:** `items` required; Secret must be labeled `neo4j.com/mountable-by-operator: "true"`. See [examples/secrets/README.md](../../../../../examples/secrets/README.md).

## Aggregation

- **Group**: AGG-STORAGE-ESCAPE
- **Must decide with**: `additionalVolumes`, `additionalVolumeMounts`

## Versioning

- **Classification**: safe
- **Rationale**: Additive secret mounts.

## FR / AC

- FR: NEO-2-004; NEO-2-014; NEO-3-004-SEC-01; NEO-3-014-RSTM-02 (seed URI)
- AC: AC-NEO-SECRETS; AC-NEO-RESTORE; AC-NEO-RESTORE-SEEDURI

## Open questions

- ~~Webhook: reject `mountPath` under `/var/lib/neo4j/certificates/` (reserved for trust)?~~ Done (STO-009).
- V1: allow `secret` volume type in `additionalMounts` or force secrets through `secretMounts` only?
- ~~Opt-in label / require items?~~ Done (NEO-005 / STO-011 / STO-012).
