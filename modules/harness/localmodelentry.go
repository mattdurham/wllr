package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LocalModelEntry is a local model chosen or manually entered by the user,
// ready to be persisted and applied as the active provider/model.
type LocalModelEntry struct {
	ID            string
	Name          string
	BaseURL       string
	APIKey        string
	ContextWindow int64
}
