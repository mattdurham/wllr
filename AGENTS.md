# wllr — Agent Guidelines

This repository is a terminal AI coding assistant built with Bubble Tea, wazero,
and Go/WASM extensions. Keep changes scoped and follow existing module
boundaries.

## Issue Tracking

- Work tracking lives in GitHub issues for `mattdurham/wllr`.
- When a user references an issue number in this repo, treat it as a GitHub
  issue unless they say otherwise.
- Prefer linking new work to an existing issue before starting broader changes.
- GitHub Issues are the source of truth for work status. If the `gh` CLI is
  installed and authenticated, use `gh issue view`, `gh issue list`, and related
  commands; otherwise use the GitHub web interface or connector. Local plans
  and knowledge files are supporting context, not replacements for issue state.
- See [.knowledge/decisions/github-issue-tracking.md](.knowledge/decisions/github-issue-tracking.md)
  for the full workflow and fallback rules.

## Repository Layout

- `cmd/` — binary entry point, provider/config wiring, OAuth/login, native tools,
  embedded built-in WASM loading, and startup orchestration.
- `cmd/builtins/` — generated/embedded built-in WASM extension artifacts. Build
  with `make builtins` or `make build`.
- `modules/sdk/` — shared wire types, event/method constants, and ABI-facing
  structs. Leaf package; must not depend on wllr internals.
- `modules/agent/` — LLM turn execution, compaction, context usage, agent pool,
  sub-agent spawning, teams, liveness, inbox/queue handling.
- `modules/extension/` — wazero host, extension lifecycle, host calls, event
  dispatch, permissions, per-extension stores, tool ownership.
- `modules/tools/` — adapts `sdk.Tool` definitions into `fantasy.AgentTool`.
- `modules/session/` — subsystem wiring and lifecycle glue between host, pool,
  main agent, and renderer.
- `modules/harness/` — Bubble Tea TUI, rendering, input handling, scene graph,
  status/tool/queue UI, and `Renderer` interface.
- `modules/mcp/` — MCP server subprocess bridge and dynamic MCP tool discovery.
- `modules/testutil/` — fake LM/provider helpers for tests.
- `extensions/` — Go WASM extension source plus shared `wllrsdk.go` boilerplate.
- `docs/` — extension API, tool contracts, provider docs, design notes, and
  plans.
- `.knowledge/` — the repository knowledge catalog, conforming to OKF v0.2.
  Start with [.knowledge/index.md](.knowledge/index.md) for architecture,
  package details, decisions, patterns, playbooks, and planned features. Use
  the [OKF specification](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
  for the bundle format and metadata conventions.

When adding or revising durable repository knowledge, update the relevant
`.knowledge/` concept and its section index. Every non-reserved Markdown file
in the bundle must have YAML frontmatter with a non-empty `type`; `index.md`
and `log.md` use their reserved OKF structures.

## Runtime Config And State

- Main config path: `WLLR_CONFIG` if set, otherwise
  `~/.config/wllr/config.yaml`.
- Config format: flat YAML object keyed by group name. The main app reads the
  `wllr` group; extensions read their own group names.
- Installed optional extensions: `~/.wllr/extensions/<name>/`.
- Runtime logs: `~/.wllr/logs/`.
- Session history: `~/.wllr/sessions/`.
- User skills: `~/.wllr/skills/`.
- Build artifacts: `dist/` and `cmd/builtins/*.wasm`.

Useful config/env entry points:

- `WLLR_PROVIDER`, `WLLR_MODEL`
- `WLLR_CONTEXT_WINDOW`
- `WLLR_CONFIG`
- `WLLR_EXTENSIONS_DIR`
- `WLLR_COMPACT_THRESHOLD`

## Built-In Components

Built-in WASM extensions are embedded into the binary by `cmd/main.go` from
`cmd/builtins/*`. Rebuild them with `make build` or `make builtins`.

- `extensions/agents/` — chat transcript scene ownership plus agent/team tools:
  `create_agent`, `send_message`, `list_agents`, `get_agent_status`,
  `shutdown_agent`, team creation/membership/shutdown.
- `extensions/history/` — session persistence and browse/rollback UI. Writes
  JSONL session records under `~/.wllr/sessions/`.
- `extensions/logging/` — log event sink. Writes rolling log files under
  `~/.wllr/logs/`.
- `extensions/queue/` — queued-message visibility and `/queue` command support.
- `extensions/statusline/` — scene-driven statusline area, provider/model/status
  display, token/context indicators.

Native Go tools are not WASM but are part of the default tool surface:

- `read_file`, `write_file`, `exec`, `get_env`

Optional installed extensions are built by `make extensions` into
`~/.wllr/extensions/`:

- `extensions/context/` — loads project context such as `AGENTS.md` and system
  prompt text.
- `extensions/skills/` — skill discovery and `list_skills`/`get_skill`.
- `extensions/tasks/` — task list tools.
- `extensions/lsp/` — code-intelligence tools for diagnostics, linting,
  symbols, definitions, references, and refactor preview.
- `extensions/memory/` — memory/Engram integration.
- `extensions/permissions/` — read/write/exec permission policy enforcement.
- `extensions/mcp-bridge/` — MCP subprocess bridge and dynamic MCP tools.
- `extensions/otel-traces/` — optional OpenTelemetry trace export.

## Build And Verification

- `make build` — builds built-in WASM extensions and `dist/wllr`.
- `make extensions` — builds built-ins plus installs optional extensions to
  `~/.wllr/extensions`.
- `make test` — runs unit tests.
- `make precommit` — required before commit; runs formatting, linting, WASM
  build, tests, docs checks, deadcode, and staticcheck.

Use `PATH="$(go env GOPATH)/bin:$PATH" make precommit` if local Go-installed
tools are not on `PATH`.

WASM extension builds use `WASM_COMPILER=auto` by default: each extension is
compiled with its explicitly selected compiler. There is no dynamic fallback.
Use `WASM_COMPILER=tinygo` to force TinyGo for compatibility work, or
`WASM_COMPILER=go` to force the previous Go WASI build path. TinyGo builds use
Docker by default (`TINYGO_MODE=docker`) so a host TinyGo install is not
required.

## Spec-Driven Modules

Every non-test `.go` file in `modules/` must include:

```go
// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
```

For code changes in `modules/`, update the module docs as needed:

- `SPECS.md` for interface contracts, invariants, and behavior.
- `NOTES.md` for non-obvious design decisions.
- `TESTS.md` for new or changed test intent.
- `BENCHMARKS.md` for performance behavior changes.

## Extension API Docs

`docs/extensions.md` is the authoritative WASM extension API reference. Update it
in the same commit as any host/extension ABI change: host calls, event types,
payload fields, required exports, permissions, or SDK-facing behavior.

`docs/tool-contracts.md` documents LLM-callable tool input/output contracts.
Update it when adding, removing, or changing tool schemas or result shapes.

## Writing Go WASM Extensions

Most Go extensions use a local copy of `extensions/wllrsdk.go`. Extension source
files target:

```go
//go:build wasip1
```

The fallback Go build command is:

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o extension.wasm .
```

TinyGo builds use `scripts/build-wasm-extension.sh` with Docker by default and
`TINYGO_FLAGS` defaulting to `-buildmode=c-shared -target=wasi -opt=z`.

Keep SDK copies synchronized when changing shared extension helper behavior.
