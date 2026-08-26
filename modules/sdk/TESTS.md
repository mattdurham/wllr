# bob/sdk — Test Specifications

`TestTaskLedgerWireRoundTrip` verifies opaque IDs, raw results, versions,
statuses, workspace modes, and task host-call method constants.

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
