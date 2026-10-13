package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestModelCommandListerIncludesBuiltins proves the browser's command list is
// the terminal's: the same registry the TUI's autocomplete reads backs
// GET /api/commands, so the two front ends cannot offer different commands.
func TestModelCommandListerIncludesBuiltins(t *testing.T) {
	m := newTestModel()
	infos := m.CommandLister().ListCommands()
	got := map[string]sdk.CommandInfo{}
	for _, c := range infos {
		got[c.Name] = c
	}
	for _, name := range []string{"help", "clear", "model", "models", "thinking", "context", "login"} {
		if _, ok := got[name]; !ok {
			t.Errorf("built-in command %q missing from lister", name)
		}
	}
	if !got["help"].Instant {
		t.Error("built-in /help should be Instant")
	}
	if got["help"].Desc == "" {
		t.Error("built-in /help should carry a description")
	}
}

// TestModelRegisterCommandAppearsInList proves an extension command registered
// after serve-mode wiring still shows up in the list the browser fetches, rather
// than being silently dropped.
func TestModelRegisterCommandAppearsInList(t *testing.T) {
	m := newTestModel()
	m.RegisterCommand("custom", "does a custom thing", true)
	found := false
	for _, c := range m.CommandLister().ListCommands() {
		if c.Name == "custom" {
			found = true
			if c.Desc != "does a custom thing" {
				t.Errorf("desc = %q, want %q", c.Desc, "does a custom thing")
			}
			if !c.Instant {
				t.Error("custom command should be Instant")
			}
		}
	}
	if !found {
		t.Fatal("registered command did not appear in the lister")
	}
}

// TestSlashWordAtMatchesSharedRule guards the harness's autocomplete trigger
// against drifting from the shared sdk rule the browser reimplements.
func TestSlashWordAtMatchesSharedRule(t *testing.T) {
	for _, in := range []string{"/he", "hi /mo", "/help me", "http://x", "nothing", ""} {
		if got, want := slashWordAt(in), sdk.SlashWordAt(in); got != want {
			t.Errorf("slashWordAt(%q) = %d, sdk.SlashWordAt = %d", in, got, want)
		}
	}
}
