---
type: Built-in Extension
title: logging (built-in)
description: The log sink — subscribes to the log event and appends slog-formatted records to a per-run file.
resource: ./extensions/logging
tags: [built-in, logging, log-sink, append-file]
timestamp: 2026-07-01T13:10:47Z
---

The `logging` built-in extension writes wllr's log file. It subscribes to the
`log` event (`OnLog`), formats records in slog's text style, and appends them to
`~/.wllr/logs/<timestamp>.log` via the `append_file` host call. Moving the sink
into WASM makes logs **hookable** — any extension can subscribe to `log` and
ship records elsewhere (e.g. otel-traces) without touching the core.

# Source

- [extensions/logging](../../extensions/logging) — includes its own [README](../../extensions/logging/README.md)

# Uses

- `log` event (OnLog), `append_file` (requires `file_write`)

# Related

- [Capabilities over policy](../decisions/capabilities-over-policy.md)
