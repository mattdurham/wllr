package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import tea "charm.land/bubbletea/v2"

// Command is a slash command registered with the Registry.
type Command struct {
	Handler func(args []string) tea.Cmd
	Name    string
	Desc    string
	// Instant marks commands whose handler is invoked directly in the update loop,
	// bypassing the WASM extension dispatch path. Built-in commands are always Instant.
	Instant bool
}
