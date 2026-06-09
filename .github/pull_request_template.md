## Summary

<!-- What does this PR change, and why? Link any issue: Closes #123 -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation
- [ ] Refactor / chore / CI

## Testing

<!-- How did you verify this? -->

- [ ] `make test-unit` passes locally
- [ ] `make lint` passes locally
- [ ] Extended Integration Tests run (add the `run-integration-tests` label or
      `[run-integration]` in a commit) — required if you changed the cluster,
      standalone, backup, or restore controllers

## Checklist

- [ ] Conventional Commit title (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, `ci:`)
- [ ] Ran `make sync-all` and committed regenerated artifacts if I changed Go API
      types, kubebuilder/RBAC markers, or CRDs (CI's `check-drift` gate fails otherwise)
- [ ] Updated docs (API reference / user guides) if behavior or fields changed
- [ ] Respected the project invariants in `CLAUDE.md` (no admission webhooks,
      KIND-only dev, V2 discovery, server-based architecture, …)
