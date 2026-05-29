package harness

import "charm.land/bubbles/v2/textarea"

// InputArea wraps a textarea and handles command detection.
type InputArea struct {
	ta         textarea.Model
	width      int
	lastWasEsc bool
}
