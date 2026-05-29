package harness

import "github.com/mattdurham/wllr/modules/sdk"

// ShowPickerMsg asks the TUI to open the interactive picker overlay.
type ShowPickerMsg struct {
	Title    string
	Callback string
	Items    []sdk.ShowPickerItem
}
