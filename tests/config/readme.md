# E2E configuration — layered reconciliation

Each domain has its own **base** + **classic cases**. A **profile** picks one case per layer (or runs the full matrix) and merges with a **cloud** profile into the final runtime env.

## Layout

```
tests/config/
  load.sh                 # sources reconcile.sh
  reconcile.sh            # merge profile + cases + cloud
  derive.sh               # derived Neo4j resource names

  operator/
    base.yaml / base.sh   # CRD, Deployment, manifest paths
    cases/
      default.sh          # classic: standard install
      local-image.sh      # classic: kind pre-loaded image (Never pull)
      registry-image.sh   # classic: pull from ACR/registry

  neo4j/
    base.yaml / base.sh   # apiVersion, edition, fixture path
    cases/
      standalone-minimal.sh
      standalone-storage-class.sh
      standalone-named-cr.sh

  cloud/
    local-kind.sh
    azure-aks.sh

  profiles/
    happy-path.sh         # fixed picks per layer
```

## Reconciliation

```
final config = operator/base
             + operator/cases/<operator-case>
             + neo4j/base
             + neo4j/cases/<neo4j-case>
             + cloud/<cloud>
             + derive (STS name, secrets, …)
```

### Profiles

| Profile | Behaviour |
|---------|-----------|
| `happy-path` (default) | Fixed: `neo4j/standalone-minimal` + operator case from cloud (`local-image` on kind, `registry-image` on AKS) |
| `matrix` | All valid operator × neo4j combinations for the cloud and scenario (cleanup between each run) |
| `explicit` | You set `OPERATOR_CASE` and `NEO4J_CASE` env vars |

### Matrix per cloud

| Cloud | Operator cases | Neo4j cases (`p0-standalone`) | Total |
|-------|----------------|----------------------------------|-------|
| `local-kind` | `default`, `local-image` | `standalone-minimal`, `standalone-storage-class`, `standalone-named-cr` | 6 |
| `azure-aks` | `default`, `registry-image` | same | 6 |

## Usage

```bash
# Happy path (default) — CI
make test-e2e-local

# All classic combinations (local-kind: 6 runs)
E2E_PROFILE=matrix make test-e2e-local

# Preview matrix without running tests
make test-e2e-combinations
# CLOUD=azure-aks make test-e2e-combinations

# Single explicit combination
E2E_PROFILE=explicit OPERATOR_CASE=local-image NEO4J_CASE=standalone-storage-class \
  CLOUD=local-kind ./tests/bin/run-e2e.sh
```

Log line example:

```
E2E profile=happy-path cloud=local-kind operator=local-image neo4j=standalone-minimal cr=dev scenario=p0-standalone
```

## Operator vs tests/old

This operator uses `neo4js.neo4j.com` / `Neo4j` (`neo4j.com/v1beta1`). Do not reuse `tests/old` CR kinds (`Neo4jEnterpriseStandalone`, etc.).
