package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LogAttr is one structured key/value attribute on a log record. Values are
// pre-stringified by the host so extensions never need slog's typed Value.
type LogAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
