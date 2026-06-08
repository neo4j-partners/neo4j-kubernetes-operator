# Migration Guide: Neo4j Kubernetes Operator

This guide helps you migrate from previous versions of the Neo4j Kubernetes Operator to the latest version with the new CRD structure.

## Overview of Changes

The Neo4j Kubernetes Operator now separates single-node and clustered deployments into two distinct CRDs:

- **`Neo4jEnterpriseCluster`**: For clustered deployments requiring high availability
- **`Neo4jEnterpriseStandalone`**: For single-node deployments in single mode

## ⚠️ Breaking Changes

### Cluster intra-node TLS now defaults to strict peer validation

**What changed.** The operator used to emit `dbms.ssl.policy.cluster.trust_all=true` and `client_auth=NONE` for TLS-enabled clusters. Neo4j's own documentation flags `trust_all=true` as *"debugging only, since it does not offer security."* The default now matches the canonical production posture: `trust_all=false` + `client_auth=REQUIRE` (mutual TLS) + `verify_hostname=true`, with the cert-manager Secret's `ca.crt` projected to `/ssl/trusted/ca.crt` as the trust anchor.

**Who is affected.** Existing `Neo4jEnterpriseCluster` resources with `spec.tls.mode=cert-manager`. On the next reconcile after the operator upgrade:
1. The StatefulSet template changes (new Secret projection + new config keys), triggering a rolling restart of the server pods.
2. Restarted pods run with the strict configuration. The rolling restart is safe because old pods (loose) still accept any cert presented by new pods, and new pods validate old pods' certs against the same CA — RAFT quorum survives the mixed-state window.

**Action required.** None for clusters whose cert-manager issuer populates `ca.crt` in the Secret it issues (CA, ACME, Vault, and most external issuers all do). The operator detects a missing `ca.crt` at reconcile time and refuses to apply the strict config — `status.phase` flips to `Failed` with a message naming the offending issuer.

**Opt-out.** Set `spec.tls.strictPeerValidation: false` on the cluster CR to keep the old behavior. The escape hatch is intended for installations whose external issuer doesn't populate `ca.crt`. Reverting to the legacy posture means accepting Neo4j's "debugging only, no security" warning.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
    strictPeerValidation: false   # legacy trust_all=true + client_auth=NONE
```

No effect on `Neo4jEnterpriseStandalone` (single-server, no intra-cluster TLS).


> **Single-node Cluster CRDs**: removed in v1.6-alpha. If you still have a `Neo4jEnterpriseCluster` with `topology.servers: 1`, migrate to `Neo4jEnterpriseStandalone` — same data via a backup/restore round-trip. For step-by-step alpha-era guidance, see the [v1.6-alpha migration section](#upgrading-to-v160-alpha-api-stabilization) below or the older versions of this doc in git history.


## Upgrading from v1.10.x to v1.11.x

This section covers the breaking and behavioural changes landing in v1.11.x (since `v1.10.0`).

### 1. Removed spec fields (CRD validation will reject manifests using them)

Four typed fields that were defined on the schema but were never wired through to Neo4j config have been removed. Manifests still using these fields will be rejected by CRD validation with `unknown field` errors:

| Removed field | Replacement |
|---|---|
| `Neo4jEnterpriseCluster.spec.auth.jwt` (also: `JWTAuthSpec`, `JWTValidationSpec` types) | Use the `oidc-<name>` typed providers under `spec.auth.oidc` — Neo4j ID tokens are JWTs, so OIDC covers the JWT use case end-to-end. |
| `Neo4jEnterpriseCluster.spec.ui` and `Neo4jEnterpriseStandalone.spec.ui` (`UISpec` type) | Neo4j Browser is bundled in the Enterprise image. Expose it via the existing `spec.service.ingress` block (or your own ingress / route). The typed `UISpec` block was a no-op. |
| `Neo4jEnterpriseCluster.spec.restoreFrom` and `Neo4jEnterpriseStandalone.spec.restoreFrom` (`RestoreSpec` inline type) | Use the `Neo4jRestore` CR. Apply the cluster/standalone first, wait for `status.phase=Ready`, then apply a `Neo4jRestore` referencing the backup. The migration-from-cluster-to-standalone example in the standalone API reference shows the canonical flow. |
| `Neo4jPlugin.spec.license` (`PluginLicense` type) | Mount license files via `spec.extraVolumes` + `spec.extraVolumeMounts` on the cluster/standalone CR, then reference the mount path from `spec.config` (e.g. `gds.enterprise.license_file: /licenses/gds.license`). |

**Action**: grep your manifests for these fields and migrate before upgrading the operator:

```bash
grep -rE 'spec:.*\b(jwt|ui|restoreFrom|license):' path/to/manifests/
```

### 2. `spec.auth.passwordPolicy` and `spec.auth.kerberos` are removed from the CRD

Both blocks were always schema-only — the operator never wired either through to Neo4j config. Both have been removed from the CRD entirely. Manifests still carrying `spec.auth.passwordPolicy` or `spec.auth.kerberos` will be rejected by the API server after the upgrade.

**Action for `passwordPolicy`**: set the equivalent Neo4j config keys directly in `spec.config`:

```yaml
spec:
  auth:
    adminSecret: neo4j-admin-secret
  config:
    dbms.security.auth_minimum_password_length: "12"
```

**Action for `kerberos`**: there is no drop-in `spec.config` replacement. Neo4j Kerberos requires the separate [Neo4j Kerberos Add-On](https://neo4j.com/docs/kerberos-add-on/current/) (a plugin JAR + its own `kerberos.conf` + `krb5.conf` files), and the operator does not assemble that bundle today. See the [Security Guide § Kerberos Authentication](security.md#kerberos-authentication) for the current state.

Also: the operator's earlier validator that required `dbms.security.kerberos.service_principal` and `dbms.security.kerberos.keytab` keys in `spec.config` whenever `kerberos` appeared in `authenticationProviders` has been removed. Those keys aren't actually part of Neo4j's Kerberos configuration surface (which lives in `kerberos.conf`, not `neo4j.conf`); the validator was enforcing a wrong model.

### 3. `neo4j_operator_cluster_replicas_total` metric: role label values renamed

The Prometheus gauge that exposes server counts had its `role` label values renamed:

| Before | After | Meaning |
|---|---|---|
| `role="primary"` | `role="desired"` | `spec.topology.servers` |
| `role="secondary"` | `role="ready"` | StatefulSet `readyReplicas` |

The original `primary` / `secondary` shape was inherited from a pre-server-architecture design. Neo4j 5.26+ uses a single `{cluster}-server` StatefulSet where roles are assigned at runtime via `serverModeConstraint`; the old labels were structurally meaningless and the call site was hardcoding `secondaries=0` regardless of cluster state. Per-server primary/secondary state is exposed separately via `neo4j_operator_server_health` (populated from `SHOW SERVERS` when monitoring is enabled).

**Action**: update PromQL queries / Grafana dashboards filtering on the old labels:

```promql
# Before
neo4j_operator_cluster_replicas_total{role="primary"}

# After
neo4j_operator_cluster_replicas_total{role="desired"}
```

### 4. Env-var removals from `spec.config` now actually take effect

Previously, removing a key from `spec.config` did not remove the corresponding `NEO4J_*` env var from the live StatefulSet — the cluster controller's `envVarsEqual` was a one-directional subset check that didn't detect "name dropped from desired". Pods continued running with the stale setting until something else triggered a template-replacing restart.

The fix tracks the cluster controller's owned env-var names in a `neo4j.com/cluster-controller-env-vars` annotation on the StatefulSet; on each reconcile, names previously owned but no longer in desired are dropped from the live env array, while foreign env vars (added by the plugin / fleet-management / Aura controllers) are preserved as before.

**Action**: this is generally the behaviour users expected. But if any cluster has been silently relying on a stale env var sticking around after the corresponding `spec.config` key was removed, that env var will disappear on the next reconcile after the upgrade — and then on the next pod restart, Neo4j will boot without that setting. Audit your `spec.config` entries before upgrading if your cluster has a long history of key edits.

**Behind the scenes**: the annotation is bootstrapped on the next reconcile after the upgrade — `previousOwned` is empty on first read, so no spurious removals happen. From that reconcile onward the set is tracked.

### 5. `Neo4jEnterpriseStandalone` now requires a headless Service (delete + recreate for in-place upgrades)

A backup against a `Neo4jEnterpriseStandalone` used to fail end-to-end because three things were broken: the backup Job built a cluster-shaped FQDN (`{name}-server-0...`) the standalone never had, the standalone's StatefulSet had no `spec.serviceName` so no DNS name resolved to the pod, and the standalone's `neo4j.conf` didn't enable `server.backup.listen_address` so port 6362 wasn't bound. The fix lands all three pieces:

- A new `{name}-headless` Service (`ClusterIP=None`, port 6362).
- `StatefulSet.spec.serviceName = {name}-headless`.
- `server.backup.enabled=true` + `server.backup.listen_address=0.0.0.0:6362` in the standalone ConfigMap.
- Backup controller branches on standalone vs cluster targets when picking the `--from` FQDN.

**Caveat**: `StatefulSet.spec.serviceName` is **immutable after creation**. Existing standalones upgraded in place will keep their old (empty) `serviceName` and will NOT get the headless service routing the backup Job depends on — backups against them will continue to fail with `Connection refused` on `:6362`.

**Action for existing standalones**: delete and recreate the `Neo4jEnterpriseStandalone` CR (PVC retention applies — `spec.storage.retentionPolicy=Retain` preserves the data PVC across the delete/recreate cycle, so the new StatefulSet picks up the same data volume). New deployments get the headless routing automatically with no extra steps.

```bash
# 1. (Optional but recommended) take a backup with a Neo4jBackup CR
#    targeting the standalone, OR cordon/quiesce application traffic.
# 2. Set retentionPolicy=Retain so the data volume survives the delete.
kubectl patch neo4jenterprisestandalone <name> --type=merge \
    -p '{"spec":{"storage":{"retentionPolicy":"Retain"}}}'

# 3. Delete the CR. The PVC stays because of Retain.
kubectl delete neo4jenterprisestandalone <name>

# 4. Re-apply the same manifest. The operator creates the new STS with
#    spec.serviceName=<name>-headless + the headless Service; the pod
#    attaches to the existing PVC.
kubectl apply -f <name>.yaml
```

Backups against the recreated standalone work end-to-end after step 4.

### 6. `spec.backups` and the backup sidecar are removed

The entire legacy centralized-backup architecture has been removed. The [`Neo4jBackup` CRD](../api_reference/neo4jbackup.md) (one Kubernetes Job per CR) is now the only backup path. Removed surfaces:

| Removed | Replacement |
|---|---|
| `Neo4jEnterpriseCluster.spec.backups` and `Neo4jEnterpriseStandalone.spec.backups` | A `Neo4jBackup` CR targeting the cluster/standalone. |
| `spec.storage.backupStorage` (per-cluster backup PVC) | `Neo4jBackup.spec.storage` (PVC or cloud); a PVC destination is auto-provisioned when needed. |
| The centralized `{cluster}-backup` / `{cluster}-backup-0` StatefulSet | Each `Neo4jBackup` CR spawns a short-lived Job — no long-running backup pod. |
| The standalone **backup sidecar** container (ran on every standalone pod) | Same `Neo4jBackup` CR path; standalones no longer carry a sidecar. |

Manifests still carrying `spec.backups` or `spec.storage.backupStorage` will be rejected by CRD validation with `unknown field` errors after the upgrade.

**Action:**

1. Grep your manifests and remove the legacy blocks:
   ```bash
   grep -rE '^\s*(backups|backupStorage):' path/to/manifests/
   ```
2. For each former `spec.backups` destination, create a `Neo4jBackup` CR. A daily scheduled cluster backup to S3:
   ```yaml
   apiVersion: neo4j.neo4j.com/v1beta1
   kind: Neo4jBackup
   metadata:
     name: daily-backup
   spec:
     target:
       kind: Cluster        # or Standalone
       name: my-cluster
     storage:
       type: s3
       bucket: neo4j-backups
       path: production/
     schedule: "0 2 * * *"  # omit for a one-shot backup
     retention:
       maxAge: "30d"
       maxCount: 30
   ```
   For Workload Identity (IRSA / GKE WI / Azure WI), set `spec.cloud.identity.autoCreate.annotations` on the `Neo4jBackup` — the operator annotates the auto-created `neo4j-backup-sa` ServiceAccount.
3. **Standalone pods will roll once** on upgrade as the sidecar container is dropped from the pod template (one-time restart; PVC data is untouched).

See the [Backup & Restore guide](guides/backup_restore.md) for scheduled backups, restore, sharded-database backups, and mixed-cadence FULL+DIFF chains.

### Quick upgrade checklist

1. Grep manifests for the removed fields (step 1) and migrate them.
2. If you set `spec.auth.passwordPolicy` or `spec.auth.kerberos` (both now removed) and were depending on them doing something, move the equivalent keys into `spec.config` (step 2).
3. Update PromQL / Grafana queries on `cluster_replicas_total` (step 3).
4. Audit `spec.config` if you have long-edit-history clusters that may have relied on the env-var-removal bug (step 4).
5. If you have existing `Neo4jEnterpriseStandalone` CRs AND want backups against them, delete + recreate with `retentionPolicy=Retain` per step 5 above. Standalones that never need backups can be left as-is.
6. Remove any `spec.backups` / `spec.storage.backupStorage` blocks and replace them with `Neo4jBackup` CRs (step 6). Expect a one-time standalone pod restart as the backup sidecar is dropped.

---

## Upgrading to v1.7.0-alpha (API Version Bump to v1beta1)

v1.7.0-alpha graduates the API from `v1alpha1` to `v1beta1`, signaling field stability. The API schema is unchanged — only the version identifier changes. Additionally, TLS bolt enforcement and standalone health probes are introduced.

### API version change

All manifests must update their `apiVersion` field:

```yaml
# Before (v1.6.0-alpha and earlier)
apiVersion: neo4j.neo4j.com/v1alpha1

# After (v1.7.0-alpha)
apiVersion: neo4j.neo4j.com/v1beta1
```

This applies to **all** operator CRDs:
- `Neo4jEnterpriseCluster`
- `Neo4jEnterpriseStandalone`
- `Neo4jDatabase`
- `Neo4jBackup`
- `Neo4jRestore`
- `Neo4jPlugin`
- `Neo4jShardedDatabase`

### Bolt TLS enforcement

When TLS is enabled (`tls.mode: cert-manager`), the operator now sets `server.bolt.tls_level=REQUIRED` on both cluster and standalone deployments. Previously this was `OPTIONAL`, meaning plain `bolt://` connections were silently accepted even with TLS configured.

**Action required** if you have clients connecting via plain `bolt://` to TLS-enabled deployments:
- Update connection strings from `bolt://` to `bolt+s://` (with CA verification) or `bolt+ssc://` (self-signed certs)
- Update `cypher-shell` commands to use `-a bolt+ssc://host:7687`
- Update application driver configurations to enable TLS

### Deprecated configuration key

`dbms.logs.query.enabled` is now flagged as deprecated by the config validator. Replace with `db.logs.query.enabled` in your `spec.config` sections.

### Standalone health probes

Standalone deployments now include readiness, liveness, and startup probes using `/conf/health.sh`. This means:
- Pods are no longer marked Ready until Neo4j is actually accepting connections
- The `status.phase` transition to `Ready` now reflects true Neo4j readiness
- Existing deployments will see a rolling update when the operator is upgraded (new probe spec on the StatefulSet)

### Quick upgrade checklist

1. Update all manifests: `apiVersion: neo4j.neo4j.com/v1alpha1` → `apiVersion: neo4j.neo4j.com/v1beta1`
2. Apply updated CRDs before deploying the new operator version:
   ```bash
   # If using Helm
   helm upgrade neo4j-operator ./charts/neo4j-operator --namespace neo4j-operator-system

   # If using make targets
   make install   # Updates CRDs
   make deploy-prod  # or deploy-dev
   ```
3. If TLS is enabled, update client connection strings from `bolt://` to `bolt+s://` or `bolt+ssc://`
4. Replace `dbms.logs.query.enabled` with `db.logs.query.enabled` in `spec.config`
5. Expect a one-time rolling restart of standalone pods (new health probes added to StatefulSet)

### Batch update with sed

```bash
# Update all YAML manifests in your deployment directory
find /path/to/manifests -name '*.yaml' -exec \
  sed -i 's|neo4j.neo4j.com/v1alpha1|neo4j.neo4j.com/v1beta1|g' {} +

# Update deprecated config key
find /path/to/manifests -name '*.yaml' -exec \
  sed -i 's|dbms.logs.query.enabled|db.logs.query.enabled|g' {} +
```

---

## Upgrading to v1.6.0-alpha (API Stabilization)

v1.6.0-alpha included breaking changes to the `v1alpha1` API to stabilize fields ahead of the v1beta1 graduation (completed in v1.7.0-alpha). These changes fixed field naming inconsistencies, removed deprecated fields, and resolved bugs.

### Field renames

| Resource | Old Field | New Field | Action |
|----------|-----------|-----------|--------|
| `Neo4jRestore` | `spec.targetCluster` | `spec.clusterRef` | Rename in YAML |
| Auth TrustStore | `spec.auth.trustStore.secretRef` | `spec.auth.trustStore.name` | Rename in YAML |
| Kerberos Keytab | `spec.auth.kerberos.keytab.secretRef` | `spec.auth.kerberos.keytab.name` | Rename in YAML |

Example — before:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
spec:
  targetCluster: my-cluster
  databaseName: neo4j
  source:
    type: backup
    backupRef: daily-backup
```

After:
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
spec:
  clusterRef: my-cluster
  databaseName: neo4j
  source:
    type: backup
    backupRef: daily-backup
```

### Removed fields

| Field | Replacement |
|-------|-------------|
| `spec.auth.provider` | `spec.auth.authenticationProviders` (list) |
| `spec.auth.secretRef` | Use provider-specific typed configs (`spec.auth.ldap`, `spec.auth.oidc`, etc.) |
| `spec.persistence` (standalone) | `spec.storage.retentionPolicy` (already present on StorageSpec) |
| `spec.route` (standalone top-level) | `spec.service.route` (matching cluster pattern) |

Example — migrating deprecated auth fields:

Before:
```yaml
spec:
  auth:
    provider: ldap
    secretRef: ldap-config
```

After:
```yaml
spec:
  auth:
    authenticationProviders: ["ldap"]
    authorizationProviders: ["ldap"]
    ldap:
      host: "ldap://ldap.example.com:389"
      authentication:
        userDNTemplate: "uid={0},ou=users,dc=example,dc=com"
```

Example — migrating standalone persistence:

Before:
```yaml
spec:
  storage:
    className: standard
    size: "10Gi"
  persistence:
    enabled: true
    retentionPolicy: Delete
    accessModes: ["ReadWriteOnce"]
```

After:
```yaml
spec:
  storage:
    className: standard
    size: "10Gi"
    retentionPolicy: Delete
```

Example — migrating standalone route:

Before:
```yaml
spec:
  route:
    enabled: true
    host: neo4j.apps.example.com
```

After:
```yaml
spec:
  service:
    type: ClusterIP
    route:
      enabled: true
      host: neo4j.apps.example.com
```

### Unified secret reference type

`TrustStoreSpec`, `AuraTokenSecretRef`, and `KerberosKeytabSpec` have been replaced by a single `SecretKeyRef` type with `name` and `key` fields. The JSON structure for `AuraFleetManagement.tokenSecretRef` is unchanged (fields were already `name`/`key`). For `trustStore` and `kerberos.keytab`, the `secretRef` field is now `name`.

### Quick upgrade checklist

1. Search your manifests for `targetCluster:` and replace with `clusterRef:`
2. Search for `auth.provider:` / `auth.secretRef:` and migrate to `authenticationProviders`/`authorizationProviders` with typed provider configs
3. Search standalone manifests for `spec.route:` and move to `spec.service.route:`
4. Search standalone manifests for `spec.persistence:` and move retention policy to `spec.storage.retentionPolicy:`
5. Search for `trustStore.secretRef:` and rename to `trustStore.name:`
6. Search for `kerberos.keytab.secretRef:` and rename to `kerberos.keytab.name:`
7. Apply updated CRDs before deploying the new operator version

## What's Next

After completing your migration:

1. **Update monitoring** dashboards and alerts
2. **Update documentation** and runbooks
3. **Train your team** on the new CRD structure
4. **Consider proper resource configuration** for cluster deployments
5. **Implement proper backup** strategies for your deployment type

For more information, see:
- [Neo4jEnterpriseCluster API Reference](../api_reference/neo4jenterprisecluster.md)
- [Neo4jEnterpriseStandalone API Reference](../api_reference/neo4jenterprisestandalone.md)
- [Getting Started Guide](getting_started.md)
- [Troubleshooting Guide](guides/troubleshooting.md)
