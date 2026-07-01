---
type: Pattern
title: Interceptor transform chain
description: Extensions can inspect, transform, or block tool calls and provider requests via EventResponse.Payload through Host.DispatchEventChain.
tags: [interceptor, extension, agent, transform]
timestamp: 2026-07-01T13:10:47Z
---

Three seams let extensions transform interactions: `before_tool_call`,
`before_provider_request`, and `after_tool_call`. The host runs them as a
**chain** (`Host.DispatchEventChain`): each extension receives the current
payload and may return an `EventResponse` whose `Payload` replaces it for the
next extension (a transform), or signal a block with a reason. A malformed
transformed payload is tolerated — the original is kept.

This is the mechanism behind [capabilities over policy](../decisions/capabilities-over-policy.md):
wllr ships the transform seam; behaviors (redaction, rerouting, blocking) are
extensions. Author hooks: `OnInterceptToolCall`, `OnInterceptProviderRequest`,
`OnInterceptToolResult`.

# Applies To

- [extension package](../packages/extension.md), [agent package](../packages/agent.md)
