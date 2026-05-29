# session — Test Specifications

## Existing Tests

### session_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestWire_ReturnsSession` | Wire returns non-nil Session | Host + pool + renderer | sess != nil |
| `TestWire_Start_FiresSessionStart` | Start dispatches session_start event | No WASM extensions loaded | Start() returns nil error |
| `TestWire_Cancel_NoopWhenNotStreaming` | Cancel safe when idle | No active turn | No panic |

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestWire_NilHost_NoopExtensionCalls` | Wire with nil host | Returns usable Session; extension calls are no-ops |
| HIGH | `TestConversationSession_Submit_SendsToPool` | Submit dispatches to agent pool | pool.Send called with content |
| HIGH | `TestLiveState_ConcurrentAccess` | Status callback read concurrent with state write | No data race (run with -race) |
| MEDIUM | `TestWire_Start_AssemblesSystemPrompt` | Start assembles default action prompt | pool.BaseSystemPrompt non-empty after Start |
| MEDIUM | `TestConversationSession_ReloadExtensions` | ReloadExtensions calls host.Reload | host.Reload called with correct paths |
| LOW | `TestConversationSession_Close_CancelsAgents` | Close cancels all agents | pool.CancelAll called |
