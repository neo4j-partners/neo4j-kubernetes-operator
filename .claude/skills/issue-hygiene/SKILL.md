---
name: issue-hygiene
description: Keep GitHub issues honest — dedupe before filing, link every PR to its issue, and close on the right trigger (maintainer-filed work closes on merge; team-reported issues stay open until verified on a pinned release).
---

# GitHub issue hygiene

Repo: `neo4j-partners/neo4j-kubernetes-operator`. This skill encodes the issue/PR
conventions for this project so the tracker stays a true picture of what's done
versus what's merely merged. The mechanics use the `gh` CLI (or the equivalent
GitHub MCP tools — `search_issues`, `issue_write`, `list_issue_types`); the
*policy* is below.

`CONTRIBUTING.md` already mandates two of the four habits — Conventional Commit
titles and "Reference to related issues" on every PR (the PR template's
`Closes #123` line). The closure policy that follows is **not yet documented in
the repo** (it lives only in maintainer convention); treat this skill as its
written home until it lands in `CONTRIBUTING.md`.

## When to use

- You're about to **file** an issue (check for dupes first).
- You're opening a **PR** and need to wire it to its issue correctly.
- A PR just **merged** and you must decide whether its issue closes now or stays
  open for release verification.
- You're doing a tracker sweep and want a checklist for closing stale issues with
  a correct, non-empty `state_reason`.

## The closure policy (the load-bearing rule)

Who *filed* the issue decides what happens when the fixing PR merges:

| Issue author | On merge of the fixing PR | Why |
|---|---|---|
| The **maintainer**, self-filed as tracked work | **Close**, with a comment citing the shipping PR(s); `reason: completed` | No external verifier — merge is the done signal. |
| The **wider team / external reporter** | **Keep OPEN**; add the `ready for review` label and move it to *Ready for review* on the project board | The reporter re-tests against the **next pinned operator release** the maintainer cuts, then signs off. Merge ≠ verified. |

The team path is part of the PS verification flow: a team-reported issue is only
truly done once it's confirmed against a *pinned* release, not the moment code
lands on `main`. Don't auto-close it just because the PR merged.

## Procedure

### A. Before filing — dedupe

1. Search open *and* closed issues for the symptom before creating anything:
   ```bash
   gh issue list  --repo neo4j-partners/neo4j-kubernetes-operator \
                  --search "backup pvc owner-ref" --state all --limit 30
   gh search issues --repo neo4j-partners/neo4j-kubernetes-operator \
                  "backup pvc owner-ref" --state all
   ```
   (MCP equivalent: `search_issues`.) If a match exists, comment there instead of
   opening a duplicate. If you do file a clear duplicate later, close it with
   `--reason duplicate` and a link to the canonical issue.

2. File with a focused title and the matching template
   (`.github/ISSUE_TEMPLATE/bug_report.md` or `feature_request.md` — blank issues
   are disabled by `config.yml`):
   ```bash
   gh issue create --repo neo4j-partners/neo4j-kubernetes-operator \
     --title "backup: operator-owned PVC is owner-ref'd to the CR" \
     --body "..."
   ```

### B. Opening a PR — link it to the issue

3. Use a Conventional Commit title that names the issue, matching the established
   pattern in this repo's history (`fix(backup): ... (#227)`,
   `feat(release): ... (#245 #246)`):
   ```bash
   gh pr create --repo neo4j-partners/neo4j-kubernetes-operator \
     --title "fix(backup): stop owner-ref'ing the backup PVC to the CR (#227)" \
     --body  "Closes #227

   ..."
   ```
   Put a closing keyword (`Closes #NN` / `Fixes #NN`) in the PR **body** so GitHub
   auto-links — that's the `Closes #123` line the PR template already asks for.
   For a *team-reported* issue, deliberately do NOT use a closing keyword
   (auto-close would violate the policy); link it with a plain `Refs #NN`
   instead and close/keep-open by hand per section C.

### C. On merge — close or keep open

4. Identify the issue author, then branch on the policy table above:
   ```bash
   gh issue view <NN> --repo neo4j-partners/neo4j-kubernetes-operator \
     --json author,title,labels -q '.author.login'
   ```

5. **Maintainer-filed → close, always with a reason:**
   ```bash
   gh issue close <NN> --repo neo4j-partners/neo4j-kubernetes-operator \
     --reason completed \
     --comment "Fixed by #<PR>. Shipping in the next release."
   ```
   `--reason` accepts only `{completed|not planned|duplicate}` (MCP `issue_write`
   calls this `state_reason`). **Never close without one** — a bare close leaves
   the tracker ambiguous about whether it was done or abandoned.

6. **Team-reported → keep open, mark ready for verification:**
   ```bash
   gh issue edit <NN> --repo neo4j-partners/neo4j-kubernetes-operator \
     --add-label "ready for review"
   gh issue comment <NN> --repo neo4j-partners/neo4j-kubernetes-operator \
     --body "Fixed by #<PR>, merged to main. Please re-test and confirm once \
   it ships in the next pinned release."
   ```
   It closes only after the reporter verifies on the pinned release the
   maintainer later cuts (see the `cut-release` skill's post-release wrap-up,
   which adds the verification-pin comment).

## Guardrails

- **Dedupe first, every time.** A closed-as-duplicate issue is cheap; a parallel
  thread that splits the discussion is expensive.
- **No closing keyword on team-reported issues' PRs.** GitHub's auto-close would
  pull the issue closed on merge and silently break the verification policy.
- **Every close carries a `state_reason`/`--reason`.** No silent closes.
- **This is process, not code.** The skill changes the tracker, never the repo;
  the only repo-side touchpoints it references (`CONTRIBUTING.md`, the PR
  template, the issue templates) are read-only context here.

## Why this exists / provenance

Distilled from how the maintainer actually triages this tracker: self-filed
"done" issues close on the merge that fixes them, while team-reported issues
stay open under a `ready for review` label until the reporter re-tests against a
freshly *pinned* operator release. Merge is the done-signal for the author who
can self-verify, and only a verification gate for everyone else. The dedupe and
PR-linking habits come straight from `CONTRIBUTING.md` ("Reference to related
issues") and the repo's own commit history, where every fix names its issue
(`fix(...): ... (#NN)`). Codifying it here keeps an agent from auto-closing a
team bug the instant a PR merges — the single most common way this tracker would
otherwise lie about what's verified.
