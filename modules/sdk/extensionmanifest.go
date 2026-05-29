package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ExtensionManifest is loaded from the JSON file alongside a .wasm extension.
// It declares the permissions the extension requires and its dispatch priority.
type ExtensionManifest struct {
	Permissions []Permission `json:"permissions"`
	// Priority controls event dispatch order. Lower = runs first.
	// Built-ins default to 0, user extensions to 100 when unset.
	// Within the same priority, extensions run alphabetically by name.
	Priority *int `json:"priority,omitempty"`
}
