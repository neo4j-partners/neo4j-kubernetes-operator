# Neo4j Kubernetes Operator Comprehensive Audit Report

Date: 2026-01-19

## Executive Summary

This report updates the comprehensive audit of the Neo4j Kubernetes Operator with a focus on current architecture, validation, security posture, and operational readiness for Neo4j Enterprise 5.26+ and 2025.x. The operator now demonstrates strong version enforcement, improved validation for plugins and backups, dynamic memory sizing for clusters, and consistent manager pod security defaults across deployment paths. The primary gaps remain in security hardening (RBAC scope, data-plane pod hardening, network policies) and supply-chain controls for plugins/seed/backup inputs.

## Scope and Sources

Scope:
- CRDs and validation logic in `api/v1alpha1` and `internal/validation`
- Resource builders in `internal/resources`
- Controllers in `internal/controller`
- Security posture in `config/rbac` and deployment manifests

Sources reviewed:
- `reports/2025-11-20-security-review.md`
- `reports/2025-07-24-reconcile-loop-analysis.md`
- Current code in `internal/validation`, `internal/resources`, `internal/controller`

## Architecture Snapshot (Current)

- **Server-based cluster architecture**: Single `{cluster}-server` StatefulSet with `topology.servers` and self-organizing roles.
- **Standalone architecture**: Single `{standalone}` StatefulSet, fixed replica count (1).
- **Centralized backup**: `{cluster}-backup` StatefulSet for cluster backups; standalone uses sidecar.
- **Discovery**: V2_ONLY Kubernetes discovery for 5.26+/2025.x; service/endpoint discovery with required labels.
- **Safety controls**: Split-brain detection with automated repair; conflict-safe updates via retry on conflict.

## Compliance Assessment (Estimated)

Overall compliance score: **88/100**

Breakdown:
- **Version enforcement**: 95/100 (strong semver + calver checks)
- **Validation coverage**: 88/100 (plugins and backups now validated; some security validations remain)
- **Test coverage**: 95/100 (broad unit/integration coverage; targeted security tests still missing)
- **Documentation alignment**: 90/100 (recent pass improved alignment, but audits need refresh)
- **Security posture**: 74/100 (RBAC breadth, data-plane pod hardening, network controls still open)
- **Performance optimization**: 82/100 (dynamic memory sizing added; reconcile and cache tuning still areas)

## Notable Improvements Since 2025-07 Audit

1. **Plugin compatibility validation**
   - Compatibility matrix and version checks are enforced.
   - Source validation includes checksum requirement for URL-based plugins.

2. **Backup validation coverage**
   - Storage types and cloud configurations validated for 5.26+ flows.
   - Retention and scheduling validation added.

3. **Dynamic memory configuration**
   - Heap/page cache sizing derives from resource limits for clusters.
   - 5.26+ optimized allocations for larger memory profiles.

4. **Operator pod security defaults normalized**
   - Kustomize manager defaults now align with Helm defaults (non-root, seccomp, drop caps, read-only root FS).

## Key Findings

### Strengths

1. **Version enforcement is robust**
   - 5.26+ and 2025.x versions are validated consistently.

2. **Discovery configuration is correct for V2_ONLY**
   - Uses Kubernetes API discovery with `tcp-discovery` port mapping.

3. **Backup architecture is resource-efficient**
   - Centralized cluster backup reduces footprint versus per-pod sidecars.

4. **Conflict-safe writes and split-brain protections**
   - Retries on conflict and split-brain detection reduce operational risk.

5. **Cluster memory tuning is dynamic**
   - Memory sizing uses resource limits with safety constraints.

### Gaps and Risks

1. **Security hardening is incomplete**
   - Data-plane pods default to non-root with dropped caps and seccomp but still require writable root FS by default.
   - RBAC scope remains broad for typical deployments.

2. **Network controls are optional**
   - TLS can be disabled; no default NetworkPolicies.

3. **Supply-chain controls are partial**
   - URL plugins require checksum, but other plugin sources and seed/backup ingestion lack integrity checks.

4. **Operational tooling follow-ups**
   - Cache cleanup now checks for active Neo4j resources before eviction and is wired when selective cache is used (`internal/controller/cache_manager.go`, `cmd/main.go`).
   - Reconcile-loop analysis report has been updated for the server-based StatefulSet flow (`reports/2025-07-24-reconcile-loop-analysis.md`).

## Prioritized Recommendations

These recommendations remain valid after the latest manager pod security default alignment.

### P0 (Immediate)

1. **Harden pod security defaults**
   - Add secure default pod and container securityContext for Neo4j, backup, restore, plugin pods.

2. **Reduce RBAC blast radius**
   - Provide namespace-scoped defaults; split roles by controller; gate optional permissions.

3. **Supply-chain controls**
   - Add checksums or signatures for non-URL plugin sources and seed/backup inputs.

### P1 (Next Sprint)

1. **Network policy defaults**
   - Provide default NetworkPolicies and promote TLS-on guidance in examples and Helm values.

2. **Observability hardening**
   - Protect metrics endpoints with rbac-proxy or mTLS.

3. **Cache cleanup safety**
   - Add tests/metrics for cleanup decisions and validate behavior in selective cache mode.

### P2 (Backlog)

1. **Update reconciler analysis**
   - Keep reconcile-loop analysis aligned with future controller changes.

2. **Security validation enhancements**
   - Validate security config settings (password policies, TLS modes) with clear warnings.

3. **Expanded test coverage**
   - Add security regression tests for RBAC scope, plugin integrity, and TLS enforcement.

## Proposed Roadmap

- **Phase 1 (Security Hardening)**: Pod security defaults, RBAC split, plugin/seed integrity controls.
- **Phase 2 (Operational Safety)**: NetworkPolicies, metrics hardening, cache cleanup implementation.
- **Phase 3 (Documentation + Testing)**: Update audits, add security regression suite, refresh reconcile-loop report.

## Appendix: Key Files Reviewed

- `internal/validation/plugin_validator.go`
- `internal/validation/backup_validator.go`
- `internal/resources/memory_config.go`
- `internal/resources/cluster.go`
- `internal/controller/cache_manager.go`
- `config/rbac/*`
