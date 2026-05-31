package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// abortStreamMsg is sent by the OnAbort callback to cancel the active agent turn
// through the bubbletea program.
type abortStreamMsg struct{}
