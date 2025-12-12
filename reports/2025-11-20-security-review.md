# Neo4j Kubernetes Operator Security Review

## Scope and Context
- Repo: neo4j-kubernetes-operator (alpha; Kind-only dev/test).
- Components reviewed: CRDs (api/v1alpha1), controllers (internal/controller), validation, RBAC manifests (config/rbac), deployment manifests (config/manager, charts/), and backup/plugin/seed flows.
- Focus: in-cluster operator posture, RBAC surface, pod hardening, data-path protections (TLS, backups, plugins, seed URI), and supply-chain risk.

## Key Observations
- **RBAC breadth**: `config/rbac/role.yaml`/`role-consolidated.yaml` grant cluster-scoped create/delete on Secrets/ServiceAccounts/PVCs/StatefulSets, `pods/exec`, and ExternalSecrets CRDs even when not in use. Node read is allowed. Blast radius is high if operator is compromised.
- **Pod hardening gaps**: Neo4j/backup StatefulSets are generated without default pod/container securityContext (no runAsNonRoot, seccomp/AppArmor, capability drops, read-only root). Manager deployment has runAsNonRoot, but data-plane pods likely run as root.
- **Network/TLS optionality**: `spec.tls.mode` can be `disabled`; no default NetworkPolicies. Bolt/HTTP may be cleartext and reachable cluster-wide.
- **Plugin supply chain**: Plugin controller accepts arbitrary plugin names/versions/URLs and mixes env/neo4j.conf sources; no allowlist, checksum, or signature validation. Attackers with CR write could drop arbitrary jars.
- **Backup/seed ingestion**: Centralized backup pod processes request files from writable paths; seedURI imports external data. No integrity checks or access controls beyond namespace write permissions.
- **Metrics exposure**: Metrics auth roles exist, but mTLS/rbac-proxy enforcement is not evident; internal scrape endpoints may be accessible inside the cluster.
- **Secret handling**: Auth relies on Secrets, but no rotation guidance or automated safeguards beyond validation; operator has broad Secret access.

## Recommendations (prioritized)
1) **Reduce RBAC blast radius**
   - Default to namespace-scoped install; split roles per CRD if cluster-scope is unavoidable.
   - Drop unused verbs/resources (ExternalSecrets) behind explicit feature gates; remove `pods/exec` unless required for health checks.
   - Constrain node reads; prefer informer filters for owned namespaces.
2) **Harden pod security defaults**
   - Inject defaults in resource builders for Neo4j/backup/restore/plugin pods: `runAsNonRoot`, explicit UID/GID, `fsGroup`, read-only root FS where possible, drop ALL capabilities, seccomp/AppArmor profiles.
   - Expose securityContext in CRDs with validation, but keep safe defaults enforced when unset.
3) **Enforce transport and network controls**
   - Make TLS the default/required mode for production; keep discovery parameters in sync with 5.x/2025.x but avoid cleartext services.
   - Provide and recommend NetworkPolicies limiting client ingress to namespaces and restricting egress to needed endpoints (cert-manager/webhooks).
4) **Supply-chain gates for plugins/backups/seed**
   - Add plugin name/source allowlist and optional SHA256/signature fields; block arbitrary URLs by default.
   - Require trusted registries/imagePullSecrets; validate enterprise tags only.
   - For backup dropbox and seedURI, add checksum/signature validation and role-based access for writers separate from operator identity.
5) **Secure observability surfaces**
   - Front metrics with kube-rbac-proxy or enforce mTLS; restrict Service exposure to operator namespace.
   - Ensure liveness/readiness endpoints are not externally exposed.
6) **Secret management hygiene**
   - Document and automate admin credential rotation; ensure NEO4J_AUTH only sourced from Secrets.
   - Limit Secret access to operator namespace where feasible; annotate Secrets for ownership and reconciliation scope.
7) **Testing and automation**
   - Add conformance checks (e.g., kube-linter/Kyverno policies) for securityContext presence, TLS enabled, enterprise image tags, and NetworkPolicies.
   - Add e2e/security tests for plugin allowlist enforcement and backup request ACLs.

## Suggested Next Steps
- Implement RBAC split and optional ExternalSecrets role gating; ship a restricted namespace-scoped overlay.
- Add hardened pod security defaults in resource builders and validate via unit tests.
- Provide default NetworkPolicies and enforce TLS-on in samples/helm values; add warnings when disabled.
- Introduce plugin/seed/backup integrity controls (checksums/allowlists) and document operational procedures for secret rotation and backup request ACLs.
