package harness

// abortStreamMsg is sent by the OnAbort callback to cancel the active agent turn
// through the bubbletea program.
type abortStreamMsg struct{}
