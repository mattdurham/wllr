package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// ShowPickerMsg asks the TUI to open the interactive picker overlay.
type ShowPickerMsg struct {
	Title    string
	Callback string
	Items    []sdk.ShowPickerItem
}
