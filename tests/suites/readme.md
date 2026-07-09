# E2E suites — table-driven tests with shared setup

Suites run **setup once**, loop **cases** (run → assert → case_teardown), then **teardown once**.

## Layout

```
tests/
  pipelines/          reusable phase definitions
    standalone-suite.yaml
  suites/               test tables + pipeline overrides
    p0-standalone.yaml
    neo4j-admission.yaml
  fixtures/             CR manifests (valid and invalid)
```

## Pipeline phases

| Phase | Frequency | Example |
|-------|-----------|---------|
| `setup` | 1× | `operator/install` |
| `case_run` | per case | `deploy/standalone` |
| `case_assert` | per case | `assert/{{assert}}` |
| `case_teardown` | per case | `cleanup/standalone` |
| `teardown` | 1× | `cleanup/operator` |

## Suite example

```yaml
name: neo4j-admission
use_pipeline: standalone-suite
on_case_failure: continue

pipeline:
  case_assert:
    - assert/{{assert}}

cases:
  - id: no-license
    fixture: tests/fixtures/neo4j-no-license.yaml
    assert: admission-rejected
    expect: failure
    expect_contains: license
```

## Run

```bash
make test-e2e-local                              # p0-standalone (happy path)
E2E_PROFILE=matrix make test-e2e-local           # p0 matrix, setup once
./tests/bin/run-e2e.sh neo4j-admission           # admission suite
```

## Case fields

| Field | Description |
|-------|-------------|
| `id` | Case identifier (logs, diagnostics path) |
| `fixture` | Neo4j CR fixture path |
| `assert` | Assert action name (used in `{{assert}}` template) |
| `expect` | `success` (default) or `failure` for kubectl apply |
| `expect_contains` | Fragment expected in apply stderr (negative cases) |
| `cr_name` | CR metadata.name override |
| `from_reconcile` | Use `OPERATOR_CASE` / `NEO4J_CASE` from reconcile profile |
| `neo4j_case` / `operator_case` | Explicit config cases (matrix expansion) |

## Matrix

Set `expand_matrix.assert` in a suite and run with `E2E_PROFILE=matrix` — cases are generated from `reconcile_list_combinations` without reinstalling the operator between rows.
