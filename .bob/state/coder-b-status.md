# Coder B status

Implemented the WASM task facade, durable coordination guidance, public task
contracts, and integration coverage on branch `agent-notifications`.

## Files

- Replaced process-local task maps, counters, claim/update logic, and status
  notifications in `extensions/tasks` with host-ledger calls.
- Added `tasks_report` and `tasks_events_after`; wrappers preserve host errors,
  versions, attempts, result/reason fields, cursors, and workspace-mode metadata.
- Updated task and agent READMEs, agent system guidance, public extension/tool
  docs, manifest, and SDK test specification.
- Reworked integration tests for temporary ledger lifecycle, replay, CAS, and
  report validation.
- Preserved and included the existing agent lifecycle notification changes in
  `modules/agent`, `extensions/agents/main.go`, and `docs/extensions.md`.

## Verification

- `WASM_COMPILER=go make extensions` — passed.
- `go test ./...` — passed.
- `go test -race ./modules/sdk ./modules/extension ./modules/agent` — passed.
- `go test -tags=integration ./test/integration/tasks -count=1` — passed.
- `(cd extensions/tasks && go test ./)` — passed.
- `git diff --check` and guidance term checks — passed.

## Blockers

None. Worktree allocation, shell security, and filesystem enforcement remain
out of scope as requested.
