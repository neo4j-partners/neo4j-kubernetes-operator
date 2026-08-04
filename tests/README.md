# E2E tests (Gate 2 — ADR-012)

End-to-end conformance tests on a real Kubernetes cluster (operator install, Neo4j deploy,
operand assertions). Unit tests remain under `src/` (`make test`).

| Doc | Read it for |
|-----|-------------|
| [coverage.md](coverage.md) | What each suite asserts today, mapped to `NEO-*` / `OP-*` requirements and AC groups — with a done / not-done checklist |
| [design.md](design.md) | How the harness is built: layout, suite/pipeline/action model, storage & connectivity mechanics, CI structure |
| [contribute.md](contribute.md) | How to run suites locally (kind / Azure), configuration profiles, and how to add tests |

Quick start:

```bash
bash tests/bin/setup-local-kind.sh
make test-e2e-local
```