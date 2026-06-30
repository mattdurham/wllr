package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LogAttr is one structured key/value attribute on a log record. Values are
// pre-stringified by the host so extensions never need slog's typed Value.
type LogAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LogRecord is a single structured log record forwarded to extensions via
// EventLog. Time is RFC3339Nano in UTC. Level is the slog level name in
// lowercase ("debug", "info", "warn", "error"). Attrs preserve emission order.
type LogRecord struct {
	Time    string    `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Attrs   []LogAttr `json:"attrs,omitempty"`
}

// LogBatchPayload is the payload for EventLog: a coalesced batch of log records.
// Batching keeps the host/WASM crossing rate bounded regardless of log volume.
type LogBatchPayload struct {
	Records []LogRecord `json:"records"`
}
