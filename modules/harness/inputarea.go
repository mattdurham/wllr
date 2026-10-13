package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "charm.land/bubbles/v2/textarea"

// maxInputHistory bounds how many submitted messages the input box remembers.
const maxInputHistory = 100

// InputArea wraps a textarea and handles command detection.
type InputArea struct {
	ta         textarea.Model
	width      int
	lastWasEsc bool

	// history is the most recent submitted prompts, oldest first, used by the
	// up/down recall in Update. histIdx indexes the entry currently recalled
	// (-1 when not navigating history); histDraft holds the text that was in
	// the box when recall started so down past the newest entry restores it.
	history   []string
	histIdx   int
	histDraft string
}
