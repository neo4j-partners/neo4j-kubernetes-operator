# Developer Quickstart — Day 1

The single happy path from a fresh clone to your first iterated change. Follow
it top to bottom; each step gates the next. For the full reference see
[CONTRIBUTING.md](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/CONTRIBUTING.md) (root) and
[makefile_reference.md](makefile_reference.md).

> **Kind only.** This project uses Kind (Kubernetes in Docker) for all
> dev/test/CI. minikube, k3s, Docker Desktop K8s, and friends are **not**
> supported. The operator always runs **in-cluster** — never `make dev-run`
> (DNS resolution fails outside the cluster).

## 0. Prerequisites

Install these before you start. Versions are what CI runs against — older may
work but is unsupported.

| Tool | Version | Why / check | Install |
|------|---------|-------------|---------|
| Docker | running daemon | image builds + Kind nodes; `docker info` must succeed | [docs.docker.com/get-docker](https://docs.docker.com/get-docker/) |
| Go | 1.26 | matches `go.mod` (`go 1.26.0`) and CI (`GO_VERSION: '1.26'`) | [golang.org/doc/install](https://golang.org/doc/install) |
| Kind | 0.27.0+ | local Kubernetes; `kind version` | `brew install kind` |
| kubectl | 1.36.x | talks to the Kind cluster | [kubernetes.io/docs/tasks/tools](https://kubernetes.io/docs/tasks/tools/install-kubectl/) |
| make + git | any | drives every workflow | pre-installed on macOS/Linux |

Exact CI-pinned versions live in `.tool-versions` (Go 1.26.1, Kind 0.27.0,
kubectl 1.36.0, kustomize 5.4.3, helm 3.16.0, golangci-lint 1.64.8, ginkgo
2.29.0). If you use [mise](https://mise.jdx.dev/) or asdf, `mise install`
pins all of them. Go toolchains (kustomize, controller-gen, envtest, ginkgo,
etc.) are auto-downloaded into `bin/` by the Makefile — you don't install them
by hand.

## 1. Clone

```bash
git clone https://github.com/priyolahiri/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator
```

## 2. Verify your machine is ready

```bash
make check-prereqs
```

Checks that `go`, `docker` (+ running daemon), `kubectl`, `kind`, and `make`
are present, and fails with an install hint for anything missing. Fix what it
flags before continuing.

## 3. Bring up the dev environment

```bash
make dev-up
```

One command chains `check-prereqs` → `dev-cluster` (Kind cluster
`neo4j-operator-dev` + cert-manager v1.20.0 + `ca-cluster-issuer`) → `install`
(CRDs) → `deploy-dev-local` (builds `neo4j-operator:dev`, loads it into Kind,
deploys), then waits for the operator rollout. First run takes ~3 minutes
(image pulls); later runs reuse the cluster and are faster.

Confirm the operator is running:

```bash
kubectl get deploy -n neo4j-operator-dev neo4j-operator-controller-manager
make operator-logs   # follow logs
```

## 4. Run the unit tests

```bash
make test-unit
```

Fast (~30s), no cluster needed — it spins up envtest in-process. This is your
inner feedback loop. `make test-unit` also runs `fmt` and `vet` as
prerequisites, so it doubles as a quick sanity gate before you commit.

## 5. Make a change and redeploy

Edit code under `internal/`, `api/`, or `cmd/`, then rebuild + redeploy:

```bash
make deploy-dev-local   # rebuild image + reload into Kind + redeploy (~60s)
```

For a tighter loop, use a watcher instead of re-running by hand:

- `make dev-watch` — re-runs `manifests generate build deploy-dev-local` on
  saves under `api/`, `internal/`, `cmd/` (needs `watchexec` or `fswatch`).
- `tilt up` — live reload (~5s) with a web dashboard (needs Tilt).

> **If you touched `api/v1beta1/*_types.go` or a `+kubebuilder:rbac:` marker**,
> regenerate the derived artifacts (CRDs, RBAC, Helm chart, kustomize lists,
> OperatorHub bundle) or CI's drift gate will fail your PR:
>
> ```bash
> make sync-all      # fast: every regeneration step, no bundle
> # or, before tagging a release:
> make ship-prep     # sync-all + bundle + helm-lint + CSV coverage
> ```
>
> See `## Generated artifacts` in `CLAUDE.md` for the full source→artifact map.

## 6. Verify end-to-end (optional but recommended)

```bash
make smoke-test
```

Deploys a minimal Neo4j Enterprise standalone (`hack/smoke-test-standalone.yaml`),
waits for `Ready` (up to 5 min), then cleans up — a quick real-cluster sanity
check. To run one integration test by name:

```bash
make test-one TEST="should create standalone"
```

(Wraps Ginkgo `--focus`; assumes a cluster + operator already exist.)

## 7. Tear down

```bash
make dev-down
```

Deletes the `neo4j-operator-dev` Kind cluster and cleans up dev artifacts.

## Essential `make` targets

`make help` lists the curated set; `make help-all` lists everything.

| Target | What it does |
|--------|--------------|
| `make check-prereqs` | Verify go/docker/kubectl/kind/make are installed |
| `make dev-up` | Create Kind cluster + cert-manager + deploy operator (one command) |
| `make dev-down` | Tear the whole dev environment down |
| `make deploy-dev-local` | Rebuild image + reload into Kind + redeploy (~60s) |
| `make dev-watch` | Auto rebuild+redeploy on file saves (needs watchexec/fswatch) |
| `make test-unit` | Fast unit tests + fmt + vet, no cluster (~30s) |
| `make test-one TEST="..."` | Run a single integration test by name |
| `make smoke-test` | Deploy a standalone, assert it reaches Ready, clean up |
| `make sync-all` | Regenerate all derived artifacts (run after API/RBAC edits) |
| `make check-drift` | CI gate: regenerate + fail if any generated file is stale |

## Before you push

1. `make test-unit` passes (also runs fmt + vet).
2. `make lint` passes locally — **golangci-lint is *not* run in CI**, so this
   is on you.
3. If you touched API types or RBAC markers, `make sync-all` (or `ship-prep`)
   and commit the regenerated files — CI's `check-drift` job blocks the PR
   otherwise.
4. Read [AGENT-GUARDRAILS.md](AGENT-GUARDRAILS.md) — the project invariants
   (no webhooks, Kind only, Enterprise only, V2_ONLY discovery, server-based
   architecture) are non-negotiable and partly machine-enforced.
5. Conventional Commit messages (`feat:`, `fix:`, `docs:`, …).

Run `make install-hooks` once to wire the pre-commit drift/fmt/lint/gitleaks
hooks (needs `pre-commit` on your PATH).
