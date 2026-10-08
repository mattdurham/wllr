# tools — Test Specifications

## Existing Tests

### adapter_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestParseInputSchema_Empty` | Nil schema returns empty | nil input | Empty params + required, nil error |
| `TestParseInputSchema_MissingRequiredNonNil` | Missing required returns array-safe empty slice | Schema with properties and no required | `required` is non-nil and marshals as `[]` |
| `TestParseInputSchema_WithProperties` | Schema with properties + required | Valid JSON schema | Params has key; required slice populated |
| `TestParseInputSchema_InvalidJSON` | Invalid JSON returns error | `{not json` | err != nil |
| `TestBuildFantasyTools_NilHost` | Nil host returns nil | nil host | nil result |
| `TestSDKToolAdapter_Info` | Tool info passthrough | sdk.Tool with schema | Name/description/parameters match |
| `TestSDKToolAdapter_Run_NilHost` | Nil host produces error response | nil host adapter | IsError() == true |
| `TestWatchdog_TimeoutReturnsErrorToModel` | Wedged host hits watchdog | blockingHost, 50ms timeout | prompt `timed out` error response; Run returned < 2s |
| `TestWatchdog_ParentCancelNotMisreported` | Turn cancelled mid-tool-call | cancel parent ctx in flight | `cancelled` error, not `timed out` |
| `TestWatchdog_SuccessPathUnaffected` | Fast tool with watchdog installed | okHost, 10s timeout | normal success response |
| `TestWatchdog_SetToolCallTimeout` | Setter contract | set 5s then -1 | ToolCallTimeout reflects; <=0 restores default |
| `TestWatchdog_HostErrorSurfaces` | ExecuteTool Go error | errHost | `boom` error response |

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestBuildFantasyTools_EmptyRegistry` | Host with no registered tools | Returns nil |
| HIGH | `TestBuildFantasyTools_SkipsBadSchema` | One tool with invalid schema | Other tools still returned; bad one skipped |
| MEDIUM | `TestSDKToolAdapter_Run_ToolError` | ExecuteTool returns error result | IsError() response returned |
