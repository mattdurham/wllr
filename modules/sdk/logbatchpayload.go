package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LogBatchPayload is the payload for EventLog: a coalesced batch of log records.
// Batching keeps the host/WASM crossing rate bounded regardless of log volume.
type LogBatchPayload struct {
	Records []LogRecord `json:"records"`
}
