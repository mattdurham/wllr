package harness

import tea "charm.land/bubbletea/v2"

// Command is a slash command registered with the Registry.
type Command struct {
	Handler func(args []string) tea.Cmd
	Name    string
	Desc    string
}
