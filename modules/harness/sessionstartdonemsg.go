package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// sessionStartDoneMsg is returned after EventSessionStart has been dispatched to
// all extensions.
type sessionStartDoneMsg struct {
	Err     error
	Results []sdk.EventResponse
}
