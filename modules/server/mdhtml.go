package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// mdhtml.go renders a chat message's content to safe HTML for the browser
// transcript.
//
// It is deliberately the *same* pipeline the terminal transcript uses, not a
// second markdown implementation: assistant content goes through
// modules/sdk/md — the renderer the bundled agents extension calls via
// format_markdown — and the ANSI it produces is translated to spans by
// ansihtml.go. That keeps one definition of "what markdown means" across the
// TUI and the browser, so a message cannot render differently in each.
//
// User content is escaped verbatim, not markdown-rendered, matching the
// extension, which shows prompts exactly as typed.

import (
	"html"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/sdk/md"
)

// MessageHTML renders one message body to HTML that is safe to insert as
// markup. Both branches escape their text (md output is escaped by ANSItoHTML,
// user text by html.EscapeString), so model or user output cannot inject tags.
func MessageHTML(role sdk.Role, content string) string {
	if content == "" {
		return ""
	}
	if role == sdk.RoleUser {
		return html.EscapeString(content)
	}
	return ANSItoHTML(md.Render(content))
}
