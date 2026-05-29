package sdk

// ShowPickerItem is one entry displayed in the interactive picker overlay.
type ShowPickerItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
}
