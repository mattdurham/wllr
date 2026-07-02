# Packages

## Core Modules (`modules/`)

Spec-driven Go packages — each has SPECS.md / NOTES.md / TESTS.md.

* [sdk](sdk.md) — Shared wire types, lifecycle events, and ABI constants for the host↔extension boundary.
* [extension](extension.md) — wazero-based WASM host; loads modules, dispatches events, mediates capabilities through five typed bridges.
* [agent](agent.md) — LLM turn execution, the agent pool, sub-agent/team spawning, runtime model/provider-option swapping.
* [harness](harness.md) — Bubble Tea TUI (rendering, input, pickers, scene graph), decoupled behind the Renderer interface.
* [session](session.md) — Subsystem wiring and lifecycle; the UI-agnostic coordinator (`Wire`).
* [tools](tools.md) — Pure adapter from `sdk.Tool` to `fantasy.AgentTool`.
* [mcp](mcp.md) — Bridge to external MCP server subprocesses.
* [testutil](testutil.md) — Fake LM/provider test helpers.

## Built-in Extensions (embedded in the binary)

* [agents (built-in)](ext-agents.md) — Sub-agent orchestration, message passing, the WASM-driven chat transcript.
* [history (built-in)](ext-history.md) — Records sessions to JSONL; `/history` browses and replays from a chosen point.
* [logging (built-in)](ext-logging.md) — The log sink; appends `log`-event records to a per-run file.
* [statusline (built-in)](ext-statusline.md) — Renders the status-line scene area from live harness state.

## Installed Extensions (`~/.wllr/extensions/`)

* [context (installed)](ext-context.md) — Injects project context into the system prompt.
* [skills (installed)](ext-skills.md) — Discovers skills and exposes them as slash commands.
* [tasks (installed)](ext-tasks.md) — In-memory task lists with atomic multi-worker claiming.
* [lsp (installed)](ext-lsp.md) — Bridges Language Server Protocol servers for code navigation/diagnostics.
* [memory (installed)](ext-memory.md) — Persistent agent memory across sessions.
* [mcp-bridge (installed)](ext-mcp-bridge.md) — Extension-side glue for MCP server subprocesses.
* [otel-traces (installed)](ext-otel-traces.md) — Ships log/telemetry records to an OpenTelemetry collector.
* [permissions (installed)](ext-permissions.md) — Interactive permission gating — the sandbox's consent layer.
