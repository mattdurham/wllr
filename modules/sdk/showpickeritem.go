package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShowPickerItem is one entry displayed in the interactive picker overlay.
type ShowPickerItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
	// Preview is optional multi-line content shown in the right pane of a
	// split picker (ShowPickerParams.Split). When empty the split picker
	// falls back to Sublabel.
	Preview string `json:"preview,omitempty"`
}
