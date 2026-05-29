# mcp — Test Specifications

## Existing Tests

### extension_test.go

Basic EventBus subscription and tool dispatch tests.

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestBridge_Spawn_DuplicateID` | Spawn with duplicate ID | Error returned |
| HIGH | `TestMCPBridgeAdapter_Spawn` | MCPBridge interface Spawn method | Delegates to Bridge |
| MEDIUM | `TestExtension_ToolCall_Dispatch` | before_tool_call event for MCP tool | Dispatched to correct server |
