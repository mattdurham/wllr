---
type: Pattern
title: Reserved-callback core pickers
description: Reuse the one ShowPicker widget for core features by giving them a "__wllr:"-prefixed callback routed to a native handler.
tags: [ui, pickers, harness]
timestamp: 2026-07-01T13:10:47Z
---

wllr has one picker overlay, used by both extensions (selection → `EventOnCommand`)
and core features. Core-owned pickers set a `callback` with the reserved
`__wllr:` prefix; `updateKeyPressPicker` recognises the prefix and emits a native
message (`setModelMsg`, `setThinkingMsg`, `recordAuthMsg`) instead of dispatching
to a WASM extension. This avoids a second picker implementation and prevents an
extension command from colliding with a core path.

# Example

```go
m.picker.Open("Select a model", items, "__wllr:model")
// in updateKeyPressPicker:
if callback == "__wllr:model" {
    return m, func() tea.Msg { return setModelMsg{Model: id} }, true
}
```

# Applies To

- [harness package](../packages/harness.md)
