# Aura Fleet Management Integration

[Neo4j Aura Fleet Management](https://neo4j.com/docs/aura/fleet-management/) lets you monitor all your Neo4j deployments — both Aura-managed and self-managed — from a single Aura console view. The operator integrates with Fleet Management natively: it installs the plugin automatically and handles token registration once your cluster is ready.

## How it works

The fleet-management plugin jar is pre-bundled in every Neo4j Enterprise image at `/var/lib/neo4j/products/`. When you enable Fleet Management in the operator:

1. The operator merges `"fleet-management"` into the existing `NEO4J_PLUGINS` list on the StatefulSet. This is additive — any plugins already installed via `Neo4jPlugin` CRDs are preserved. At pod startup, the Docker entrypoint copies the bundled jar to `/plugins/` — **no internet access required**.
2. The required procedure security settings (`dbms.security.procedures.unrestricted=fleetManagement.*` and `dbms.security.procedures.allowlist=fleetManagement.*`) are added to `neo4j.conf` automatically.
3. Once the cluster reaches `Ready` phase and the plugin is loaded, the operator reads the Aura registration token from a Kubernetes Secret and calls `CALL fleetManagement.registerToken($token)` via Bolt. This is idempotent — re-registering on reconcile loops is harmless.

### Two-phase reconciliation

Plugin installation (step 1) happens on every reconcile when `enabled: true`. Token registration (step 2–3) is gated on `status.phase == "Ready"` and skips if `status.auraFleetManagement.registered == true`. This ensures the pod restart triggered by the plugin load completes before registration is attempted.

### Plugin coexistence

Fleet Management works alongside any `Neo4jPlugin` CRDs you apply to the same deployment. For example, if APOC is installed via a `Neo4jPlugin` resource, the `NEO4J_PLUGINS` list on the StatefulSet will contain both:

```
["apoc","fleet-management"]
```

The operator uses an additive merge strategy — neither controller overwrites the other's entries.

## Prerequisites

- A Neo4j Aura account with Fleet Management enabled.
- A registration token from the Aura console wizard (see [Generate a token](#generate-a-token)).
- Neo4j Enterprise 5.26+ or 2025.x+. All supported enterprise images bundle the fleet-management plugin.

## Generate a token

In the Aura console:

1. Navigate to **Instances** → **Self-managed** → **Add deployment**
2. Select **Monitor deployment**
3. Follow the wizard — skip the "Install the plugin" step (the operator handles it)
4. Generate a token with your preferred expiry and note whether you want **auto-rotation** enabled (the plugin will renew it automatically before expiry)
5. Copy the token value

## Configure the operator

### Step 1 — Store the token in a Kubernetes Secret

```bash
kubectl create secret generic aura-fleet-token \
  --from-literal=token='<paste-token-here>' \
  -n <your-namespace>
```

### Step 2 — Enable Fleet Management in the cluster spec

Add the `auraFleetManagement` field to your `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  image:
    repo: neo4j
    tag: 2025.12-enterprise
  topology:
    servers: 3
  storage:
    size: 10Gi
  auth:
    adminSecret: neo4j-admin-secret

  auraFleetManagement:
    enabled: true
    tokenSecretRef:
      name: aura-fleet-token   # the Secret created above
      key: token               # defaults to "token" if omitted
```

The operator will:
- Merge `"fleet-management"` into `NEO4J_PLUGINS` on the next reconcile (causes a rolling pod restart to load the jar)
- Register the token once the cluster reaches `Ready` phase

### Verify registration

```bash
# Check status
kubectl get neo4jenterprisecluster my-cluster -o jsonpath='{.status.auraFleetManagement}'

# Example output:
# {"registered":true,"lastRegistrationTime":"2025-12-01T10:00:00Z","message":"Registered with Aura Fleet Management"}

# Check events
kubectl get events --field-selector reason=AuraFleetManagementRegistered
```

After a few minutes you should see the deployment appear in the Aura console under **Instances** → **Self-managed**.

## Standalone deployment

Works identically for `Neo4jEnterpriseStandalone`:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: my-standalone
spec:
  image:
    repo: neo4j
    tag: 5.26-enterprise
  storage:
    size: 10Gi
  auth:
    adminSecret: neo4j-admin-secret

  auraFleetManagement:
    enabled: true
    tokenSecretRef:
      name: aura-fleet-token
```

## Plugin only, no token (monitoring-ready mode)

You can enable the plugin without providing a token yet. The plugin will be installed and the security settings applied, but registration is deferred until a `tokenSecretRef` is added.

```yaml
auraFleetManagement:
  enabled: true
  # tokenSecretRef omitted — plugin installed, registration deferred
```

This is useful if you want to pre-install the plugin before setting up Aura access.

## Token rotation

If you enable auto-rotation in the Aura wizard, the plugin handles renewal automatically — no operator changes needed. If you rotate manually (generate a new token in Aura), update the Kubernetes Secret:

```bash
kubectl create secret generic aura-fleet-token \
  --from-literal=token='<new-token>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

The operator will detect that `status.auraFleetManagement.registered` is `true` and skip re-registration. To force re-registration with the new token, patch the cluster status:

```bash
kubectl patch neo4jenterprisecluster my-cluster \
  --type=merge --subresource=status \
  -p '{"status":{"auraFleetManagement":{"registered":false}}}'
```

The operator will then call `registerToken` with the new token on the next reconcile.

## Security

- The token Secret is read at registration time — it is never stored in the cluster spec or status.
- Fleet Management uses outbound-only connections from your deployment to Aura — no inbound ports are opened.
- The plugin does not read database data; it only reports metrics and topology. See [Fleet Management Data Transparency](https://neo4j.com/docs/aura/fleet-management/data/) for the full list of transmitted data.

## Status fields

| Field | Type | Description |
|---|---|---|
| `status.auraFleetManagement.registered` | bool | `true` once `registerToken` succeeded |
| `status.auraFleetManagement.lastRegistrationTime` | time | Timestamp of last successful registration |
| `status.auraFleetManagement.message` | string | Human-readable status or error message |

## Troubleshooting

**Registration not happening after enabling**

The operator only attempts registration when `status.phase == "Ready"`. Check that all pods are healthy:
```bash
kubectl get pods
kubectl describe neo4jenterprisecluster my-cluster
```

**`fleetManagement.registerToken` procedure not found**

The plugin jar may not have been copied yet. This happens if pods are still restarting after `NEO4J_PLUGINS` was updated. Wait for the rolling update to complete, or check:
```bash
kubectl exec my-cluster-server-0 -- cypher-shell -u neo4j -p <password> \
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'fleetManagement' RETURN name"
```

**`AuraFleetManagementFailed` event**

```bash
kubectl get events --field-selector reason=AuraFleetManagementFailed
```

Common causes:
- Token Secret not found in the correct namespace
- Incorrect key name in the Secret (default is `token`)
- Token has expired — generate a new one in the Aura console and update the Secret

**Other plugins disappear after enabling Fleet Management**

This should not happen with current operator versions (the additive merge strategy prevents it). If you observe it, verify the operator is up to date and check:

```bash
# Inspect the full NEO4J_PLUGINS value
kubectl get statefulset my-cluster-server \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NEO4J_PLUGINS")].value}'
# Should show all plugins, e.g. ["apoc","fleet-management"]
```

**Manually verify the plugin**

```bash
kubectl exec my-cluster-server-0 -- cypher-shell -u neo4j -p <password> \
  "CALL fleetManagement.status()"
```
