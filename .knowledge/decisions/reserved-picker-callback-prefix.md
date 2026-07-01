---
type: Decision
title: Reserved "__wllr:" prefix for core-owned picker callbacks
description: Picker callbacks prefixed __wllr: route to native harness handlers instead of dispatching EventOnCommand to a WASM extension.
tags: [harness, pickers, ui]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** The picker overlay carries a `callback` string. Callbacks prefixed
`__wllr:` (e.g. `__wllr:model`, `__wllr:thinking`, `__wllr:auth`) are routed by
`updateKeyPressPicker` to native harness handlers; all others dispatch
`EventOnCommand` to the WASM extension that opened the picker.

**Rationale:** Lets core features (model/thinking/auth pickers) reuse the same
`ShowPicker` widget as extensions without a second implementation. The prefix is
reserved so an extension command name cannot collide with or spoof a core path.

**Consequence:** New core-owned pickers use a `__wllr:` callback + a native
handler branch. Extensions must not use the `__wllr:` prefix.

# Applies To

- [harness package](../packages/harness.md)

# Origin

modules/harness/NOTES.md (model picker); reused by /thinking and auth.
