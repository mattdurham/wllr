package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// extensionTickMsg is fired once per second to give extensions a periodic
// callback for time-based updates (e.g. the statusline extension).
type extensionTickMsg struct{}
