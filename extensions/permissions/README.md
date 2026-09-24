# Permissions Extension

A wllr extension that enforces file system permissions and optional command
rules for `read_file`, `write_file`, and `exec` tools.

## Features

- **Intercepts** `read_file`, `write_file`, and configured `exec` calls before they execute
- **Configurable** allow/deny lists for both read and write operations
- **Path matching** with support for exact paths, prefix matching, and glob patterns
- **Tilde expansion** — `~/source` expands to your home directory
- **Command rules** — optionally allow or deny executables such as `sed`
- **AWS credential guard** — always refuses commands that name a secret-bearing AWS variable
- **Optional** — only enforces permissions when loaded

## Configuration

Configure the extension via `~/.config/wllr/config.toml`:

```toml
[extensions.permissions]
# Read permissions
[extensions.permissions.read]
allow = ["*"]  # Allow reading from anywhere (default)
deny = []      # No read restrictions

# Write permissions
[extensions.permissions.write]
allow = ["~/source", "~/documents", "/tmp"]  # Only allow writing to these directories
deny = ["/etc", "/sys", "/proc"]             # Explicitly deny system directories

# Optional command policy; no commands are denied unless configured here.
[extensions.permissions.exec]
deny_commands = ["sed", "perl"]
deny_shell_operators = false
```

Command rules match the executable at the start of each simple command,
including commands separated by `;`, `&&`, `||`, pipes, or newlines. Paths such
as `/usr/bin/sed` match `sed`. Set `allow_commands` to make the list an
allowlist. This is a pragmatic filter rather than a complete shell parser;
`deny_shell_operators = true` rejects common shell composition as well.

### AWS credential variables

Independently of the rules above, a command naming any of these variables is
always refused:

- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `AWS_SESSION_TOKEN`
- `AWS_SECURITY_TOKEN`

The check is a case-insensitive substring match over the whole command, so it
catches lowercased patterns (`env | grep aws_secret_access_key`) and the
assignment form (`AWS_ACCESS_KEY_ID=... go test`) as well as expansions. It is
not configurable and applies even with an otherwise empty, permissive config.

Variables that select or locate credentials rather than carry them —
`AWS_PROFILE`, `AWS_SHARED_CREDENTIALS_FILE`, `AWS_CONFIG_FILE` — are not
matched, so ordinary workflows like `AWS_PROFILE=prod aws s3 ls` keep working.

**This is a guard rail, not a boundary.** It stops the direct form (an agent
spelling out the variable to copy it), but it cannot see through
`printenv`/`env` with no arguments, `cat /proc/self/environ`, or indirect
expansion. Treat it as one layer; keep credentials out of the environment the
agent runs in.

### Permission Rules

1. **Deny takes precedence** — if a path matches a deny pattern, access is blocked
2. **Allow is checked next** — if a path matches an allow pattern, access is granted
3. **Default behavior**:
   - If `allow` is empty or contains `"*"`, all paths are allowed (unless denied)
   - Otherwise, only paths matching allow patterns are granted access

### Path Patterns

- `"*"` — matches everything
- `"/absolute/path"` — exact match or prefix match (includes subdirectories)
- `"~/path"` — expands to `$HOME/path`
- `"/path/*"` — glob pattern with wildcard

### Examples

**Allow read everywhere, restrict writes to home directory:**
```toml
[extensions.permissions.read]
allow = ["*"]

[extensions.permissions.write]
allow = ["~"]
deny = []
```

**Strict mode — only allow specific directories:**
```toml
[extensions.permissions.read]
allow = ["~/source", "~/documents"]
deny = []

[extensions.permissions.write]
allow = ["~/source"]
deny = []
```

**Protect system directories:**
```toml
[extensions.permissions.read]
allow = ["*"]
deny = []

[extensions.permissions.write]
allow = ["*"]
deny = ["/etc", "/sys", "/proc", "/boot", "/dev"]
```

## Build

```bash
cd extensions/permissions
GOOS=wasip1 GOARCH=wasm go build -o permissions.wasm .
```

## Install

```bash
cp permissions.wasm ~/.config/wllr/extensions/
```

The extension will be loaded automatically on next wllr startup. Use `/reload` to hot-reload without restarting.

## Behavior

When a `read_file` or `write_file` tool call is made:

1. The extension checks the path against the configured rules
2. If **allowed**, the tool proceeds normally
3. If **denied**, the extension:
   - Returns an error result to the LLM
   - Blocks the tool from executing
   - Logs a warning message

The LLM will see the permission denial as a tool error and can respond accordingly.

## Logging

The extension logs to wllr's main log:

- `info` — initialization with active rules
- `debug` — allowed operations (path and tool name)
- `warn` — denied operations (path and tool name)
- `error` — configuration or internal errors

Check logs with wllr's debug output or log file.

## Uninstall

Remove the extension file:

```bash
rm ~/.config/wllr/extensions/permissions.wasm
```

Then restart wllr or use `/reload`.
