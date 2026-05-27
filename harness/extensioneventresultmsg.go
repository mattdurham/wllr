package harness

import "github.com/mattdurham/wllr/sdk"

// ExtensionEventResultMsg carries results from dispatching an event to extensions.
type ExtensionEventResultMsg struct {
	Err     error
	Results []sdk.EventResponse
}
