package main

import (
	"testing"
)

// fakeConfigData installs a ConfigRead stand-in for native tests.
func fakeConfigData(t *testing.T, data []byte) {
	t.Helper()
	orig := configReadOverride
	configReadOverride = func() ([]byte, error) { return data, nil }
	t.Cleanup(func() { configReadOverride = orig })
}

func TestLoadAgentsConfig_MissingUsesDefaults(t *testing.T) {
	fakeConfigData(t, nil)
	cfg, err := loadAgentsConfig()
	if err != nil {
		t.Fatalf("missing config must not error: %v", err)
	}
	// Fresh-install rule: a missing config leaves the guard ENABLED with
	// default tuning — an explicit enabled:false is required to turn it off.
	if !cfg.EnabledOrDefault() {
		t.Fatalf("missing config must default to enabled")
	}
	if cfg.Enabled != nil {
		t.Fatalf("missing config must leave Enabled unset, got %v", *cfg.Enabled)
	}
	want := loopGuardConfig{}.applyDefaults()
	if cfg.Window != want.Window || cfg.MinRepeats != want.MinRepeats ||
		cfg.MaxPeriod != want.MaxPeriod || cfg.Scope != want.Scope {
		t.Fatalf("got %+v want %+v", cfg, want)
	}
}

func TestLoadAgentsConfig_PartialOverrides(t *testing.T) {
	fakeConfigData(t, []byte("loop_guard:\n    window: 6\n    scope: subagents\n"))
	cfg, err := loadAgentsConfig()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Window != 6 {
		t.Errorf("window = %d, want 6", cfg.Window)
	}
	if cfg.Scope != "subagents" {
		t.Errorf("scope = %q, want subagents", cfg.Scope)
	}
	// Untouched fields keep defaults.
	if cfg.MinRepeats != defaultLoopGuardMinRepeats || cfg.MaxPeriod != defaultLoopGuardMaxPeriod {
		t.Errorf("defaults not applied to unset fields: %+v", cfg)
	}
	if !cfg.EnabledOrDefault() {
		t.Errorf("a partial config with no enabled key must still default to enabled")
	}
}

func TestLoadAgentsConfig_Disabled(t *testing.T) {
	fakeConfigData(t, []byte("loop_guard:\n    enabled: false\n"))
	cfg, err := loadAgentsConfig()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.EnabledOrDefault() {
		t.Fatalf("enabled=false not honored: %+v", cfg)
	}
}

func TestLoadAgentsConfig_ExplicitEnabled(t *testing.T) {
	fakeConfigData(t, []byte("loop_guard:\n    enabled: true\n    window: 8\n"))
	cfg, err := loadAgentsConfig()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.EnabledOrDefault() || cfg.Window != 8 {
		t.Fatalf("explicit enabled=true with window override: %+v", cfg)
	}
}

func TestLoadAgentsConfig_MalformedIsError(t *testing.T) {
	fakeConfigData(t, []byte("loop_guard: [unclosed\n"))
	if _, err := loadAgentsConfig(); err == nil {
		t.Fatalf("malformed config must error")
	}
}

func TestLoadAgentsConfig_EmptyGuardSection(t *testing.T) {
	fakeConfigData(t, []byte("loop_guard: {}\n"))
	cfg, err := loadAgentsConfig()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := loopGuardConfig{}.applyDefaults()
	if cfg.Window != want.Window || cfg.MinRepeats != want.MinRepeats ||
		cfg.MaxPeriod != want.MaxPeriod || cfg.Scope != want.Scope || !cfg.EnabledOrDefault() {
		t.Fatalf("empty guard section should default: %+v", cfg)
	}
}
