# modules/wire — SPECS

`wire` is the HTTP/SSE **protocol contract** between a wllr engine (`wllr
serve`, exposed by `modules/server`) and any client (a browser, the
`wllr-multiplex` CLI, `modules/client`). It defines the frame envelope and the
event-kind vocabulary; it contains no behavior.

## Boundary

- **Depends on:** `modules/sdk` (for `sdk.Role`, `sdk.Message`,
  `sdk.ContextUsage`, `sdk.ShowPickerParams`, `sdk.ShowAgentTreeParams`,
  `sdk.UIArea`, `sdk.UIPatchParams`, `sdk.UIUpdateAreaParams`) and the standard
  library.
- **Never depends on:** `modules/server`, `modules/agent`, `modules/harness`,
  `modules/extension`. It is a leaf, like `sdk`.
- **Why it is separate from `modules/server`.** The envelope originally lived in
  `server/wire.go`. A client that imports `server` transitively imports
  `modules/agent` and the provider SDKs — roughly 80 MB of engine for a program
  that only needs to parse JSON frames. `wire` exists so the client links the
  protocol without the engine. `modules/server` re-exports every symbol here as
  an alias (`type Event = wire.Event`, `const KindToken = wire.KindToken`, …), so
  server code and tests are unchanged and there is still exactly one definition
  of each frame.

## Invariants

1. **One definition per frame.** `Event`, `EventKind`, the `Kind*` constants,
   `TextInputParams`, and `ProtocolVersion` are defined here and only here.
   `modules/server` aliases them; it does not redefine them.
2. **Versioned.** `ProtocolVersion` is bumped for any incompatible change to the
   envelope or the meaning of a kind. `GET /api/health` and the `ready` frame
   both carry it, so a client can gate compatibility.
3. **Flat envelope.** `Event` is a flat struct (not a tagged union): a client
   switches on `Kind` and reads the fields relevant to it. Unused fields are
   `omitempty`. Adding a field is backward compatible; changing a field's type or
   meaning is not.
4. **A frame carries only what the engine has.** No field is synthesized for the
   wire.
5. **Seq is monotonic per stream** and assigned by the hub on publish. A client
   treats a skip as a gap and re-syncs from `GET /api/history`.
6. **Frames map 1:1 to engine callbacks or sink effects.** Every kind corresponds
   to an existing engine callback or `sdk.UISink` method; no field is synthesized.

## Surface

### Event kinds

| Kind | Meaning | Key fields |
|------|---------|------------|
| `ready` | Sent once on connect, before replay | `protocol`, `seq` |
| `user_message` | A turn starts with this prompt | `text`, `role`, `inbox` |
| `token` | One streamed text delta | `text` |
| `turn_started` | Turn began, before output | — |
| `turn_done` | Turn completed | `err` (non-empty on failure) |
| `assistant_message` | Completed assistant message; raw markdown + rendered HTML | `text`, `html` |
| `tool_call` | Tool dispatched | `tool_call_id`, `tool_name`, `input` |
| `tool_result` | Tool completed | `tool_call_id`, `tool_name`, `output`, `is_error` |
| `context_usage` | Context-window snapshot | `usage` |
| `notify` | Human-readable notice (e.g. compaction) | `text` |
| `agent_spawned` / `agent_closed` | Pool membership change | `name`, `live` |
| `error` | Non-turn-ending server problem | `err` |
| `modal` | Modal overlay | `text` |
| `picker` | Interactive picker | `picker` |
| `agent_tree` | Agent-tree overlay | `agent_tree` |
| `focus` | Input/transcript owner changed | `text` (agent id) |
| `text_input` | Free-form prompt overlay | `text_input` |
| `abort` | Stop showing an in-progress stream | — |
| `status` | Status-line key/value | `key`, `value` |
| `console` / `console_clear` | Console pane line / clear | `text` |
| `reset_history` | Replace the transcript | `messages` |
| `ui_area` | Area registered | `area` |
| `ui_patch` | Scene-graph op batch | `patch` |
| `ui_area_update` | Area sizing constraints changed | `area_update` |
| `ui_area_remove` | Area removed | `text` (area id) |
| `ui_area_html` | Fully rendered area HTML | `area_id`, `html` |
| `ui_text_append` | Streaming text delta into a node | `area_id`, `node_id`, `text` |

## See also

- `modules/server/SPECS.md` — the HTTP endpoints that emit these frames.
- `modules/client/SPECS.md` — the reference consumer.
