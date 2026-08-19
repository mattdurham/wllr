package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShowTextInputMsg asks the TUI to open the interactive text input overlay.
type ShowTextInputMsg struct {
	Title        string
	Placeholder  string
	InitialValue string
	Callback     string
}
