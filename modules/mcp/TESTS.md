# mcp — Test Specifications

## Existing Tests

### config_test.go

`LoadConfig` against the mcp-bridge extension's own config.yaml (resolved via a
redirected HOME): missing file → empty config, empty document, unrelated keys
ignored, `servers` parsing (command/args/env), legacy `mcpServers` alias,
`servers` precedence over the alias, null `servers` keeps a non-nil map,
malformed YAML errors, non-map `servers` errors, and the config path default.

### config_bridge_test.go

Bridge tool registration, tool/server lookup errors, empty-bridge close,
no-server start, and tool-result formatting.

### extension_test.go

Basic EventBus subscription and tool dispatch tests.

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestBridge_Spawn_DuplicateID` | Spawn with duplicate ID | Error returned |
| HIGH | `TestMCPBridgeAdapter_Spawn` | MCPBridge interface Spawn method | Delegates to Bridge |
| MEDIUM | `TestExtension_ToolCall_Dispatch` | before_tool_call event for MCP tool | Dispatched to correct server |
