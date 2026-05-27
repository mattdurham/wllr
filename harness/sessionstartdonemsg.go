package harness

import "github.com/mattdurham/wllr/sdk"

// sessionStartDoneMsg is returned after EventSessionStart has been dispatched to
// all extensions. It is distinct from ExtensionEventResultMsg so the harness can
// inject the default action prompt exactly once, after all session_start handlers
// have had a chance to register tools and commands.
type sessionStartDoneMsg struct {
	Err     error
	Results []sdk.EventResponse
}
