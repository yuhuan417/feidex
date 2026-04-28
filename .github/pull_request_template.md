## Summary

- 

## Verification

- [ ] `./scripts/test_internal_app_refactor_guard.sh`

## Architecture Guardrails

- [ ] This PR does not add new root-level `package app` feature logic that should live in an owning subpackage.
- [ ] This PR does not add a new `*_alias.go` compatibility shim.
- [ ] This PR does not add a new root-level `*_adapters.go` compatibility shim unless it deletes a larger old adapter in the same PR.
- [ ] This PR does not introduce new cross-package raw `map[string]any` payload passing unless the boundary is transport or Feishu card JSON.
- [ ] This PR does not add new scattered `switch configuredBackend(...)` product logic outside the backend layer.
- [ ] If this PR touches approvals, turn lifecycle, thread lifecycle, review flow, compaction, tool input, or server requests, I checked it against `docs/codex-app-server-state-machine-audit.md`.
