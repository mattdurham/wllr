//go:build wasip1

package main

// PickerItem is one entry in a ShowPicker call.
type PickerItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
	// Preview is multi-line content shown in the right pane of a split picker
	// (ShowPickerSplit). It also participates in type-to-filter matching.
	Preview string `json:"preview,omitempty"`
}
