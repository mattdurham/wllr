# MCP Server Extension for bob:work

## Integration Points

### Phase 1: INIT (additions)

Add MCP server detection after adversarial mode detection:

```markdown
4. **Detect available MCP servers:**

   Spawn a Bash agent to enumerate configured MCP servers:

   ```
   Task(subagent_type: "Bash",
        description: "Detect available MCP servers",
        run_in_background: true,
        prompt: "List available MCP servers and their tools.

                1. Check if .wllr/mcp-servers.json exists:
                   if [ ! -f .wllr/mcp-servers.json ]; then
                       echo 'MCP_AVAILABLE=false'
                       exit 0
                   fi

                2. For each configured server, check if it's running by attempting
                   to invoke a basic tool (or check process list):
                   - Extract server name and command from config
                   - Check if process is running

                3. For running servers, attempt to list available tools:
                   - Use wllr introspection if available
                   - Or document server type from config

                4. Output format:
                   MCP_AVAILABLE=true
                   SERVER: navigator | TOOLS: consult, recall, remember
                   SERVER: filesystem | TOOLS: read, write, search
                   SERVER: <name> | TOOLS: <tool1>, <tool2>, ...

                   OR:

                   MCP_AVAILABLE=false")
   ```

   Read the output and store MCP availability state for later phases.
```

### Phase 3: BRAINSTORM (additions)

Add MCP context to brainstorm prompt:

```markdown
**Step 2: Write brainstorm prompt (updated)**

Write the task context to `.bob/state/brainstorm-prompt.md`:

```
Task description: [The feature/task to implement]
Requirements: [Any specific constraints or acceptance criteria]
Spec-driven modules: [List any directories in scope that contain SPECS.md, NOTES.md, TESTS.md,
  or BENCHMARKS.md — or any .go files with the NOTE invariant comment. These modules require
  doc updates alongside code changes.]

MCP servers available: [List from INIT phase]
  - navigator: consult, recall, remember (persistent knowledge base)
  - filesystem: search, read (codebase search)
  - [other servers and tools]

Consider which MCP tools might be useful for research and implementation.
```
```

Add MCP guidance to team-brainstormer prompt:

```markdown
**Step 5: Spawn knowledge teammates (updated)**

Spawn team-brainstormer:

```
"Spawn a teammate named 'team-brainstormer'.

Teammate prompt:
'You are team-brainstormer. Claim the brainstorm task from the task list
(metadata.task_type: brainstorm), research the codebase following your SKILL.md protocol,
write findings to .bob/state/brainstorm.md, mark the task complete, then stay alive
to answer questions from coders and reviewers about your research and approach decisions.

MCP SERVERS AVAILABLE: [list from INIT]
- Before researching, call mcp__navigator__consult to check for existing knowledge
  about this task scope
- Use mcp__filesystem__search to find relevant code patterns
- After completing research, call mcp__navigator__remember to record your findings

If MCP tools are unavailable, proceed with standard file-based research.

Working directory: [worktree-path]'"
```
```

### Phase 5: SPAWN EXECUTION TEAM (additions)

Add MCP guidance to coder prompts:

```markdown
**Step 1: Spawn coder teammates (updated)**

**Coder 1 and Coder 2 prompts include:**

```
MCP SERVERS AVAILABLE: [list from INIT]
- Before coding, call mcp__navigator__recall to pull proven patterns for packages in scope
- After completing all tasks, call mcp__navigator__remember to record implementation decisions
- Use mcp__filesystem__search to find similar code patterns if needed

If MCP tools are unavailable, proceed with standard grep/ripgrep searches.
```
```

Add MCP guidance to reviewer prompts:

```markdown
**Step 2: Spawn reviewer teammates (updated)**

**Reviewer 1 and Reviewer 2 prompts include:**

```
MCP SERVERS AVAILABLE: [list from INIT]
- Before reviewing, call mcp__navigator__consult for known issues or quality findings
  in the packages being changed
- After review, call mcp__navigator__remember to record high/critical findings

If MCP tools are unavailable, proceed with standard code review.
```
```

### Phase 8: REVIEW (additions)

Add MCP knowledge recording before final review:

```markdown
**Navigator (before reviewing):**

If navigator MCP server is available:
- Call `mcp__navigator__consult` for known issues, past bugs, or quality findings
  in packages being changed
- After review completes, call `mcp__navigator__remember` to record all high/critical
  findings for future sessions

If navigator is unavailable, skip and continue.
```

### Phase 9: COMPLETE (additions)

Add MCP session summary:

```markdown
3. **Record session completion in navigator:**

   If navigator MCP server is available:
   - Call `mcp__navigator__remember` with session summary:
     - What was implemented
     - Key decisions made
     - Issues found and resolved
     - Lessons learned

   If unavailable, skip.
```

## MCP Server Guidelines (add to "Tooling" section in guidelines)

```markdown
### MCP Servers

MCP servers provide additional tools that teammates can use. They are **optional** — the workflow
runs without them, but they enhance quality and speed when available.

**Common MCP servers:**

- **navigator**: Persistent knowledge base (`consult`, `recall`, `remember`)
- **filesystem**: Enhanced codebase search (`search`, `read`)
- **github**: Repository operations (`list_prs`, `get_pr`, `create_issue`)
- **database**: Database introspection and queries
- Custom domain-specific servers

**Usage principles:**

- Check availability in INIT phase
- Include available servers in brainstorm prompt
- Teammates attempt MCP tool calls but proceed normally if unavailable
- Never block on MCP tools — they're enhancements, not requirements

**Tool naming:**

MCP tools are namespaced: `mcp__<server>__<tool>`

Examples:
- `mcp__navigator__consult`
- `mcp__filesystem__search`
- `mcp__github__create_pr`

**Error handling:**

If an MCP tool call fails (server not running, tool unavailable):
- Log the failure
- Fall back to standard approach (grep instead of mcp__filesystem__search)
- Continue workflow normally
```

## Configuration Detection

Add to bob:work skill prerequisites:

```markdown
### MCP Server Configuration (optional)

If `.wllr/mcp-servers.json` exists, bob:work will detect and use configured MCP servers.

Example configuration:

```json
{
  "navigator": {
    "command": "navigator-server",
    "args": ["--db", ".navigator/knowledge.db"],
    "env": {}
  },
  "filesystem": {
    "command": "mcp-filesystem-server",
    "args": [],
    "env": {}
  }
}
```

If no MCP servers are configured, the workflow proceeds without them.
```

## Teammate Skill Updates

Coder and reviewer skills should include MCP awareness:

**team-coder.md additions:**

```markdown
## MCP Server Tools (optional)

If MCP servers are available, use them to enhance implementation:

**Before coding:**
- `mcp__navigator__recall(package_name)` - Pull proven patterns for the package
- `mcp__filesystem__search(pattern)` - Find similar implementations

**After coding:**
- `mcp__navigator__remember(context)` - Record implementation decisions

**If unavailable:**
- Use ripgrep for pattern search
- Document decisions in commit messages
```

**team-reviewer.md additions:**

```markdown
## MCP Server Tools (optional)

If MCP servers are available, use them to enhance review:

**Before reviewing:**
- `mcp__navigator__consult(scope)` - Check for known issues in changed packages

**After reviewing:**
- `mcp__navigator__remember(findings)` - Record high/critical issues for future

**If unavailable:**
- Review based on code and spec docs alone
```

## Implementation Summary

MCP support adds 4 integration points:

1. **INIT**: Detect available MCP servers
2. **BRAINSTORM**: Include MCP context, use navigator for research
3. **EXECUTE**: Coders and reviewers use MCP tools for patterns and knowledge
4. **REVIEW**: Record findings in navigator for future sessions

**No workflow structure changes.** MCP servers are optional enhancements that teammates
attempt to use but gracefully degrade without.
