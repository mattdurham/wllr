---
type: Feature
title: Live OAuth validation + non-Anthropic provider login
description: Validate the Anthropic OAuth flow end-to-end against the live endpoint and extend login to OpenAI/Gemini.
status: planned
tags: [oauth, auth, providers]
timestamp: 2026-07-01T13:10:47Z
branch: ""
pr: ""
started: ""
completed: ""
---

# Prompt

The Anthropic OAuth login (/login, PKCE + paste-back, refresh) is built and
unit-tested against a mock server. Validate it end-to-end against the live
Anthropic endpoint (a real browser sign-in), then extend interactive login to
the other providers (OpenAI, Gemini), which currently remain API-key only.

# Scope

## Packages Affected

- [harness package](../packages/harness.md) (BeginOAuthFn/CompleteOAuthFn, /login, capture mode)
- cmd/ (oauth_anthropic.go, oauthwire.go, auth.go, provider.go, config.go)

## Decisions to Respect

- [Sandboxed by design](../decisions/sandboxed-by-design.md)
- [Capabilities over policy](../decisions/capabilities-over-policy.md)

# Acceptance Criteria

- A real Anthropic browser sign-in produces a working sk-ant-oat token and completes a turn.
- Refresh-on-expiry verified against the live token lifetime.
- At least one additional provider (OpenAI or Gemini) supports interactive login end-to-end.
- Unsupported providers still fail cleanly with a notification.

# Workflow

_Filled in when work begins._
