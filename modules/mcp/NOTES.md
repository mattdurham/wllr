# mcp — Design Notes

## 1. EventBus-based dispatch

*Added: original*

**Decision:** MCP tool calls are intercepted via the EventBus (Go-native pub/sub), not via WASM extension dispatch.

**Rationale:** MCP servers are Go-native subprocesses, not WASM modules. Using the EventBus allows the MCP bridge to intercept tool calls without going through the WASM ABI, which would add unnecessary overhead and complexity.

**Consequence:** MCP and WASM extensions coexist without interference. MCP tools appear in the registered tool list alongside WASM-registered tools.

## 2. Config lives in the extension's own YAML file

*Added: 2026-09-24 — all-YAML, per-extension config*

**Decision:** MCP server config is read from
`~/.wllr/extensions/mcp-bridge/config.yaml` with a `servers` key. The old
shared-config format (a `mcp-bridge.mcpServers` key inside
`~/.config/wllr/config.json`, parsed as JSON) is gone.

**Rationale:** Extension configs live beside their WASM, are private to the
extension, and are YAML everywhere. The shared `config.json` path in this
package was a leftover from before the app config moved to YAML and could
never read the new file — it only appeared to work because a missing file
yielded an empty config.

**Consequence:** `configPath` no longer honors `WLLR_CONFIG` (that variable
relocates the app config, not extension configs). The legacy `mcpServers` key
is accepted inside the extension's own file so configs moved by the startup
migration keep working; `servers` wins when both are present. A missing file
is an empty config (fresh-install default).
