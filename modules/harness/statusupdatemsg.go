package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// StatusUpdateMsg sets or updates a keyed value in the status bar.
type StatusUpdateMsg struct {
	Key, Value string
}
