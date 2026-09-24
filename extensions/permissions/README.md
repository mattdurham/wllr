# Permissions Extension

A wllr extension that enforces file system permissions and optional command
rules for `read_file`, `write_file`, and `exec` tools.

## Features

- **Intercepts** `read_file`, `write_file`, and configured `exec` calls before they execute
- **Configurable** allow/deny lists for both read and write operations
- **Path matching** with support for exact paths, prefix matching, and glob patterns
- **Tilde expansion** — `~/source` expands to your home directory
- **Command rules** — optionally allow or deny executables such as `sed`
- **Environment variable guard** — refuse commands naming variables you list in `deny_env_vars`
- **Optional** — only enforces permissions when loaded

## Configuration

Configure the extension in the shared wllr config file
(`~/.config/wllr/config.yaml`, or `$WLLR_CONFIG`), under the `permissions`
group. The file is one YAML object keyed by group name:

```yaml
permissions:
  read:
    allow: ["*"]  # Allow reading from anywhere (default)
    deny: []      # No read restrictions
  write:
    allow: ["~/source", "~/documents", "/tmp"]  # Only allow writing here
    deny: ["/etc", "/sys", "/proc"]            # Always deny these
  # Optional command policy; no commands are denied unless configured here.
  exec:
    deny_commands: ["sed", "perl"]
    deny_env_vars: ["AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"]
    deny_shell_operators: false
```

Command rules match the executable at the start of each simple command,
including commands separated by `;`, `&&`, `||`, pipes, or newlines. Paths such
as `/usr/bin/sed` match `sed`. Set `allow_commands` to make the list an
allowlist. This is a pragmatic filter rather than a complete shell parser;
`deny_shell_operators: true` rejects common shell composition as well.

### Denied environment variables

`exec.deny_env_vars` refuses any command that names one of the listed
variables. It is off until you configure it — like `deny_commands`, an empty
list denies nothing. A typical AWS configuration:

```yaml
permissions:
  exec:
    deny_env_vars:
      - AWS_ACCESS_KEY_ID
      - AWS_SECRET_ACCESS_KEY
      - AWS_SESSION_TOKEN
      - AWS_SECURITY_TOKEN
```

The check is a case-insensitive substring match over the whole command, so it
catches lowercase patterns (`env | grep aws_secret_access_key`), `printenv
NAME`, and the assignment form (`AWS_ACCESS_KEY_ID=... go test`) as well as
`$NAME` expansions. Variable names that select or locate credentials rather
than carry them (`AWS_PROFILE`, `AWS_SHARED_CREDENTIALS_FILE`, `AWS_CONFIG_FILE`)
are deliberately not in the example above, so workflows like
`AWS_PROFILE=prod aws s3 ls` keep working.

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
```yaml
permissions:
  read:
    allow: ["*"]
  write:
    allow: ["~"]
    deny: []
```

**Strict mode — only allow specific directories:**
```yaml
permissions:
  read:
    allow: ["~/source", "~/documents"]
    deny: []
  write:
    allow: ["~/source"]
    deny: []
```

**Protect system directories:**
```yaml
permissions:
  read:
    allow: ["*"]
    deny: []
  write:
    allow: ["*"]
    deny: ["/etc", "/sys", "/proc", "/boot", "/dev"]
```

## Build

The extension is compiled to WASM. Use the repo's pinned TinyGo toolchain:

```bash
make optional-extensions
```

Or build it alone (TinyGo 0.42.0 is the pinned version):

```bash
./scripts/build-wasm-extension.sh /tmp/permissions.wasm extensions/permissions
```

## Install

`make optional-extensions` installs it to
`~/.wllr/extensions/permissions/permissions.wasm`, where wllr loads it
automatically on next startup. Use `/reload` to hot-reload without restarting.

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

Remove the extension directory and restart wllr (or use `/reload`):

```bash
rm -rf ~/.wllr/extensions/permissions
```
