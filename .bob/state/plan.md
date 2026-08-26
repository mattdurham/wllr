# Model Context Window Resolution Implementation Plan

**Goal:** Require trustworthy per-model context windows and use them consistently for compaction and subagent turns.

**Architecture:** Keep provider/model metadata resolution in the command layer, persist user-supplied values per provider/model, and pass resolved context metadata into agent snapshots. The harness owns the required interactive prompt; the agent layer rejects unresolved production turns.

**Tech Stack:** Go, Bubble Tea, fantasy providers, JSON config, unit and race tests.

---

### Task 1: Per-model agent metadata

Modify `modules/agent/{agent.go,agentpool.go,pool.go,spawnopts.go}` and add tests in `modules/agent/subagent_test.go`.

Add `ContextWindow` to `SpawnOpts`, store it with the agent model snapshot, add per-model pool metadata, and capture LM/model/window atomically at turn submission. Unknown named models resolve to zero and fail before streaming.

### Task 2: Built-in and persisted resolution

Modify `modules/agent/compaction.go`, `modules/agent/models.generated.go`, `scripts/generate-models.go`, `cmd/config.go`, `cmd/modelcatalog.go`, and `cmd/main.go`.

Use known Claude/Codex/OpenAI/Gemini values, local/provider metadata, and persisted `context_windows` values. Initialize the main agent with its resolved window and reject unknown models in exec mode.

### Task 3: Interactive unknown-model prompt

Modify `modules/harness/contextwindow.go`, `model.go`, and `modelpicker.go`; add `contextwindow_test.go` and update harness specs/tests.

Expose a required core text-input flow, validate positive token counts, persist through a callback, and retry model selection only after the value is accepted.

### Task 4: Subagent and reroute consistency

Modify `modules/agent/spawner.go` and `agent.go`; add propagation tests.

Propagate requested model names and use per-model windows. When an interceptor reroutes to a known model, update the turn’s effective context metadata; reject unknown reroutes.

### Task 5: Verification

Run `gofmt`, focused package tests, `go test ./...`, race tests for agent/harness, `go vet ./...`, `git diff --check`, and inspect final status/diff.

