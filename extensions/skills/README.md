# Skills Extension

The skills extension discovers `~/.wllr/skills/*/SKILL.md`, registers slash
commands for user-invocable skills, and exposes programmatic skill discovery.
The full tool input and output contracts are documented in
[`docs/tool-contracts.md`](../../docs/tool-contracts.md#skills-extension).

## Tools

- `list_skills` accepts `{}` and returns a JSON array of loaded skill metadata.
- `get_skill` accepts `{ "name": string }` and returns the skill body as plain
  text with frontmatter stripped.

Validation failures and unknown skill names mark the tool call as failed with
plain-text error messages.
