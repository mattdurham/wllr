package sdk

// ExtensionManifest is loaded from the JSON file alongside a .wasm extension.
// It declares the permissions the extension requires.
type ExtensionManifest struct {
	Permissions []Permission `json:"permissions"`
}
