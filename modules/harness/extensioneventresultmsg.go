package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// ExtensionEventResultMsg carries results from dispatching an event to extensions.
type ExtensionEventResultMsg struct {
	Err            error
	Results        []sdk.EventResponse
	IsSessionEvent bool // true for session_start and reload — triggers updateSuggestions
}
