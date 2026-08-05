---
type: Decision
title: GitHub Issues are the source of truth for work tracking
description: Repository work status is recorded in GitHub Issues and accessed through gh or GitHub web tooling.
tags: [github, issues, workflow, tracking]
generated: { by: human:mattdurham, at: 2026-08-05T00:00:00Z }
status: stable
---

GitHub Issues are the authoritative record for repository work: bugs, feature
requests, investigation scope, and completion state belong there. Local plans,
knowledge documents, and worktree notes provide implementation context but must
not be treated as the current issue state.

## Access workflow

1. Resolve the repository as `mattdurham/wllr` and identify an existing issue
   before opening duplicate work.
2. If the `gh` CLI is installed and authenticated, prefer it for issue reads and
   writes:

   ```sh
   gh auth status
   gh issue list --repo mattdurham/wllr
   gh issue view <number> --repo mattdurham/wllr
   gh issue create --repo mattdurham/wllr
   ```

3. If `gh` is unavailable or unauthenticated, use the GitHub web interface or
   an available GitHub connector. Do not block repository investigation merely
   because the CLI is missing.
4. Link implementation plans, commits, and pull requests to the issue when
   practical. Update the issue when scope or status changes materially.

## Agent behavior

When a user mentions an issue number, interpret it as a GitHub issue unless the
user explicitly says otherwise. Before broader implementation, search for a
related open issue and prefer updating or reopening it over creating a duplicate.
