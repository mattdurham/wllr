---
type: Installed Extension
title: otel-traces (installed)
description: Ships log/telemetry records to an OpenTelemetry collector by subscribing to the log event.
resource: ./extensions/otel-traces
tags: [installed, otel,telemetry,observability]
timestamp: 2026-07-01T13:10:47Z
---

The `otel-traces` installed extension subscribes to the `log` event and exports records as OpenTelemetry traces — a concrete example of the hookable log sink.

# Source

- [extensions/otel-traces](../../extensions/otel-traces) — installed to `~/.wllr/extensions/otel-traces/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
