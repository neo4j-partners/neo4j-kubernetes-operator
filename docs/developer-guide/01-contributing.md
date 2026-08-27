# Contributing

How a change gets from your machine into `main`: branches, CI, commits and merges. What the change
itself has to carry — regenerated output, catalogued reasons, where each package belongs — is in
[Changing the code](02-changing-the-code.md).

Two rules are non-negotiable and everything else on this page exists to make them painless:

> **Every change reaches `main` through a pull request, and a pull request with a failing check is not
> merged.**
>
> **`main` only ever moves forward by fast-forward. No merge commits.**

## The workflow

```bash
git checkout main
git pull --ff-only
git checkout -b fix/pvc-retention-pin
# work, commit
git push -u origin fix/pvc-retention-pin
gh pr create --base main
```

Branch names follow `type/subject`: `feat/`, `fix/`, `test/`, `docs/`, `refactor/`. Keep one concern
per branch — a branch that fixes a bug and reorganises three packages cannot be reviewed, and cannot
be reverted cleanly either.

Never push to `main` directly, even for a one-line documentation fix. The point is not ceremony: CI is
the only thing that runs the end-to-end suites, and a change that skipped it is a change nobody
verified.

## Why a red check blocks the merge

CI is the contract, not an advisory. It runs on every pull request targeting `main`, on pushes to
`main`, and on demand, in two stages:

| Stage | What runs | Roughly |
|-------|-----------|---------|
| `unit` | `make test`, `make govulncheck`, `make audit` | A few minutes |
| `e2e-local-kind` | Nine suites against a real Neo4j on a kind cluster, one step per suite | Tens of minutes |

The second stage `needs` the first, so a compile error or a failing unit test costs you minutes rather
than the full end-to-end cycle. Within the end-to-end job every suite runs even after one fails, so a
single run tells you everything that is broken instead of only the first thing.

Suites execute in a deliberate order — `operator-scope`, then `operator-admission`, then
`workload-standalone`, `workload-cluster`, and the `feature-*` suites. Reading the job from the top,
failures cluster around the layer that broke.

When a suite fails, the run uploads `tests/results/` as an artifact, kept for 14 days. Download it
before re-running: it holds the operator log, the resource status and the assertion output at the
moment of failure, which is usually enough to diagnose without reproducing locally.

Azure does not gate merges. The same suites run against a real AKS cluster in the *E2E — all
platforms* workflow, which fires daily at 05:00 UTC and creates then destroys the cluster on every
run; you can also start it by hand from the Actions tab. Keeping it out of the pull request path is deliberate: it is slow, it costs
money, and a cloud outage should not block a merge.

## Run the gates before you push

The whole first stage takes a couple of minutes locally, and catches most of what CI would catch:

```bash
make test          # unit tests under src/
make audit         # regenerate CRD manifests, CRD validator, reconcile linter, error catalog
make govulncheck   # known vulnerabilities in dependencies
```

`make audit` shells out to Python, so `python3` needs to be on your PATH. Use a Go toolchain matching
CI (1.26.5); the module declares `go 1.26.0`.

For anything touching the reconcile path, run the end-to-end suite you are most likely to break
before opening the pull request:

```bash
bash tests/bin/setup-local-kind.sh                     # once per kind cluster
CLOUD=local-kind ./tests/bin/run-e2e.sh workload-standalone
```

A full local matrix run exists (`make test-e2e-matrix`) and takes a long time. It is worth it before a
large change, and a waste of an afternoon otherwise. Suite mechanics, profiles and how to add a case
live in [`tests/contribute.md`](../../tests/contribute.md), with the harness design in
[`tests/design.md`](../../tests/design.md).

## Merging, fast-forward only

The history of `main` is linear. That is a deliberate choice: with no merge commits, `git log` reads as
the actual sequence of changes, `git bisect` has no branch structure to navigate, and reverting a
change means reverting one commit.

The cost is that you rebase instead of merging. Before asking for a merge, put your branch directly on
top of `main`:

```bash
git fetch origin
git rebase origin/main
# resolve conflicts, re-run make test
git push --force-with-lease
```

Use `--force-with-lease`, never plain `--force`: it refuses the push if someone else added commits to
your branch, which is the one case where a force push destroys work.

Once the checks are green and the branch is reviewed, the merge is a fast-forward:

```bash
git checkout main
git pull --ff-only
git merge --ff-only fix/pvc-retention-pin
git push origin main
```

`--ff-only` is what makes the rule enforceable rather than aspirational: if the branch is not directly
on top of `main`, the command fails instead of quietly creating a merge commit. When it fails, rebase
again — someone landed a change while you were waiting.

From the GitHub interface, only **Rebase and merge** produces the same shape. Do not use *Create a
merge commit*. Note that GitHub's rebase rewrites commit hashes, so if you care about the hashes
matching what CI tested, do the fast-forward locally.

Delete the branch after merging, locally and on the remote. A rebased branch left behind is a trap for
the next person who checks it out.

## Commits

The history uses `type(scope): summary` in the imperative:

```
fix(security): require loadBalancerSourceRanges for LoadBalancer
test(e2e): default probes + status condition catalog
docs(user-guide): explain the Secret opt-in labels
```

Explain the *why* in the body when the summary cannot carry it. A commit whose message is `fix tests`
tells a future reader nothing about the constraint it encoded.

## Reviews

Expect questions about failure modes rather than style: what happens on the second reconcile, what
happens when the object already exists with different content, what the user sees when the input is
wrong. Answer those in the code — through an idempotent apply, a stable condition reason, an Event —
rather than in the pull request discussion, where the answer is lost after merge.

The checklist a reviewer runs through — regenerated CRD, catalogued reasons, examples, coverage — is
in [Changing the code](02-changing-the-code.md#what-a-change-usually-has-to-touch).
