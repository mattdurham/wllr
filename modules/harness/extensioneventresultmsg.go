package harness

import "github.com/mattdurham/wllr/modules/sdk"

// ExtensionEventResultMsg carries results from dispatching an event to extensions.
type ExtensionEventResultMsg struct {
	Err            error
	Results        []sdk.EventResponse
	IsSessionEvent bool // true for session_start and reload — triggers updateSuggestions
}
