package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// newIsolationHost builds a minimal host with config isolation configured and a
// recording capability provider. It avoids NewHost so the tests need no wazero
// runtime: every handler exercised here reads only host fields that are
// zero-value safe.
func newIsolationHost(t *testing.T, root, shared string, provider *testCapabilityProvider) *Host {
	t.Helper()
	h := &Host{}
	h.capabilities = provider
	h.SetConfigIsolation(root, shared)
	return h
}

// addLoadedExtension registers a named extension as loaded, so the host treats
// its name as another extension's configuration.
func addLoadedExtension(h *Host, name string, perms ...sdk.Permission) *Extension {
	permMap := make(map[sdk.Permission]bool, len(perms))
	for _, p := range perms {
		permMap[p] = true
	}
	ext := &Extension{name: name, permissions: permMap}
	h.extensions = append(h.extensions, ext)
	return ext
}

func configReadParams(group string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"group": group})
	return b
}

func pathParams(path string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"path": path})
	return b
}

func writeParams(path, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return b
}

// TestConfigReadDefaultsToCallerGroup pins that an extension with no explicit
// group reads its own configuration.
func TestConfigReadDefaultsToCallerGroup(t *testing.T) {
	var gotGroup string
	provider := &testCapabilityProvider{onConfigRead: func(group string) (json.RawMessage, error) {
		gotGroup = group
		return json.RawMessage(`{}`), nil
	}}
	h := newIsolationHost(t, t.TempDir(), filepath.Join(t.TempDir(), "config.yaml"), provider)
	ext := addLoadedExtension(h, "queue")

	resp := h.handleConfigRead(ext, sdk.HostCallRequest{})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotGroup != "queue" {
		t.Fatalf("group = %q, want %q", gotGroup, "queue")
	}
}

// TestConfigReadForeignLoadedExtensionDenied is the core security case: one
// extension must not read another's config, even though the other is loaded.
func TestConfigReadForeignLoadedExtensionDenied(t *testing.T) {
	called := false
	provider := &testCapabilityProvider{onConfigRead: func(string) (json.RawMessage, error) {
		called = true
		return json.RawMessage(`{}`), nil
	}}
	h := newIsolationHost(t, t.TempDir(), filepath.Join(t.TempDir(), "config.yaml"), provider)
	addLoadedExtension(h, "permissions")
	evil := addLoadedExtension(h, "evil")

	resp := h.handleConfigRead(evil, sdk.HostCallRequest{Params: configReadParams("permissions")})
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("error = %q, want permission denied", resp.Error)
	}
	if called {
		t.Fatal("config provider was called for a foreign extension's config")
	}
}

// TestConfigReadForeignInstalledButUnloadedDenied covers the case where the
// target extension exists on disk but is not currently loaded, so its config
// must still be unreachable.
func TestConfigReadForeignInstalledButUnloadedDenied(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "permissions"), 0o755); err != nil {
		t.Fatal(err)
	}
	provider := &testCapabilityProvider{onConfigRead: func(string) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}}
	h := newIsolationHost(t, root, filepath.Join(t.TempDir(), "config.yaml"), provider)
	evil := addLoadedExtension(h, "evil")

	resp := h.handleConfigRead(evil, sdk.HostCallRequest{Params: configReadParams("permissions")})
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("error = %q, want permission denied", resp.Error)
	}
}

// TestConfigReadOwnGroupExplicitAllowed pins that naming your own group is fine.
func TestConfigReadOwnGroupExplicitAllowed(t *testing.T) {
	var gotGroup string
	provider := &testCapabilityProvider{onConfigRead: func(group string) (json.RawMessage, error) {
		gotGroup = group
		return json.RawMessage(`{}`), nil
	}}
	h := newIsolationHost(t, t.TempDir(), filepath.Join(t.TempDir(), "config.yaml"), provider)
	ext := addLoadedExtension(h, "permissions")

	resp := h.handleConfigRead(ext, sdk.HostCallRequest{Params: configReadParams("permissions")})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotGroup != "permissions" {
		t.Fatalf("group = %q, want %q", gotGroup, "permissions")
	}
}

// TestConfigReadAppGroupAllowed pins that app-level groups (which name no
// extension) stay readable — the context extension reads "wllr" this way.
func TestConfigReadAppGroupAllowed(t *testing.T) {
	var gotGroup string
	provider := &testCapabilityProvider{onConfigRead: func(group string) (json.RawMessage, error) {
		gotGroup = group
		return json.RawMessage(`{}`), nil
	}}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "permissions"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newIsolationHost(t, root, filepath.Join(t.TempDir(), "config.yaml"), provider)
	ext := addLoadedExtension(h, "context")

	resp := h.handleConfigRead(ext, sdk.HostCallRequest{Params: configReadParams("wllr")})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotGroup != "wllr" {
		t.Fatalf("group = %q, want %q", gotGroup, "wllr")
	}
}

// TestFileAccessForeignConfigDenied covers the file-capability hole: an
// extension with file_write must not reach another extension's config.yaml.
func TestFileAccessForeignConfigDenied(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(t.TempDir(), "config.yaml")
	writes := 0
	provider := &testCapabilityProvider{
		onWriteFile: func(string, string) error { writes++; return nil },
		onReadFile:  func(string) (string, error) { return "", nil },
	}
	h := newIsolationHost(t, root, shared, provider)
	addLoadedExtension(h, "permissions")
	evil := addLoadedExtension(h, "evil", sdk.PermFileWrite, sdk.PermFileRead)

	target := filepath.Join(root, "permissions", "config.yaml")
	resp := h.handleWriteFile(evil, sdk.HostCallRequest{Params: writeParams(target, "{}")})
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("write error = %q, want permission denied", resp.Error)
	}
	if writes != 0 {
		t.Fatal("write_file reached the provider for a foreign config")
	}

	resp = h.handleReadFile(evil, sdk.HostCallRequest{Params: pathParams(target)})
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("read error = %q, want permission denied", resp.Error)
	}
}

// TestFileAccessSharedConfigDenied pins that the shared config (which carries
// every group, permissions included) is off limits to extensions.
func TestFileAccessSharedConfigDenied(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "outside-config.yaml")
	writes := 0
	provider := &testCapabilityProvider{onWriteFile: func(string, string) error { writes++; return nil }}
	h := newIsolationHost(t, root, shared, provider)
	evil := addLoadedExtension(h, "evil", sdk.PermFileWrite)

	resp := h.handleWriteFile(evil, sdk.HostCallRequest{Params: writeParams(shared, "{}")})
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("error = %q, want permission denied", resp.Error)
	}
	if writes != 0 {
		t.Fatal("write_file reached the shared config")
	}
}

// TestFileAccessOwnConfigAllowed pins that an extension keeps access to its own
// configuration, so isolation does not break legitimate self-management.
func TestFileAccessOwnConfigAllowed(t *testing.T) {
	root := t.TempDir()
	var gotPath string
	provider := &testCapabilityProvider{onWriteFile: func(path, _ string) error {
		gotPath = path
		return nil
	}}
	h := newIsolationHost(t, root, filepath.Join(t.TempDir(), "config.yaml"), provider)
	logging := addLoadedExtension(h, "logging", sdk.PermFileWrite)

	own := filepath.Join(root, "logging", "config.yaml")
	resp := h.handleWriteFile(logging, sdk.HostCallRequest{Params: writeParams(own, "{}")})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotPath != own {
		t.Fatalf("provider path = %q, want %q", gotPath, own)
	}
}

// TestFileAccessUnrelatedPathAllowed pins that isolation is scoped to config
// files and does not block ordinary file work.
func TestFileAccessUnrelatedPathAllowed(t *testing.T) {
	root := t.TempDir()
	var gotPath string
	provider := &testCapabilityProvider{onWriteFile: func(path, _ string) error {
		gotPath = path
		return nil
	}}
	h := newIsolationHost(t, root, filepath.Join(t.TempDir(), "config.yaml"), provider)
	ext := addLoadedExtension(h, "logging", sdk.PermFileWrite)

	// Same directory as a config, and a path under the extensions root that is
	// not a config file, must both stay allowed.
	for _, target := range []string{
		filepath.Join(t.TempDir(), "notes.txt"),
		filepath.Join(root, "logging", "state.json"),
	} {
		resp := h.handleWriteFile(ext, sdk.HostCallRequest{Params: writeParams(target, "x")})
		if resp.Error != "" {
			t.Fatalf("unexpected error for %s: %s", target, resp.Error)
		}
		if gotPath != target {
			t.Fatalf("provider path = %q, want %q", gotPath, target)
		}
	}
}

// TestFileAccessTraversalResolvedToConfigDenied pins that a traversal segment
// cannot smuggle a foreign config path past the check.
func TestFileAccessTraversalResolvedToConfigDenied(t *testing.T) {
	root := t.TempDir()
	provider := &testCapabilityProvider{onWriteFile: func(string, string) error { return nil }}
	h := newIsolationHost(t, root, filepath.Join(t.TempDir(), "config.yaml"), provider)
	addLoadedExtension(h, "permissions")
	evil := addLoadedExtension(h, "evil", sdk.PermFileWrite)

	traversal := filepath.Join(root, "evil", "..", "permissions", "config.yaml")
	resp := h.handleWriteFile(evil, sdk.HostCallRequest{Params: writeParams(traversal, "{}")})
	if !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("error = %q, want permission denied", resp.Error)
	}
}

// TestConfigAccessDeniedScoping exercises the pure predicate directly, including
// the boundary cases that are awkward to reach through the handlers.
func TestConfigAccessDeniedScoping(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(t.TempDir(), "config.yaml")
	h := newIsolationHost(t, root, shared, &testCapabilityProvider{})

	cases := []struct {
		name   string
		caller string
		path   string
		want   bool
	}{
		{"own config", "logging", filepath.Join(root, "logging", "config.yaml"), false},
		{"foreign config", "evil", filepath.Join(root, "permissions", "config.yaml"), true},
		{"shared config", "evil", shared, true},
		{"outside root", "evil", filepath.Join(t.TempDir(), "config.yaml"), false},
		{"other file under root", "evil", filepath.Join(root, "permissions", "permissions.wasm"), false},
		{"empty path", "evil", "", false},
		{"hashed path", "evil", filepath.Join(root, "permissions", "sub", "..", "config.yaml"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.configAccessDenied(tc.caller, tc.path); got != tc.want {
				t.Fatalf("configAccessDenied(%q, %q) = %v, want %v", tc.caller, tc.path, got, tc.want)
			}
		})
	}
}

// TestConfigIsolationUnsetAllowsEverything pins the documented default: with no
// isolation configured, config_read and the file capabilities behave as before.
func TestConfigIsolationUnsetAllowsEverything(t *testing.T) {
	var gotGroup string
	provider := &testCapabilityProvider{
		onConfigRead: func(group string) (json.RawMessage, error) {
			gotGroup = group
			return json.RawMessage(`{}`), nil
		},
		onWriteFile: func(string, string) error { return nil },
	}
	h := &Host{}
	h.capabilities = provider
	ext := addLoadedExtension(h, "evil", sdk.PermFileWrite)

	resp := h.handleConfigRead(ext, sdk.HostCallRequest{Params: configReadParams("permissions")})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotGroup != "permissions" {
		t.Fatalf("group = %q, want %q", gotGroup, "permissions")
	}
	resp = h.handleWriteFile(ext, sdk.HostCallRequest{Params: writeParams("/tmp/whatever.yaml", "x")})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}
