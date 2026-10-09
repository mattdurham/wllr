package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Model is one selectable model from GET /api/models. Key is the
// provider-qualified picker key to echo back in a selection.
type Model struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Sublabel string   `json:"sublabel,omitempty"`
	Provider string   `json:"provider,omitempty"`
	Key      string   `json:"key"`
	Tiers    []string `json:"tiers,omitempty"`
	Active   bool     `json:"active,omitempty"`
}
