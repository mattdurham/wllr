package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LogRecord is a single structured log record forwarded to extensions via
// EventLog. Time is RFC3339Nano in UTC. Level is the slog level name in
// lowercase ("debug", "info", "warn", "error"). Attrs preserve emission order.
type LogRecord struct {
	Time    string    `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Attrs   []LogAttr `json:"attrs,omitempty"`
}
