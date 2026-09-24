package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// withExtensionsDir points HOME at a temp dir so wllrExtensionsDir() resolves
// inside it, and isolates the shared config file. Returns the extensions root
// (<home>/.wllr/extensions).
func withExtensionsDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	withConfigPath(t)
	return filepath.Join(home, ".wllr", "extensions")
}

// writeFile fails the test if the file cannot be written, creating parents.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadConfigGroup_PerExtensionFileTakesPrecedence(t *testing.T) {
	extDir := withExtensionsDir(t)
	writeFile(t, filepath.Join(extDir, "permissions", "config.yaml"), "deny_commands:\n  - sed\n")
	// The shared file carries a conflicting group; the extension file must win.
	writeFile(t, configPath(), "permissions:\n  deny_commands:\n    - from-shared-file\n")

	raw, err := loadConfigGroup("permissions")
	if err != nil {
		t.Fatalf("loadConfigGroup: %v", err)
	}
	var got struct {
		DenyCommands []string `json:"deny_commands"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.DenyCommands) != 1 || got.DenyCommands[0] != "sed" {
		t.Fatalf("deny_commands = %v, want [sed] from the extension config file", got.DenyCommands)
	}
}

// TestLoadConfigGroup_IgnoresSharedGroup pins the no-fallback contract: a
// legacy extension group left in the app config file is never read. Extension
// config comes only from the extension's own file; the startup migration moves
// legacy keys out of the app config, and anything that slips through is
// ignored rather than honored, so a shadowed second copy of an extension's
// rules can never silently take effect.
func TestLoadConfigGroup_IgnoresSharedGroup(t *testing.T) {
	withExtensionsDir(t)
	writeFile(t, configPath(), "permissions:\n  deny_commands:\n    - ruby\n")

	raw, err := loadConfigGroup("permissions")
	if err != nil {
		t.Fatalf("loadConfigGroup: %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("group backed only by a legacy shared-file key = %s, want {} (no fallback)", raw)
	}
}

// TestMigrateLegacyExtensionConfigs_MovesExtensionGroups covers the startup
// migration: a legacy extension group in the app config file moves (never
// copies) into the extension's own config.yaml, and the app config is rewritten
// without the key.
func TestMigrateLegacyExtensionConfigs_MovesExtensionGroups(t *testing.T) {
	extDir := withExtensionsDir(t)
	writeFile(
		t,
		configPath(),
		"permissions:\n    exec:\n        deny_commands:\n            - sed\nwllr:\n    model: m\n",
	)

	migrateLegacyExtensionConfigs()

	moved, readErr := os.ReadFile(filepath.Join(extDir, "permissions", "config.yaml"))
	if readErr != nil {
		t.Fatalf("read migrated config: %v", readErr)
	}
	if !strings.Contains(string(moved), "deny_commands") || !strings.Contains(string(moved), "sed") {
		t.Fatalf("migrated permissions config = %s, want the deny list", moved)
	}
	// The file contents ARE the group: no wrapper key.
	if strings.Contains(string(moved), "permissions:") {
		t.Fatalf("migrated permissions config = %s, want contents without a group key", moved)
	}

	app, readErr := os.ReadFile(configPath())
	if readErr != nil {
		t.Fatalf("read app config: %v", readErr)
	}
	if strings.Contains(string(app), "permissions") {
		t.Fatalf("app config still holds the legacy key: %s", app)
	}
	if !strings.Contains(string(app), "model: m") {
		t.Fatalf("app config lost the wllr group: %s", app)
	}

	// After migration the group reads from its own file and nothing else.
	raw, err := loadConfigGroup("permissions")
	if err != nil {
		t.Fatalf("loadConfigGroup: %v", err)
	}
	if !strings.Contains(string(raw), "sed") {
		t.Fatalf("group after migration = %s, want the moved deny list", raw)
	}
}

// TestMigrateLegacyExtensionConfigs_NoAppConfig covers a fresh install: with
// no app config file at all, the migration is a no-op that creates nothing —
// missing config is a fine default state.
func TestMigrateLegacyExtensionConfigs_NoAppConfig(t *testing.T) {
	extDir := withExtensionsDir(t) // HOME points at a temp dir; no config file is written

	migrateLegacyExtensionConfigs()

	if _, err := os.Stat(configPath()); !os.IsNotExist(err) {
		t.Fatalf("migration created the app config file on a fresh install: %v", err)
	}
	if _, err := os.Stat(extDir); !os.IsNotExist(err) {
		t.Fatalf("migration created the extensions dir on a fresh install: %v", err)
	}
}

// TestMigrateLegacyExtensionConfigs_PerExtensionFileWins covers an install
// that already has the per-extension file: the stale shared copy is dropped,
// and the existing file is left byte-for-byte alone.
func TestMigrateLegacyExtensionConfigs_PerExtensionFileWins(t *testing.T) {
	extDir := withExtensionsDir(t)
	writeFile(t, filepath.Join(extDir, "permissions", "config.yaml"), "deny_commands:\n  - sed\n")
	writeFile(t, configPath(), "permissions:\n  deny_commands:\n    - stale\nwllr:\n  model: m\n")

	migrateLegacyExtensionConfigs()

	app, readErr := os.ReadFile(configPath())
	if readErr != nil {
		t.Fatalf("read app config: %v", readErr)
	}
	if strings.Contains(string(app), "permissions") {
		t.Fatalf("stale shared key not dropped: %s", app)
	}
	got, readErr := os.ReadFile(filepath.Join(extDir, "permissions", "config.yaml"))
	if readErr != nil {
		t.Fatalf("read per-extension config: %v", readErr)
	}
	if string(got) != "deny_commands:\n  - sed\n" {
		t.Fatalf("per-extension config overwritten: %s", got)
	}
}

// TestMigrateLegacyExtensionConfigs_NoOpWhenClean covers the steady state: an
// app config with only the wllr group (plus non-extension keys) is left
// untouched — the migration must not rewrite files on every launch.
func TestMigrateLegacyExtensionConfigs_NoOpWhenClean(t *testing.T) {
	withExtensionsDir(t)
	app := "wllr:\n    model: m\n"
	writeFile(t, configPath(), app)

	migrateLegacyExtensionConfigs()

	got, readErr := os.ReadFile(configPath())
	if readErr != nil {
		t.Fatalf("read app config: %v", readErr)
	}
	if string(got) != app {
		t.Fatalf("app config rewritten by a no-op migration:\nwant %s\ngot  %s", app, got)
	}
}

// TestMigrateLegacyExtensionConfigs_KeepsUnknownKeys covers keys that cannot
// name an extension: they stay in place so unknown data is never discarded.
func TestMigrateLegacyExtensionConfigs_KeepsUnknownKeys(t *testing.T) {
	withExtensionsDir(t)
	writeFile(t, configPath(), "wllr:\n  model: m\nsome/unsafe:\n  value: kept\n")

	migrateLegacyExtensionConfigs()

	app, readErr := os.ReadFile(configPath())
	if readErr != nil {
		t.Fatalf("read app config: %v", readErr)
	}
	if !strings.Contains(string(app), "some/unsafe") {
		t.Fatalf("unsafe key was dropped from the app config: %s", app)
	}
}

// TestLoadConfigGroup_ExtensionFileIsTheGroup pins the contract that the
// extension config file's contents ARE the group, not a document keyed by the
// group name. A file keyed by name would silently yield {} here.
func TestLoadConfigGroup_ExtensionFileIsTheGroup(t *testing.T) {
	extDir := withExtensionsDir(t)
	writeFile(t, filepath.Join(extDir, "websearch", "config.yaml"), "api_key: abc123\nmax_results: 7\n")

	raw, err := loadConfigGroup("websearch")
	if err != nil {
		t.Fatalf("loadConfigGroup: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["api_key"] != "abc123" {
		t.Fatalf("api_key = %v, want abc123 (file contents should be the group)", got["api_key"])
	}
	if got["max_results"] != float64(7) {
		t.Fatalf("max_results = %v, want 7", got["max_results"])
	}
}

func TestLoadConfigGroup_UnknownGroupIsEmpty(t *testing.T) {
	withExtensionsDir(t)

	raw, err := loadConfigGroup("no-such-extension")
	if err != nil {
		t.Fatalf("loadConfigGroup: %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("unknown group = %s, want {}", raw)
	}
}

func TestLoadConfigGroup_MalformedExtensionFileIsAnError(t *testing.T) {
	extDir := withExtensionsDir(t)
	writeFile(t, filepath.Join(extDir, "broken", "config.yaml"), "key: [unclosed\n")

	if _, err := loadConfigGroup("broken"); err == nil {
		t.Fatal("loadConfigGroup on malformed extension config = nil error, want a parse error")
	}
}

// TestLoadConfigGroup_AcceptsRealYAML guards the format claim: the shared file
// must parse as YAML with indentation and block sequences, not only as JSON.
func TestLoadConfigGroup_AcceptsRealYAML(t *testing.T) {
	withExtensionsDir(t)
	writeFile(t, configPath(), "wllr:\n  provider: local\n  model: qwen3\n")

	raw, err := loadConfigGroup("wllr")
	if err != nil {
		t.Fatalf("loadConfigGroup: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["provider"] != "local" || got["model"] != "qwen3" {
		t.Fatalf("wllr group = %v, want provider=local model=qwen3", got)
	}
}

func TestExtensionConfigPath_RejectsUnsafeNames(t *testing.T) {
	withExtensionsDir(t)
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "../escape"} {
		if got := extensionConfigPath(name); got != "" {
			t.Errorf("extensionConfigPath(%q) = %q, want empty", name, got)
		}
	}
	if got := extensionConfigPath("permissions"); !strings.HasSuffix(got, filepath.Join("permissions", "config.yaml")) {
		t.Errorf("extensionConfigPath(permissions) = %q, want a permissions/config.yaml path", got)
	}
}

func TestWriteWllrConfig_EmitsYAML(t *testing.T) {
	path := withConfigPath(t)
	if err := saveModel("claude-opus-4-8"); err != nil {
		t.Fatalf("saveModel: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if json.Valid(raw) {
		t.Errorf("config file is valid JSON, want YAML:\n%s", raw)
	}
	var all map[string]any
	if err := yaml.Unmarshal(raw, &all); err != nil {
		t.Fatalf("config file is not valid YAML: %v\n%s", err, raw)
	}
	wllr, ok := all["wllr"].(map[string]any)
	if !ok {
		t.Fatalf("wllr group = %#v, want a mapping", all["wllr"])
	}
	if wllr["model"] != "claude-opus-4-8" {
		t.Errorf("wllr.model = %v, want claude-opus-4-8", wllr["model"])
	}
}

// TestWriteWllrConfig_NotBinaryRoundTrips guards the json.RawMessage hazard:
// marshaling a []byte to YAML emits a !!binary blob, which would silently
// corrupt every value. The written file must survive a read back intact.
func TestWriteWllrConfig_NotBinaryRoundTrips(t *testing.T) {
	path := withConfigPath(t)
	if err := saveLocalModels([]localModelConfig{{
		ID:            "deepseek-v4-flash",
		Name:          "Dwarfstar 4 Flash",
		BaseURL:       "http://localhost:11434/v1",
		ContextWindow: 262144,
	}}); err != nil {
		t.Fatalf("saveLocalModels: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "!!binary") {
		t.Fatalf("config file contains a !!binary blob, want plain YAML:\n%s", raw)
	}
	settings := loadWllrSettings()
	if len(settings.LocalModels) != 1 {
		t.Fatalf("len(LocalModels) = %d, want 1", len(settings.LocalModels))
	}
	if got := settings.LocalModels[0]; got.ID != "deepseek-v4-flash" || got.ContextWindow != 262144 {
		t.Fatalf("round-tripped local model = %+v", got)
	}
}
