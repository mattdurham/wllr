package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// PickerView is a fullscreen overlay list picker shown instead of the chat.
type PickerView struct {
	Title        string
	Callback     string
	query        string
	Items        []sdk.ShowPickerItem
	filtered     []int
	selectedIdx  int
	scrollOffset int
	width        int
	height       int
	searchable   bool
	active       bool
}
