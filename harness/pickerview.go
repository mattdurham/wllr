package harness

import "github.com/mattdurham/wllr/sdk"

// PickerView is a fullscreen overlay list picker shown instead of the chat.
type PickerView struct {
	Title        string
	Callback     string
	Items        []sdk.ShowPickerItem
	selectedIdx  int
	scrollOffset int
	width        int
	height       int
	active       bool
}
