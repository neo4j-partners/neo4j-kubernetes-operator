# Neo4j 4.x Deprecated Settings Audit Report

## Summary
This audit identifies all references to deprecated Neo4j 4.x settings in the codebase that should be removed or updated for Neo4j 5.26+ compatibility.

## Findings

### 1. `dbms.mode=SINGLE` (Deprecated in Neo4j 5.x+)

**Critical Issues Found:**

1. **Documentation Files:**
   - `/docs/developer_guide/architecture.md:38` - "Automatically sets `dbms.mode=SINGLE` for standalone deployments"
   - `/docs/developer_guide/architecture.md:64` - "Configuration: Automatically sets `dbms.mode=SINGLE`"
   - `/docs/api_reference/neo4jenterprisestandalone.md:83` - "The operator automatically sets `dbms.mode=SINGLE`"
   - `/docs/user_guide/migration_guide.md:351` - "`dbms.mode=SINGLE` is automatically set"
   - `/docs/user_guide/guides/troubleshooting.md:274` - "# dbms.mode=SINGLE is automatically set"

2. **Test Files:**
   - `/test/integration/standalone_deployment_test.go:112-113` - Tests check that `dbms.mode=SINGLE` should NOT be present (correct behavior)
   - `/test/integration/standalone_deployment_test.go:275-276` - Tests check that `dbms.mode=SINGLE` should NOT be present (correct behavior)
   - `/internal/controller/neo4jenterprisestandalone_controller_test.go:130-137` - Tests expect `dbms.mode=SINGLE` to be set (incorrect expectation)
   - `/internal/controller/neo4jenterprisestandalone_controller_test.go:218` - Tests expect `dbms.mode=SINGLE` to be set (incorrect expectation)
   - `/internal/controller/neo4jenterprisestandalone_controller_test.go:253` - Tests expect `dbms.mode=SINGLE` to be set (incorrect expectation)

3. **Script Files:**
   - `/scripts/demo.sh:397` - Comments mention "dbms.mode=SINGLE"
   - `/scripts/README-demo.md:35` - Lists "Single-node mode (`dbms.mode=SINGLE`)"

4. **Implementation:**
   - The actual controller implementation (`neo4jenterprisestandalone_controller.go`) does NOT set `dbms.mode=SINGLE` (which is correct for Neo4j 5.26+)
   - However, tests and documentation incorrectly state that it does

### 2. `causal_clustering.*` Settings (Replaced with `dbms.cluster.*`)

**Found:**
- `/docs/user_guide/clustering.md:343` - Documentation mentions updating from `causal_clustering.*` to `dbms.cluster.*` (informational reference, acceptable)

### 3. `dbms.cluster.discovery.endpoints` (Deprecated in 5.23)

**Found:**
- `/internal/validation/standalone_validator.go:267` - Listed as a forbidden configuration for standalone deployments (correct usage - preventing deprecated settings)

### 4. Discovery V1 / V1_ONLY References

**Found:**
- `/internal/validation/config_validator_test.go:98` - Test case for rejecting "V1_ONLY" (correct - testing rejection of deprecated value)
- `/internal/validation/config_validator_test.go:148` - Test case name "invalid V1_ONLY version" (correct - testing rejection)

### 5. Other Deprecated Settings

**Not Found:**
- `dbms.logs.debug.*` - No references found ✓
- `metrics.bolt.*` - No references found ✓
- `server.groups` - No references found ✓

## Recommendations

### High Priority Fixes:

1. **Update Documentation** - Remove all references to `dbms.mode=SINGLE`:
   - `docs/developer_guide/architecture.md`
   - `docs/api_reference/neo4jenterprisestandalone.md`
   - `docs/user_guide/migration_guide.md`
   - `docs/user_guide/guides/troubleshooting.md`

2. **Fix Controller Tests** - Update test expectations in:
   - `internal/controller/neo4jenterprisestandalone_controller_test.go` - Tests should NOT expect `dbms.mode=SINGLE`

3. **Update Scripts** - Remove references to deprecated mode:
   - `scripts/demo.sh`
   - `scripts/README-demo.md`

### Low Priority (Informational):

1. The validation logic correctly rejects deprecated settings
2. The actual controller implementation correctly does NOT use `dbms.mode=SINGLE`
3. Some integration tests correctly verify that `dbms.mode=SINGLE` is NOT present

## Notes

According to the CLAUDE.md documentation, Neo4j 5.26+ uses a unified clustering approach where all deployments use clustering infrastructure, even single-node deployments. The `dbms.mode=SINGLE` setting is deprecated and should never be used. The operator should use `internal.dbms.single_raft_enabled=true` for all deployments instead.
