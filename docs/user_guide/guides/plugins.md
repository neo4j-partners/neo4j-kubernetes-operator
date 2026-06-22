# Plugin Management

Install and manage Neo4j plugins (APOC, Graph Data Science, Bloom, GenAI, N10s, GraphQL, and custom JARs) declaratively via the `Neo4jPlugin` CRD. Works against both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` — the controller auto-detects the target from `spec.clusterRef`.

This guide is task-oriented. For the full field reference, see the [Neo4jPlugin API Reference](../../api_reference/neo4jplugin.md).

## Overview: choosing an install mode

The Neo4j Enterprise Docker entrypoint resolves the `NEO4J_PLUGINS` environment variable at pod startup. **APOC core is bundled in the image** — the entrypoint just copies the JAR from `/var/lib/neo4j/labs/` to `/plugins/`, no internet needed. **Every other plugin** (`graph-data-science`, `bloom`, `genai`, `n10s`, `graphql`, `apoc-extended`) is **downloaded from the internet on every pod start** when installed this way.

`spec.installMode` controls how the JAR reaches the pod:

| Mode | How the JAR arrives | Egress required | Artifact pinned? | When to use |
|------|--------------------|-----------------|------------------|-------------|
| `Managed` (default) | Operator adds the plugin to `NEO4J_PLUGINS`; the Neo4j entrypoint installs it at pod start | Yes — except APOC core (bundled in the image) | No — a restart can pull a different artifact | APOC anywhere; other plugins in dev/test where re-download on restart is acceptable |
| `PreBaked` | You build a custom image with the JAR in `/plugins/`; operator writes **only** the plugin's configuration, never touches `NEO4J_PLUGINS` | None | Yes — pinned, signed, scannable image | **Recommended for production** and air-gapped clusters |
| `VerifiedDownload` | Operator injects an init container that downloads `source.url`, verifies it against `source.checksum`, and writes the JAR to `/plugins` **before** Neo4j starts | Yes (https to your mirror or the vendor) | Yes — checksum enforced at download time | Production when you can't build custom images but must pin the exact artifact |

Regardless of mode, the operator always writes the plugin's required configuration (security allowlists, unrestricted procedures, plugin settings) and triggers a rolling restart of the target StatefulSet to apply changes.

> **Multi-controller coexistence**: the plugin controller merges plugin names into `NEO4J_PLUGINS` additively — it never overwrites entries added by other controllers (e.g. the Aura Fleet Management reconciler). Installing APOC via `Neo4jPlugin` alongside `spec.auraFleetManagement` on the cluster yields `["apoc","fleet-management"]`, and neither entry is removed on subsequent reconciles. Fleet Management itself is managed via the cluster/standalone CRD, not via `Neo4jPlugin`.

## Quick starts

### APOC (works offline)

APOC core is pre-bundled in the Neo4j Enterprise image, so this works with no internet egress:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jPlugin
metadata:
  name: apoc-plugin
spec:
  clusterRef: my-cluster        # Neo4jEnterpriseCluster OR Neo4jEnterpriseStandalone
  name: apoc
  version: "5.26.0"             # match your Neo4j version
  source:
    type: official
  config:
    # APOC settings become NEO4J_APOC_* environment variables (see "Plugin configuration")
    apoc.export.file.enabled: "true"
    apoc.import.file.enabled: "true"
    apoc.load.json.enabled: "true"
  security:
    allowedProcedures:
      - "apoc.load.*"
      - "apoc.export.*"
      - "apoc.import.*"
```

Wait for `status.phase: Ready`:

```bash
kubectl get neo4jplugin apoc-plugin -w
```

### Graph Data Science (GDS)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jPlugin
metadata:
  name: gds-plugin
spec:
  clusterRef: my-cluster
  name: graph-data-science
  version: "2.10.0"
  source:
    type: community
  config:
    # GDS Enterprise license (optional). Mount the license file at
    # /licenses/gds.license via the cluster/standalone spec.extraVolumes +
    # spec.extraVolumeMounts (e.g. from a Secret).
    gds.enterprise.license_file: "/licenses/gds.license"
  security:
    allowedProcedures:
      - "gds.*"
```

The operator automatically adds the security configuration GDS needs: `dbms.security.procedures.unrestricted=gds.*` and the matching procedure allowlist. With `Managed` mode the GDS JAR is downloaded by the Neo4j entrypoint at pod start — in production prefer `PreBaked` or `VerifiedDownload` (see [Supply-chain security](#supply-chain-security)).

### Bloom (commercial license required)

Bloom needs a commercial license. Mount the license file via the cluster/standalone CR's `spec.extraVolumes` + `spec.extraVolumeMounts` (e.g. from a Secret), then:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jPlugin
metadata:
  name: bloom-plugin
spec:
  clusterRef: my-cluster
  name: bloom
  version: "2.15.0"
  source:
    type: official
  config:
    dbms.bloom.license_file: "/licenses/bloom.license"
    # Optional: restrict Bloom access to specific roles
    dbms.bloom.authorization_role: "admin,architect"
```

Bloom's required settings are applied automatically — no manual security configuration needed:

- `dbms.security.procedures.unrestricted=bloom.*`
- `dbms.security.http_auth_allowlist=/,/browser.*,/bloom.*`
- `server.unmanaged_extension_classes=com.neo4j.bloom.server=/bloom`

After the rolling restart, Bloom is served at `http(s)://<neo4j-host>:<http-port>/bloom/`.

More examples (GenAI, standalone targets, multi-plugin setups): [examples/plugins/](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples/plugins).

## Custom and URL plugins

For plugins not in Neo4j's official/community catalogs, use `source.type: url` (direct download) or `source.type: custom` (private registry). Two rules are **enforced by the controller-side validator** — a CR that violates them goes to `status.phase: Invalid` and stops reconciling until you fix the spec:

### 1. The URL must be `https://`

`http://`, `file://`, and every other scheme is rejected. Plugin JARs are fetched over the network, and a non-https scheme has no transport integrity — a network attacker could swap the JAR in transit. If you host plugins internally, serve them from an https endpoint (an in-cluster mirror with a cert-manager certificate works).

### 2. A checksum is required

For `type: url` and `type: custom`, `source.checksum` is mandatory and must match:

- `sha256:` followed by exactly 64 hex characters, **or**
- `sha512:` followed by exactly 128 hex characters.

SHA1, MD5, and unprefixed hex are rejected — supply-chain protection demands a collision-resistant hash, and verification tooling should never have to guess the algorithm. Compute one locally:

```bash
shasum -a 256 my-plugin-2.0.0.jar
# abcd1234...  my-plugin-2.0.0.jar
# → checksum: "sha256:abcd1234..."
```

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jPlugin
metadata:
  name: url-plugin
spec:
  clusterRef: my-cluster
  name: my-custom-plugin
  version: "2.0.0"
  source:
    type: url
    url: "https://artifacts.example.com/plugins/my-plugin-2.0.0.jar"
    checksum: "sha256:abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234"
```

> **Important — when is the checksum enforced?** In the default `Managed` mode the upstream Neo4j Docker entrypoint does **not** verify this checksum at download time. The operator records it (and surfaces it on the StatefulSet) for audit and out-of-band verification. If you need enforcement at the moment of download, use `installMode: VerifiedDownload` (below) or pin the artifact with `PreBaked`.

### Private endpoints (`authSecret`)

For authenticated mirrors or registries, reference a Secret via `source.authSecret`. With `VerifiedDownload`, the Secret should carry either a `token` key (sent as `Authorization: Bearer <token>`) or a `header` key (used verbatim as the full `Authorization:` header value). Custom registries additionally support `source.registry` with its own `authSecret` and TLS configuration — see the [API reference](../../api_reference/neo4jplugin.md#pluginregistry).

## Supply-chain security

For a security reviewer, the question is: *what artifact runs in the pod, and who verified it?* The three modes answer it differently.

### `installMode: PreBaked` (recommended for production)

Build a custom image with the JAR copied in, reference it from the cluster/standalone CR's image spec, and set `installMode: PreBaked` on the `Neo4jPlugin`:

```dockerfile
FROM neo4j:2025.01.0-enterprise
COPY graph-data-science-2.13.0.jar /var/lib/neo4j/plugins/
```

The operator does **not** touch `NEO4J_PLUGINS` — no runtime fetch ever happens — but still writes the plugin's required configuration. You keep the declarative CRD UX while the artifact is pinned, signed, and scannable through your normal image pipeline. This is also the only fully offline option for non-APOC plugins.

### `installMode: VerifiedDownload`

When you can't bake images, `VerifiedDownload` closes the `Managed`-mode verification gap:

- **What is verified**: the SHA-256/SHA-512 of the downloaded JAR against `spec.source.checksum`.
- **When**: at pod startup, **before** Neo4j starts.
- **By what**: an operator-injected init container on the target StatefulSet's pod template. It downloads `spec.source.url`, computes the digest, compares, and writes the JAR to the shared `/plugins` emptyDir. `NEO4J_PLUGINS` is **not** mutated for this plugin, so the entrypoint's own (unverified) download path is bypassed entirely and cannot race the verified JAR.
- **On mismatch**: the init container exits non-zero and the pod stays `Pending`. `kubectl describe pod` shows the failure on the init container's `state.terminated.message`. Fix the URL or checksum; the next pod spawn re-runs the verification.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jPlugin
metadata:
  name: gds-verified
spec:
  clusterRef: my-cluster
  name: graph-data-science
  version: "2.13.0"
  installMode: VerifiedDownload
  source:
    type: url
    url: https://github.com/neo4j/graph-data-science/releases/download/2.13.0/neo4j-graph-data-science-2.13.0.jar
    checksum: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

The validator enforces additional gates for this mode:

- `source.url` and `source.checksum` are **mandatory**.
- `source.type` must be `url` or `custom` — `official`/`community` resolve via the Neo4j entrypoint's internal manifest and cannot be pointed at a verifiable URL.
- `spec.dependencies` are **rejected** — each dependency must be its own `Neo4jPlugin` CR with its own `source.url` + `checksum`, so the entire chain is verified.

**Internal CAs**: the owning cluster's (or standalone's) `spec.trustedCASecrets` certificates are mounted under `/etc/plugin-ca` and passed to the downloader, so an internal https mirror signed by your own CA works.

**Air-gapped clusters**: the init container image defaults to `curlimages/curl:8.5.0`, pulled from a public registry. Point it at an internal mirror via the operator Helm chart value:

```yaml
# values.yaml for the neo4j-operator chart
pluginInitContainer:
  image: registry.internal.example.com/mirror/curlimages/curl:8.5.0
```

Note that every pod restart re-runs the init container and re-downloads the JAR (≈ 30 MB for GDS) — budget bandwidth accordingly.

### Duplicate-CR protection (all modes)

Two `Neo4jPlugin` CRs in the same namespace targeting the same `clusterRef` with the same plugin `name` would race on the same `/plugins` directory and `NEO4J_PLUGINS` value. The reconciler refuses the duplicate: the **oldest CR wins**, the newer one is set to `status.phase: Failed` with a message naming the older CR, and a `PluginDuplicate` Warning event is emitted. Delete the unwanted CR; the survivor takes over on its next watch event.

## Plugin configuration

`spec.config` is a string map of plugin settings. Where each entry lands depends on the plugin:

- **APOC** (and APOC Extended): settings are applied as **environment variables**, not `neo4j.conf` — Neo4j 5.26+ no longer reads APOC settings from the config file. `apoc.export.file.enabled: "true"` becomes `NEO4J_APOC_EXPORT_FILE_ENABLED=true` on the StatefulSet.
- **GDS, Bloom, GenAI, and others**: settings flow through Neo4j configuration — the ConfigMap for standalone targets, runtime configuration for clusters.
- **Procedure allowlisting** (`dbms.security.procedures.unrestricted`, allowlists) always goes through `neo4j.conf`-level configuration, for APOC too — that's what `spec.security` drives.

Validation on `spec.config`: keys may contain only letters, digits, dots, underscores and dashes; values may not contain newline or carriage-return characters (they are rendered into config lines / env vars, so a stray newline could forge an extra setting).

Some plugins also receive **automatic security configuration** even with no `security` section — Bloom and GDS get their required unrestricted-procedures and allowlist entries applied by the operator; your own `security` settings override the automatic ones. See [Automatic Security Configuration](../../api_reference/neo4jplugin.md#automatic-security-configuration).

## Troubleshooting

### Validator rejections (`status.phase: Invalid`)

A spec that fails structural validation lands in `Invalid` with a `ValidationFailed` Warning event, and is **not requeued** — edit the CR to retry. Common causes:

| Symptom in `status.message` / event | Cause | Fix |
|---|---|---|
| `URL scheme must be https` | `source.url` uses `http://`, `file://`, etc. | Host the JAR on an https endpoint (in-cluster mirror with a cert-manager certificate works) |
| `checksum is required for url and custom source types` | Missing `source.checksum` | `shasum -a 256 <jar>` and set `checksum: "sha256:<digest>"` |
| `checksum must be of the form sha256:<64 hex chars> or sha512:<128 hex chars>` | SHA1/MD5, unprefixed hex, wrong length | Use a prefixed sha256/sha512 digest |
| `installMode: VerifiedDownload requires spec.source.url` / `...checksum` | VerifiedDownload without a source | Add `source.url` + `source.checksum` |
| `VerifiedDownload requires source.type=url or source.type=custom` | `official`/`community` with VerifiedDownload | Point at a downloadable URL, or switch mode |
| `VerifiedDownload does not support spec.dependencies` | Dependencies on a VerifiedDownload CR | Create each dependency as its own `Neo4jPlugin` CR |

Compatibility-matrix notes (unknown plugin name, version below the recorded minimum) are **advisory only** — they surface as `ValidationWarning` events and never block installation.

```bash
kubectl describe neo4jplugin <name>          # status.message + events
kubectl get events --field-selector involvedObject.name=<name>
```

### Verifying the plugin landed

For **clusters**, check the `{cluster-name}-server` StatefulSet's environment variables:

```bash
kubectl get statefulset my-cluster-server \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="neo4j")].env}' | jq .
# Expect NEO4J_PLUGINS to include your plugin, plus NEO4J_* config vars
```

For **standalone**, check the StatefulSet (named after the standalone CR) the same way, and the `{standalone-name}-config` ConfigMap for `neo4j.conf`-level plugin settings (security allowlists etc.):

```bash
kubectl get configmap my-standalone-config -o jsonpath='{.data.neo4j\.conf}' | grep -i <plugin>
```

Then confirm Neo4j actually loaded the procedures:

```bash
kubectl exec <pod-name> -c neo4j -- \
  cypher-shell -u neo4j -p <password> \
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc' RETURN name LIMIT 5"
```

### Events to look for

| Event reason | Type | Meaning |
|---|---|---|
| `PluginInstalled` | Normal | Plugin installed and verified |
| `PluginInstallFailed` | Warning | Installation failed — see the event message |
| `PluginDuplicate` | Warning | Another CR already owns this `(clusterRef, name)` pair — delete one |
| `ValidationFailed` | Warning | Spec rejected by the validator (hard error, no requeue) |
| `ValidationWarning` | Warning | Advisory compatibility note — does not block install |

### Other common issues

- **`status.phase: Waiting`**: the target cluster/standalone isn't functional yet — the plugin controller requeues until it is.
- **VerifiedDownload pod stuck `Pending`**: checksum mismatch or unreachable URL — `kubectl describe pod` and read the init container's termination message.
- **Plugin loads but procedures fail**: check `spec.security.allowedProcedures` and whether the plugin needs unrestricted procedures; for license-bound plugins (Bloom, GDS Enterprise, GenAI) verify the license file is mounted (`kubectl exec <pod> -c neo4j -- ls /licenses/`).

More general debugging recipes: [Troubleshooting Guide](troubleshooting.md).

## See also

- [Neo4jPlugin API Reference](../../api_reference/neo4jplugin.md) — every field, status phase, and the full supply-chain section
- [Plugin examples](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples/plugins) — APOC, GDS, Bloom, GenAI, cluster and standalone targets
- [Aura Fleet Management](../aura_fleet_management.md) — the fleet-management plugin is managed via the cluster CRD, not `Neo4jPlugin`
- [Configuration](../configuration.md) — cluster/standalone `spec.config`, `extraVolumes`, `extraVolumeMounts` for license files
