---
name: retract-release
description: Safely retract a bad release via the gated retract workflow — probe the grace window in dry-run first, then either re-cut (window open) or supersede with a new patch (window closed). Never reuse a version number.
---

# Retract a release (retract-and-supersede doctrine, #246)

A bad release is unpublished via `.github/workflows/release-retract.yml`. The
core doctrine: **NEVER reuse a version number.** Whether you can cleanly re-cut
or must supersede depends entirely on whether any public artifact has already
shipped — the "grace window."

## STOP-POINTS

- **ALWAYS run `dry-run=true` first.** The dry-run is also the grace-window
  probe; it makes no changes. Do not run any execution toggle until you have
  read a dry-run probe for the exact tag.
- **Window CLOSED ⇒ supersede, do not re-cut.** If any surface is already
  published, you may NOT delete-and-re-cut the same version. You retract the
  published surfaces (this workflow) and ship the fix as the next patch
  (`vX.Y.Z+1`) via the `cut-release` skill.
- **`delete-pinned-image=true` is security-dire only** — it breaks running
  clusters that re-pull the image. Get explicit human confirmation before using it.

## What the dry-run probes (the grace window)

The workflow checks each public surface for the target tag and prints either
"GRACE WINDOW: OPEN" or "GRACE WINDOW: CLOSED at <surfaces>":
- GHCR image tag for `vX.Y.Z`
- the GitHub Release
- the classic Helm index entry (`/charts/index.yaml`)
- the OCI Helm chart version
- versioned docs at `/vX.Y/`
- the OperatorHub submission PR (note: if **MERGED**, it cannot be yanked —
  supersede only)

Window OPEN (nothing published) ⇒ a `delete-git-tag` + re-cut of the same
version is safe. Window CLOSED ⇒ supersede.

## Procedure

1. **Identify the bad tag and the previous-good tag.** Confirm with the human
   which release is being retracted and what to repoint moving image tags /
   latest docs at.
   ```bash
   gh release list --limit 5
   ```

2. **DRY-RUN PROBE (mandatory, no changes):**
   ```bash
   gh workflow run release-retract.yml \
     -f tag=vX.Y.Z \
     -f reason="<why>" \
     -f dry-run=true
   gh run list --workflow=release-retract.yml --limit=1
   gh run watch <run-id> --exit-status
   ```
   Read the probe output and the OPEN/CLOSED verdict in the run log.

3. **Decide, then get human confirmation of the plan:**
   - **Window OPEN:** safe to re-cut. Run the workflow with `dry-run=false` and
     `delete-git-tag=true` (and `previous-good-tag` set, which execution
     requires), then fix the underlying problem and re-cut the SAME version via
     `cut-release`.
   - **Window CLOSED:** retract the published surfaces (next step) and plan a
     **superseding** `vX.Y.Z+1` via `cut-release`. Do not delete-and-re-cut.

4. **EXECUTE the retraction** (`dry-run=false`, with `previous-good-tag` set).
   Enable only the per-surface toggles that the probe showed as PUBLISHED:
   ```bash
   gh workflow run release-retract.yml \
     -f tag=vX.Y.Z \
     -f previous-good-tag=vX.Y.W \
     -f reason="<why>" \
     -f dry-run=false \
     -f repoint-moving-tags=true \   # image: point moving tags at previous good
     -f retract-release=true \        # GitHub release: title banner + prerelease flag
     -f remove-helm-classic=true \    # classic gh-pages helm index regen
     -f retract-docs=true             # republish /vX.Y/ + latest from previous good
   # OCI chart delete and pinned-image delete are separate, more dangerous
   # toggles — only add when the probe + human confirm they're needed.
   ```
   Each step self-guards: it only acts when the probe marked that surface
   PUBLISHED. The run also files a retraction-record issue when `dry-run=false`.

5. **Watch the run to completion** and read the retraction-record issue it
   files:
   ```bash
   gh run watch <run-id> --exit-status
   ```
   If the OperatorHub PR was MERGED, it cannot be yanked — follow the issue's
   checklist to submit a superseding bundle / channel-graph edit.

6. **Ship the fix.** Window OPEN: re-cut the same version. Window CLOSED:
   supersede with `vX.Y.Z+1` via the `cut-release` skill (full gates again).

## Why this exists / provenance

Distilled from the #246 retract-and-supersede work. The grace-window probe
exists because the cheapest safe recovery (delete tag + re-cut) is only valid
while nothing is public; once an artifact ships, users may have pulled it, so
the version is burned forever and the only honest path is a new patch that
supersedes it. Dry-run-first is mandatory precisely because the probe and the
risk assessment are the same operation.
