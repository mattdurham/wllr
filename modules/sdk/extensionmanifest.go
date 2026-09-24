package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ExtensionManifest is loaded from extension.yaml in the extension's directory
// (alongside the .wasm file). <basename>.yaml/.yml are also accepted, and the
// legacy JSON manifest (<basename>.json) is read last for older installs. It
// declares the permissions the extension requires and its dispatch priority.
type ExtensionManifest struct {
	// Priority controls event dispatch order. Lower = runs first.
	// Built-ins default to 0, user extensions to 100 when unset.
	// Within the same priority, extensions run alphabetically by name.
	Priority    *int         `json:"priority,omitempty"`
	Permissions []Permission `json:"permissions"`
}
