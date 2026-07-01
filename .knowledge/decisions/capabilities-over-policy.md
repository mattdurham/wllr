---
type: Decision
title: Capabilities over policy — ship the seam, not opinions
description: wllr provides transform-capable interceptor seams and hookable events; it does not ship opinionated PII/routing/blocking features.
tags: [extension, interceptor, philosophy]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** wllr ships the *mechanism* — a transform-capable interceptor chain
(`before_tool_call`, `before_provider_request`, `after_tool_call`) and hookable
events (e.g. `log`) — but not the *policy*. Features like PII redaction, request
routing, or blocking are left to extensions built on the seam.

**Rationale:** Keeps the core small and unopinionated; developers compose the
behavior they want (and can audit it) rather than inheriting baked-in policy.

**Consequence:** When tempted to add a behavior, add the capability/seam that
makes it possible and implement the behavior as an extension.

# Applies To

- [extension package](../packages/extension.md), [agent package](../packages/agent.md)

# Origin

docs/plans/2026-06-30-interceptor-contract-design.md; interceptor commits 438affc/0baf465/3c4ab8a.
