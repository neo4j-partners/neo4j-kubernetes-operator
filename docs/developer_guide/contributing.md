# Contributing

Contribution guidance lives in two canonical places — this page is a redirect
so the docs don't drift out of sync with the root guide.

## Start here

- **[CONTRIBUTING.md](../../CONTRIBUTING.md)** (repository root) — the single
  source of truth for the full contribution workflow: prerequisites, dev
  environment, the inner dev loop (manual / `dev-watch` / Tilt), testing tiers,
  code quality, conventional commits, the generated-artifacts sync pipeline,
  and the PR process.
- **[QUICKSTART.md](QUICKSTART.md)** — the Day-1 happy path: clone →
  `make check-prereqs` → `make dev-up` → `make test-unit` →
  `make deploy-dev-local` → iterate.

## Before your first PR, also read

- **[AGENT-GUARDRAILS.md](AGENT-GUARDRAILS.md)** — the project invariants (no
  admission webhooks, Kind only, Enterprise images only, V2_ONLY discovery,
  server-based architecture with Job-per-CR backups), what breaks if you
  violate them, and which are machine-enforced (drift gate, guard scripts) vs.
  prose. Especially important for LLM-assisted contributions.
- **[CLAUDE.md](../../CLAUDE.md)** — the project constitution and index,
  including the `## Generated artifacts` source→artifact map and the
  regression-prevention rules.

## Other developer references

- [QUICKSTART.md](QUICKSTART.md) — Day-1 setup
- [development.md](development.md) — deeper dev-environment notes
- [testing.md](testing.md) — test tiers, labels, and patterns
- [makefile_reference.md](makefile_reference.md) — every `make` target
- [architecture.md](architecture.md) — system design
