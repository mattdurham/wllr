package harness

// extensionTickMsg is fired once per second to give extensions a periodic
// callback for time-based updates (e.g. the statusline extension).
type extensionTickMsg struct{}
