# harness — Benchmark Specifications

## Metric Targets

| Benchmark | Target | Description |
|---|---|---|
| `BenchmarkModel_Update_TokenMsg` | < 5 us/op | Cost of processing a single `TokenMsg` through `Model.Update`; visible transcript rendering is driven by WASM `EventToken` batches, not this message. |
| `BenchmarkScene_RenderAppendTextNode` | < 750 us/op | Render previous/current states of a single styled text node for the append fast path. |
| `BenchmarkModel_RefreshWASMChatAppend_1000Lines` | < 2 ms/op | Coalesced append refresh that renders only the trailing assistant node and replaces the cached viewport suffix. |
| `BenchmarkModel_RefreshWASMChat_Full_1000Lines` | < 10 ms/op | Full chat scene render fallback for structural updates, resize, or mixed append targets. |

---

## Benchmark Scenarios

### BenchmarkModel_Update_TokenMsg

**Setup:**
- Create a `Model` via `newTestModel()`.
- Set `m.streaming = true`.
- Run `b.ResetTimer()`.

**Operation:**
- Call `callUpdate(m, TokenMsg{Token: "x"})` in the benchmark loop.

**Rationale:** `TokenMsg` still accumulates assistant text for persistence/logging, but the visible transcript is updated through WASM scene patches. This path should stay trivial.

---

### BenchmarkScene_RenderAppendTextNode

**Setup:**
- Create a `SceneRenderer` with a `chat` area.
- Add a rounded, wrapping assistant text node containing a representative in-flight response.
- Apply a representative appended text suffix.
- Run `b.ResetTimer()`.

**Operation:**
- Call `RenderAppendTextNode("chat", assistantID, width, appendedText)`.

**Rationale:** Append-only streaming renders previous/current states of the active assistant node so the viewport cache can replace a suffix. It must remain proportional to the active assistant block, not the full transcript.

---

### BenchmarkModel_RefreshWASMChatAppend_1000Lines

**Setup:**
- Create a `Model` with a `chat` scene area containing a long transcript and a trailing assistant text node.
- Set `m.chat.externalContent` from a full `refreshWASMChat()`.
- Apply one `append_text` op to the trailing assistant node.
- Set `m.chatAppendID` and `m.chatAppendText`.
- Run `b.ResetTimer()`.

**Operation:**
- Call `m.refreshWASMChatAppend()`.

**Rationale:** This is the streaming hot path after token batching. It should avoid full scene rendering and update the cached viewport content by replacing only the rendered suffix for the active assistant node.

---

### BenchmarkModel_RefreshWASMChat_Full_1000Lines

**Setup:**
- Create a `Model` with a `chat` scene area containing a long transcript.
- Run `b.ResetTimer()`.

**Operation:**
- Call `m.refreshWASMChat()`.

**Rationale:** Full transcript rendering remains necessary for structural chat changes and resize. It can be slower than the append path, but must stay bounded enough for infrequent operations.

---

## Running Benchmarks

```bash
go test ./modules/harness -bench=. -benchtime=5s -benchmem
```

To run a single benchmark:

```bash
go test ./modules/harness -bench=BenchmarkModel_RefreshWASMChatAppend_1000Lines -benchtime=10s -benchmem
```

To compare against a baseline using benchstat:

```bash
go test ./modules/harness -bench=. -count=10 | tee new.txt
benchstat old.txt new.txt
```
