package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "charm.land/bubbles/v2/textarea"

// InputArea wraps a textarea and handles command detection.
type InputArea struct {
	ta         textarea.Model
	width      int
	lastWasEsc bool
}
