# bob/sdk — Test Specifications

`TestTaskLedgerWireRoundTrip` verifies opaque IDs, raw results, versions,
statuses, workspace modes, and task host-call method constants.

Task protocol coverage also includes request/response field presence for
`expected_version`, `attempt_id`, `result`, `reason`, cursors, and replayable
event IDs. Integration tests exercise the facade lifecycle
`create -> claim -> report -> events_after`, stale-version rejection, structured
terminal reporting, and all three workspace-mode placeholders.

## Existing Tests (types_test.go)

---

### TestEventJSONRoundTrip

**Scenario:** Every defined event type marshals to JSON and unmarshals back with identical type and payload bytes.

**Setup:**
- For each of the 9 `EventType` constants, create a `sdk.Event` with the corresponding payload struct encoded as `json.RawMessage`.
- Marshal the `Event` to JSON, then unmarshal back into a fresh `sdk.Event`.

**Assertions:**
- `got.Type` equals the original `EventType` constant.
- `string(got.Payload)` equals `string(payloadBytes)` (byte-identical round-trip, no key reordering).
- No marshal or unmarshal error occurs for any event type.

---

### TestEventResponseRoundTrip

**Scenario:** All meaningful combinations of `EventResponse` fields round-trip through JSON.

**Setup:**
- Test cases: `{Cancel: true}`, `{Block: true}`, `{Error: "something went wrong"}`, `{}` (zero value).
- Marshal each to JSON, then unmarshal back.

**Assertions:**
- `got == c` (struct equality) for every case.
- Empty `EventResponse{}` marshals to `{}` (all `omitempty` fields absent).

---

### TestMessageRoundTrip

**Scenario:** A `Message` with `RoleUser` round-trips through JSON.

**Setup:**
- `msg := sdk.Message{Role: sdk.RoleUser, Content: "hello"}`
- Marshal then unmarshal.

**Assertions:**
- `got.Role == sdk.RoleUser`
- `got.Content == "hello"`

---

### TestToolSchemasPreserveRawJSON

**Scenario:** `Tool.InputSchema` and `Tool.OutputSchema` are stored as `json.RawMessage` and survive a marshal/unmarshal cycle byte-for-byte.

**Setup:**
- Raw input JSON Schema string: `{"type":"object","properties":{"q":{"type":"string"}}}`
- Raw output JSON Schema string: `{"type":"object","properties":{"result":{"type":"string"}}}`
- Create `sdk.Tool{Name: "search", Description: "search the web", InputSchema: json.RawMessage(inputRaw), OutputSchema: json.RawMessage(outputRaw)}`
- Marshal then unmarshal.

**Assertions:**
- `string(got.InputSchema) == inputRaw` (no key sorting, no whitespace change).
- `string(got.OutputSchema) == outputRaw` (no key sorting, no whitespace change).

---

### TestHostCallRoundTrip

**Scenario:** `HostCallRequest` and `HostCallResponse` round-trip through JSON with `json.RawMessage` fields preserved.

**Setup (request):**
- `req := sdk.HostCallRequest{Method: sdk.MethodSubscribe, Params: json.RawMessage({"event":"session_start"})}`
- Marshal then unmarshal.

**Assertions (request):**
- `gotReq.Method == sdk.MethodSubscribe`
- `string(gotReq.Params) == {"event":"session_start"}`

**Setup (response):**
- `resp := sdk.HostCallResponse{Result: json.RawMessage({"ok":true})}`
- Marshal then unmarshal.

**Assertions (response):**
- `string(gotResp.Result) == {"ok":true}`

---

### TestRoleConstants

**Scenario:** Role constants have the exact wire values the Anthropic API expects.

**Setup:** Direct string comparison of exported constants.

**Assertions:**
- `sdk.RoleUser == "user"`
- `sdk.RoleAssistant == "assistant"`

---

### TestEventTypeConstants

**Scenario:** All event type constants are non-empty strings.

**Setup:** Collect all event type constants into a slice.

**Assertions:**
- The list includes every current event type, including `EventModelChanged`.
- Every element is non-empty string.

---

### TestMethodConstants

**Scenario:** All 10 host_call method constants are non-empty strings.

**Setup:** Collect all method constants into a slice.

**Assertions:**
- `len(methods) == 10`
- Every element is non-empty string.

---

## Missing Tests Worth Adding

### TestABIVersionConstant
**Scenario:** `ABIVersion` equals 1.
**Assertion:** `sdk.ABIVersion == 1`
**Rationale:** Catches accidental edits to the ABI version without a deliberate decision.

### TestErrorCodeConstants
**Scenario:** Error code constants have their documented values.
**Assertions:** `sdk.ErrOK == 0`, `sdk.ErrGeneral == 1`, `sdk.ErrCancel == 2`
**Rationale:** These are ABI boundary values; a regression would silently break the WASM protocol.

### TestEventPayloadNilRoundTrip
**Scenario:** An `Event` with a `nil` Payload marshals and unmarshals without error.
**Assertion:** `got.Payload == nil` (or `"null"`) after round-trip.
**Rationale:** Extensions may receive events with no payload; they must not panic.

### TestHostCallRequestOmitsParamsWhenNil
**Scenario:** A `HostCallRequest` with nil `Params` marshals without the `"params"` key.
**Assertion:** Marshalled JSON does not contain `"params"`.
**Rationale:** `omitempty` on `json.RawMessage` must be verified; a nil `json.RawMessage` is `omitempty` only when the slice is nil, not `[]byte("null")`.

### TestHostCallResponseOmitsResultWhenNil
**Scenario:** A `HostCallResponse` with nil `Result` and non-empty `Error` marshals without the `"result"` key.
**Assertion:** Marshalled JSON contains `"error"` but not `"result"`.

### TestOnToolCallPayloadInputPreservesRawJSON
**Scenario:** `OnToolCallPayload.Input` survives a marshal/unmarshal cycle byte-for-byte.
**Assertion:** Same as `TestToolInputSchemaPreservesRawJSON` but for tool-call input.

### BenchmarkEventMarshal
**Scenario:** Marshal a populated `Event` with `BeforeProviderRequestPayload` (realistic size).
**Target:** See BENCHMARKS.md.

### BenchmarkHostCallRequestRoundTrip
**Scenario:** Marshal then unmarshal a `HostCallRequest` with a small params object.
**Target:** See BENCHMARKS.md.

---

## UI Scene-Graph Tests (ui_test.go)

### TestUINodeRoundTrip
**Scenario:** A `UINode` with props and a text child marshals and unmarshals without loss.
**Assertion:** ID, Type, child count, and child text are preserved.

### TestUINodeTextNodeOmitsChildren
**Scenario:** A `UINodeText` node with no children and nil props is marshalled.
**Assertion:** The JSON contains neither `"children"` nor `"props"` (omitempty verified).

### TestUIPatchOpIndexZeroPreserved
**Scenario:** A `UIOpInsert` op with `Index = &0` and one with a nil `Index` are marshalled.
**Assertion:** The first contains `"index":0`; the second omits `"index"` entirely.
**Rationale:** `Index` is `*int` so a valid position of 0 survives while a nil (append) is dropped.

### TestUIPatchParamsRoundTrip
**Scenario:** A `UIPatchParams` with an append_text and a remove op round-trips.
**Assertion:** Area, op count, and the append_text op's ID/Text are preserved.

### TestUIAreaRoundTrip
**Scenario:** A `UICreateAreaParams` with placement main and weight 3 round-trips.
**Assertion:** Area ID, Placement, and Weight are preserved.

### TestTokenPayloadRoundTrip
**Scenario:** A `TokenPayload{AgentID, Text}` marshals and unmarshals without loss.
**Assertion:** AgentID and Text (including a trailing space) are preserved.

### TestNotifyPayloadRoundTrip (covered via test/wasmchat end-to-end)
**Scenario:** `EventNotify`/`NotifyPayload{Text}` is dispatched to the bundled agents.wasm and rendered into the transcript scene.
**Assertion:** The transcript contains the notification text (see test/wasmchat TestAgentsWASMDrivesChatTranscript).

## Context usage tests (contextusage_test.go)

### TestContextUsagePercent
**Scenario:** 80k input over a 100k window.
**Assertion:** `Percent` = 80.0.

### TestContextUsagePercentZeroWindow
**Scenario:** Zero window (unresolved metadata).
**Assertion:** `Percent` is exactly 0 (no divide-by-zero).

### TestContextUsageFromFantasyUsage
**Scenario:** Plain 1000/200 usage over a 200k window.
**Assertion:** Fields pass through; Percent = 0.5.

### TestContextUsageIncludesCacheTokens
**Scenario:** The three cache-reporting conventions: OpenAI (raw input excludes cached: 10k + 80k cache-read), Anthropic (cache read AND creation additive: 10k + 70k + 5k), and a fully cache-served turn (raw input 0 + 90k cache-read).
**Assertion:** Display `InputTokens` is the sum (90000 / 85000 / 90000) and Percent matches the sum — a fully cached turn never displays zero used context.
**Rationale:** Providers subtract or add cache tokens incompatibly; only the sum reconstructs the true prompt size, and the raw field alone blanked the statusline on cache-heavy sessions.

## Shared scene-graph logic (uigraph_test.go)

These tests cover the pure helpers both front-end renderers depend on
(`uigraph.go`, `uilayout.go`). They are the direct unit tests; the renderers add
their own integration tests, and `modules/server/scenemodel_test.go` re-asserts
the same patch behaviours to pin the two front ends together.

| Test | Intent |
|------|--------|
| `TestApplyOp_SetRootAndClone` | `set_root` deep-copies: mutating the result (including the props slice) never touches the source |
| `TestApplyOp_InsertIndexAndErrors` | Index-0 inserts first; missing parent, no root, out-of-range index, and a missing node all error |
| `TestApplyOp_TextOpsRejectNonText` | `append_text`/`replace_text` on a non-text node error, as does a missing target |
| `TestApplyOp_RemoveRootVsChild` | Removing a child prunes it; removing the root yields `nil` (empty area); a missing node errors |
| `TestApplyOp_UnknownOp` | An unrecognised op errors rather than silently no-op'ing |
| `TestCollapseBlankLineRuns` | A single blank line survives; runs of 3+ collapse to 2 |
| `TestPropSides` | 1/2/4-length specs expand correctly; other lengths report `ok=false` |
| `TestParseDimensionAndConstraint` | `fill`/`auto`/numeric and `N%`/absolute constraint resolution, including the no-available-width `fill` case |
| `TestExpandSideCSS` | Side lists render as CSS shorthand; empty yields `""` |

## UI callback router (uicallback_test.go)

Unit tests for the inbound half of the sink contract: routing a front-end
selection back into the engine, and the sentinels that let a server answer
`501` rather than `500`.

| Test | Intent |
|------|--------|
| `TestIsCoreCallback` | The `__wllr:` prefix is the sole core/extension predicate |
| `TestFuncRouter_RoutesCoreAndExtension` | A core callback reaches the core handler and an extension callback the extension handler, with args intact |
| `TestFuncRouter_NilHandlersReturnSentinels` | Nil fields yield `ErrNoCoreHandler`/`ErrNoExtensionHost` |
| `TestFuncRouter_PropagatesHandlerError` | A handler error is returned unchanged (not masked by a sentinel) |
| `TestCoreCallbackFunc_Adapts` | The function adapter satisfies `CoreCallbackHandler` |

## Model-selection contract (modelchoice_test.go)

Unit tests for the shared model-list/key/callback contract that the terminal
picker and the HTTP front end must agree on. A divergence here is silent (a
selection simply matches nothing), so the round trip is pinned directly.

| Test | Intent |
|------|--------|
| `TestModelPickerKeyRoundTrip` | A provider-qualified key survives encode/decode with the unit separator present |
| `TestModelPickerKeyBare` | An empty provider yields the bare model id and decodes back with an empty provider |
| `TestSplitModelPickerKeyEmpty` | An empty key is rejected, so "no selection" is distinguishable |
| `TestModelListerFunc` | The function adapter satisfies `ModelLister` |
| `TestModelPickerCallbackIsCore` | `ModelPickerCallback` is recognized as core-owned by `IsCoreCallback` |

## Slash-command contract (uicommand_test.go)

Unit tests for the shared command-discovery contract. The trigger rule is
reimplemented in the browser client, so it is pinned directly.

| Test | Intent |
|------|--------|
| `TestSlashWordAt` | The trigger rule fires at a word-start slash and not mid-word or after the command name |
| `TestCommandQuery` | The in-progress name is lower-cased and absent when no slash word is present |
| `TestCommandListerFunc` | The function adapter satisfies `CommandLister` |

## Markdown detection contract (md/markdown_test.go)

The renderer is shared by both front ends (the terminal via `format_markdown`,
the browser via `MessageHTML`), so its detection gate is pinned here rather than
only through a front end.

| Test | Intent |
|------|--------|
| `TestDetect_InlineCode` | A line with two backticks is markdown; a lone backtick is not |
| `TestRender_InlineCode` | A code span is styled and its backticks are consumed |
