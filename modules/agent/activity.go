package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "time"

// ActivitySnapshot captures non-mutating runtime liveness state for status
// surfaces. Timestamps are zero when the event has not happened yet.
type ActivitySnapshot struct {
	TurnStartedAt     time.Time
	LastActivityAt    time.Time
	LastToolCallAt    time.Time
	ActiveToolCallID  string
	ActiveToolName    string
	LastToolName      string
	ShutdownRequested bool
}
