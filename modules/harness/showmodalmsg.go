package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShowModalMsg asks the TUI to open a modal overlay with the given text.
type ShowModalMsg struct {
	Text string
}
