# AGENTS

This repository expects both human contributors and coding agents to follow a small set of explicit rules.

## Required Reading

Read these documents before making non-trivial changes:

- [DEVELOPER.md](/home/yuhuan/feidex/DEVELOPER.md)
- [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)

## Hard Rules

- `DEVELOPER.md` is the default engineering contract for repository structure, build outputs, cache usage, and change boundaries.
- `docs/codex-app-server-state-machine-audit.md` is the protocol contract for Codex app-server behavior.
- If a change touches `internal/app`, `internal/codexrpc`, approvals, turn lifecycle, thread lifecycle, review flow, compaction, tool input, or server requests, you must explicitly check it against the state machine audit.
- Do not introduce behavior that contradicts the documented protocol state unless you also update the audit document and explain the reason.
- Prefer shared helpers and existing abstractions over duplicating similar logic in multiple packages.

## Practical Expectation

- Keep Feidex behavior conservative around approvals and lifecycle transitions.
- When adjusting menus, preserve the current menu structure and item order unless the user explicitly asks to change them.
- Treat protocol correctness as a product requirement, not optional cleanup.
- If there is tension between local convenience and the documented contracts, follow the documented contracts.
