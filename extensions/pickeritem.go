//go:build wasip1

package main

// PickerItem is one entry in a ShowPicker call.
type PickerItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
}
