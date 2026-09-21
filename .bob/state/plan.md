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

---

# Ordered Task Runner Implementation Plan (2026-09-17)

**Goal:** Implement an optional WASM task runner that creates/starts ordered
tasks, gives each task to a fresh child agent, advances after explicit
completion, and surfaces review requests in the main chat.

### Task 1: Extension scaffold and durable runner state

Add `extensions/task-runner/` with SDK copy, manifest, README, runner state,
tool schemas, and command registration. Persist list ID, active task, active
agent, and runner phase with `StoreSet`/`StoreGet`. Add Makefile installation.

### Task 2: Ordered task orchestration

Implement list creation/start, atomic next-task claim, child prompt
construction, agent spawn, and completion/review tools. Capture `agent_id` from
`before_tool_call`, enforce ownership, report to the task ledger, request
graceful child shutdown, and advance only on `AGENT_SHUTDOWN`.

### Task 3: User controls and documentation

Add status/stop/continue commands and notifications for started, completed,
review, stopped, and exhausted states. Document the extension and tool
contracts in `docs/extensions.md` and `docs/tool-contracts.md`.

### Task 4: Verification

Build the extension, run focused extension tests and repository tests, run
`git diff --check`, and inspect final status. Do not alter unrelated worktree
changes.

---

# OpenRouter Provider, Key Wizard, and Model Discovery Plan (2026-09-19)

**Goal:** Add OpenRouter as a first-class provider. Let users enter and persist an API key in the TUI, search OpenRouter's remote model catalog, and add chosen models to the regular `/models` picker as prominent saved choices.

**Current architecture:** `cmd/provider.go` builds Fantasy providers; `cmd/config.go`, `cmd/auth.go`, and `cmd/modelconfig.go` load credentials and persist settings. `cmd/main.go` wires provider/model callbacks into the harness. `/login` uses `modules/harness/providerpicker.go`; `/models` opens the current provider's `ModelListFn` choices through `modules/harness/modelpicker.go`. `PickerView` in `modules/harness/picker.go` currently handles only arrows, Enter, and Esc, so search must be implemented. The installed Fantasy v0.36.0 already has `providers/openrouter.New(WithAPIKey(...))`. OpenRouter documents `GET https://openrouter.ai/api/v1/models`, whose entries include model ID, name, and `context_length` (https://openrouter.ai/docs/quickstart).

### Task 1: Provider and credential plumbing

- Modify `cmd/provider.go`, `cmd/config.go`, and `cmd/main.go`; add focused `cmd/provider_test.go` and `cmd/config_test.go` cases. Add `providerOpenRouter = "openrouter"`, `OPENROUTER_API_KEY` plus `auth.json` key resolution (environment takes precedence), a `missingProviderAuth` case, and Fantasy's dedicated OpenRouter provider. Do not route OpenRouter through the generic local provider. Add the provider to `/login` choices and startup selection.
- Keep the key in `auth.json` (`authCredential{Type: api_key, Key: ...}`), which already writes mode 0600. Do not put it in `config.json`, notifications, or logs. Add a narrow callback to save the key and rebuild the live Fantasy provider/model only after persistence succeeds. Preserve the prior active provider/model if key entry is cancelled or invalid.
- `SelectProviderFn` currently expects a built-in default model and treats cloud providers as OAuth. Add a distinct OpenRouter setup path so no OAuth flow starts, and avoid selecting an arbitrary model before the key and chosen catalog model exist. On restart, validate a saved OpenRouter model and rebuild with its stored key.

### Task 2: Remote catalog and saved shortlist

- Add `cmd/openroutermodels.go` and `cmd/openroutermodels_test.go`. Query `GET /api/v1/models` with a bounded HTTP timeout and response size, check status, decode `data` entries, ignore unusable IDs, and retain the documented `context_length` metadata. Keep remote network work in a Bubble Tea command, not the update loop. Report network/auth/parse errors in the UI; retain already saved models if discovery fails.
- Add an `openrouter_models` setting under the existing `wllr` config group via `cmd/modelconfig.go` and `cmd/config.go` (ID, display name, context window). Persist selections idempotently; deduplicate by ID and place the newest selection first. `ModelListFn` in `cmd/main.go` should show these saved OpenRouter models at the top of the OpenRouter `/models` list, with a distinct "Browse OpenRouter models…" action below them. Choosing a remote result saves it, makes it the active model, and exposes it as a normal top-level `/models` choice on subsequent visits and launches. Keep provider/model selection consistent with `contextWindowForSelection`; prompt for a context window only when the API omits a usable value.
- Check OpenRouter model IDs at selection against the fetched catalog when online; preserve saved choices while offline. Reuse the existing model switch and pool context-window wiring. Ensure subagents using OpenRouter can resolve configured model IDs through the active provider and receive the selected model's context window.

### Task 3: Searchable catalog UI and key entry

- Add a dedicated OpenRouter setup/catalog flow in `modules/harness/openroutersetup.go`, wired through `modules/harness/model.go`, `providerpicker.go`, and `modelpicker.go`. Prompt for the key with a masked text input (extend `TextInputView` in `modules/harness/textinputview.go` if needed); never echo it in status/notification. Use core-owned `__wllr:` callbacks and explicit result messages for key save, async catalog fetch, and selection. Esc cancels cleanly.
- Add type-to-filter behavior to `PickerView` in `modules/harness/picker.go` with query display, Unicode-safe deletion, case-insensitive matching against ID/name, selection/scroll reset, empty-result handling, and no behavior change for ordinary pickers unless search is enabled. Enable it for the OpenRouter remote catalog. For a large catalog, render only visible rows and avoid blocking per keystroke. A dedicated search overlay is acceptable if simpler than extending the shared picker.
- Preserve `/model <id>` and existing `/models` navigation. The remote catalog is a separate browsing action; selecting a result should return to the normal saved model flow.

### Task 4: Docs and verification

- Update `modules/harness/SPECS.md`, `NOTES.md`, and `TESTS.md` for the wizard, key-entry safety, picker search, and shortlist contracts. Update `docs/providers.md` with OpenRouter setup, `OPENROUTER_API_KEY`, key storage, model browsing, and saved model behavior. No `docs/extensions.md` or `docs/tool-contracts.md` change is needed unless the implementation changes host/extension ABI or LLM-callable tool contracts.
- Add harness tests for key cancellation/masking, setup order, async success/failure, search/filter/empty result/selection, and saved `/models` action. Add command tests for key precedence and persistence, Fantasy provider creation, HTTP catalog parsing and errors, duplicate selection, context-window handling, restart, and live provider switch.
- Run `gofmt` on touched Go files, `go test ./cmd ./modules/harness ./modules/agent`, `go test ./...`, `make build`, `git diff --check`, and review `git status --short --branch`. Before a commit, run `PATH="$(go env GOPATH)/bin:$PATH" make precommit`. Preserve the existing unrelated worktree changes.

**Risk/rollback:** A large or unavailable catalog can make setup unusable if fetched synchronously; keep the request asynchronous and retain saved choices. Secrets must remain confined to `auth.json` and process memory. Rollback removes the new provider path and UI callbacks without changing existing provider settings; unknown `openrouter_models` data can remain harmless in config.
