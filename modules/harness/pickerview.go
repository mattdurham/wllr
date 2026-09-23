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
	preview      bool
	active       bool
	// previewScroll is the scroll offset of the right preview pane.
	previewScroll int
}
