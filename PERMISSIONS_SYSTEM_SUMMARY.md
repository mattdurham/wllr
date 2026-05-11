# wllr Permissions System - Current State & Analysis

## Overview

The wllr permissions system provides a **declarative, manifest-based capability model** for WASM extensions. It distinguishes between **trusted** (built-in) and **untrusted** (user-provided) extensions, granting different levels of access.

---

## Architecture

### Permission Types (sdk/types.go)

Currently defined permissions:

```go
const (
    PermExec         Permission = "exec"          // Execute shell commands
    PermFileOpen     Permission = "file_open"     // Open files
    PermFileRead     Permission = "file_read"     // Read file contents
    PermFileWrite    Permission = "file_write"    // Write file contents
    PermNetworkRead  Permission = "network_read"  // Read from network
    PermNetworkWrite Permission = "network_write" // Write to network
)
```

### Extension Trust Model (extension/host.go)

```go
type Extension struct {
    trusted     bool                       // Trusted = bypass all checks
    permissions map[sdk.Permission]bool    // Declared permissions for untrusted exts
    // ...
}

func (e *Extension) HasPermission(p sdk.Permission) bool {
    if e.trusted {
        return true  // Trusted extensions bypass all checks
    }
    return e.permissions[p]  // Check declared permissions
}
```

### Loading Mechanisms

**1. Trusted Extensions (built-in)**
```go
// LoadBytes with trusted=true
host.LoadBytes(ctx, "builtin.wasm", data, true)
```
- Grants **all permissions** automatically
- No manifest required
- Used for core extensions bundled with wllr

**2. Untrusted Extensions (user-provided)**
```go
// Load from filesystem
host.Load(ctx, "/path/to/extension.wasm")
```
- Reads companion `<name>.json` manifest
- Only grants permissions listed in manifest
- Missing manifest = no permissions

---

## Manifest Format

**File:** `extension-name.json` (alongside `extension-name.wasm`)

```json
{
  "permissions": ["exec", "file_read"]
}
```

**Example:** `extensions/mcp-bridge/mcp-bridge.json`
```json
{"permissions": ["exec"]}
```

---

## Permission Enforcement

### Current Implementation

**1. host_call method handlers check permissions:**

```go
func (h *Host) handleExec(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
    if ext == nil || !ext.HasPermission(sdk.PermExec) {
        return sdk.HostCallResponse{Error: "exec: permission denied: requires exec"}
    }
    // ... proceed with exec
}
```

**2. Extensions can self-check via `request_permission` method:**

```go
// Extension calls host_call with:
{
  "method": "request_permission",
  "params": {"permission": "exec"}
}

// Returns error if permission not granted
```

### Currently Enforced

| host_call Method | Required Permission | Enforcement Location |
|-----------------|---------------------|---------------------|
| `exec`          | `PermExec`          | `handleExec()` |
| `mcp_spawn`     | `PermExec`          | `handleMCPSpawn()` |
| `get_env`       | None (but documented as requiring `PermFileRead`) | Not enforced |

### NOT Currently Enforced

These permissions are **defined but not checked**:
- `PermFileOpen` - No host_call method checks this
- `PermFileRead` - Not checked in `get_env` or anywhere else
- `PermFileWrite` - No host_call method checks this
- `PermNetworkRead` - No host_call methods exist for network access
- `PermNetworkWrite` - No host_call methods exist for network access

---

## File System Permissions (extension/permissions.go)

A **separate, unused** system exists for path-based file access control:

```go
type PermissionsConfig struct {
    Enabled    bool     `json:"enabled"`
    ReadAllow  []string `json:"read_allow,omitempty"`   // Allowed read paths
    ReadDeny   []string `json:"read_deny,omitempty"`    // Denied read paths
    WriteAllow []string `json:"write_allow,omitempty"`  // Allowed write paths
    WriteDeny  []string `json:"write_deny,omitempty"`   // Denied write paths
}
```

**Status:** This code exists but is **not integrated** with the host_call dispatcher.

**Path matching:**
- Prefix-based matching
- Deny rules take precedence over allow
- Supports `~/` expansion
- Normalizes to absolute paths

**Not connected to:**
- `readfile` extension
- `writefile` extension
- `get_env` host_call
- Any actual file I/O operations

---

## Current Extension Permissions

### Built-in Extensions (trusted=true)

All loaded via `LoadBytes(ctx, name, data, true)`:
- `agents` - No manifest (trusted)
- `env` - No manifest (trusted)
- `exec` - No manifest (trusted)
- `history` - No manifest (trusted)
- `readfile` - No manifest (trusted)
- `writefile` - No manifest (trusted)
- `lsp` - No manifest (trusted)

**These bypass all permission checks.**

### User Extensions (trusted=false)

Loaded via `Load(ctx, path)` from filesystem:

| Extension | Manifest File | Declared Permissions |
|-----------|--------------|---------------------|
| `mcp-bridge` | `mcp-bridge.json` | `["exec"]` |
| `context` | `context.json` | `[]` |
| `skills` | `skills.json` | `[]` |
| `tasks` | `tasks.json` | Unknown (file exists but not read) |

---

## Documentation vs Implementation Gaps

### docs/extensions.md States:

> **MethodGetEnv** — Read environment variables from the host. Requires PermFileRead (env is read-only).

**Reality:** `handleGetEnv()` does **not** check `PermFileRead`.

### Permission Types Defined but Unused:

- `PermFileOpen` - No code references
- `PermFileRead` - Defined but never enforced
- `PermFileWrite` - Defined but never enforced
- `PermNetworkRead` - No network host_call methods exist
- `PermNetworkWrite` - No network host_call methods exist

---

## Testing Coverage

### Existing Tests (extension/host_test.go)

**Permission Model Tests:**
- ✅ `TestHost_Permission_TrustedGrantedAll` - Trusted extensions get all perms
- ✅ `TestHost_Permission_UntrustedDeclared` - Untrusted extensions check manifest
- ✅ `TestHost_Permission_RequestPermission_Granted` - Self-check succeeds
- ✅ `TestHost_Permission_RequestPermission_Denied` - Self-check fails

**Not Tested:**
- ❌ Manifest loading from filesystem
- ❌ Invalid manifest handling
- ❌ Permission enforcement in actual host_call methods
- ❌ File system permissions (PermissionsConfig)

---

## Issues & Recommendations

### 1. Incomplete Enforcement

**Problem:** Only `exec` and `mcp_spawn` check permissions. File I/O, env access, and network are unrestricted.

**Fix:**
```go
func (h *Host) handleGetEnv(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
    if ext == nil || !ext.HasPermission(sdk.PermFileRead) {
        return sdk.HostCallResponse{Error: "get_env: permission denied: requires file_read"}
    }
    // ... rest of implementation
}
```

### 2. Unused File System Permissions

**Problem:** `PermissionsConfig` code exists but is never used.

**Options:**
- **Remove it** if path-based restrictions aren't needed
- **Integrate it** by:
  1. Loading config from `config_read("permissions")`
  2. Checking paths in file I/O host_call methods
  3. Documenting the combined capability + path model

### 3. Undefined Permission Semantics

**Problem:** `PermFileOpen` vs `PermFileRead` vs `PermFileWrite` - what's the difference?

**Recommendation:**
```
PermFileRead  -> Can read file contents (read_file, get_env)
PermFileWrite -> Can write file contents (write_file)
PermFileOpen  -> REMOVE (redundant with read/write)
```

### 4. Missing host_call Methods

**Problem:** Network permissions exist but there are no network host_calls.

**Options:**
- Remove `PermNetworkRead`/`PermNetworkWrite` until network access is added
- Add `http_get`, `http_post`, etc. host_call methods

### 5. Manifest Validation

**Current:** Silently ignores invalid manifests.

**Should:**
- Fail extension load on malformed JSON
- Warn on unknown permission strings
- Log missing manifests (currently silent)

---

## Proposed Permission Matrix

| Host Call Method | Required Permission | Currently Enforced? |
|-----------------|---------------------|---------------------|
| `exec` | `exec` | ✅ Yes |
| `mcp_spawn` | `exec` | ✅ Yes |
| `get_env` | `file_read` | ❌ No (docs say required) |
| `read_file` (if added) | `file_read` | N/A (method doesn't exist) |
| `write_file` (if added) | `file_write` | N/A (method doesn't exist) |
| `config_read` | None | N/A (reads own config only) |
| `store_get/set` | None | N/A (isolated per-extension) |

**Note:** `readfile` and `writefile` are currently **extensions** that register tools, not host_call methods. They operate as trusted extensions with full access.

---

## Security Model Summary

**Current State:**
- **Trusted extensions** (built-in) = unrestricted
- **Untrusted extensions** (user) = restricted by manifest
- **Enforcement** = partial (only `exec` + `mcp_spawn`)
- **File system paths** = unrestricted (PermissionsConfig unused)

**Effective Security:**
- ⚠️ User extensions can't `exec` without manifest permission ✅
- ⚠️ User extensions **can** read environment variables without permission ❌
- ⚠️ User extensions rely on built-in `readfile`/`writefile` tools (which are trusted) ❌
- ⚠️ No network isolation (no network methods exist yet) ⚠️

---

## Next Steps for Hardening

1. **Enforce existing permissions:**
   - Add `PermFileRead` check to `handleGetEnv()`
   - Document which host_calls require which permissions

2. **Convert built-in I/O to host_calls:**
   - Replace `readfile` extension with `read_file` host_call
   - Replace `writefile` extension with `write_file` host_call
   - Enforce `PermFileRead`/`PermFileWrite` in handlers

3. **Integrate or remove PermissionsConfig:**
   - Either wire it up to file I/O host_calls
   - Or delete `extension/permissions.go`

4. **Remove unused permissions:**
   - Delete `PermFileOpen` (redundant)
   - Delete `PermNetworkRead`/`PermNetworkWrite` (no network yet)
   - Or add network host_call methods if needed

5. **Improve manifest handling:**
   - Fail on malformed JSON
   - Warn on unknown permissions
   - Log when manifest is missing

6. **Add tests:**
   - Test manifest loading from filesystem
   - Test permission denial in all enforced methods
   - Test path-based restrictions (if PermissionsConfig is kept)

---

## File Reference

**Core Files:**
- `sdk/types.go` - Permission type definitions
- `extension/host.go` - Extension struct, HasPermission(), LoadBytes(), Load()
- `extension/permissions.go` - Path-based PermissionsConfig (unused)
- `extension/host_test.go` - Permission model tests
- `docs/extensions.md` - Public API documentation

**Extension Manifests:**
- `extensions/mcp-bridge/mcp-bridge.json` - Declares `["exec"]`
- `extensions/context/context.json` - Declares `[]`
- `extensions/skills/skills.json` - Declares `[]`
- `extensions/tasks/tasks.json` - Exists but content unknown

**Built-in Extensions (no manifests):**
- All in `extensions/*/main.go` loaded as trusted via `LoadBytes()`
