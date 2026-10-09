package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// This file defines the renderer-neutral slash-command contract shared by every
// front end. The interactive TUI builds its autocomplete dropdown from the
// command registry; the HTTP front end serves the same list as JSON so a
// browser can offer an equivalent dropdown. For the two to agree, both must use
// the same command shape and the same rule for when a "/word" is being typed —
// so both live here rather than being reimplemented in each front end.

import "strings"

// CommandInfo is one registered slash command as a front end needs it: the name
// (without the leading slash), a human description, and whether the command runs
// immediately rather than being queued to an extension.
type CommandInfo struct {
	Name    string `json:"name"`
	Desc    string `json:"desc,omitempty"`
	Instant bool   `json:"instant,omitempty"`
}

// SlashWordAt returns the index of the "/" that begins the in-progress command
// word in val, or -1 when the caret is not inside one. A slash word starts at
// position 0 or right after a space, and has no space between it and the end of
// the string (i.e. the command name is still being typed). This is the exact
// rule the terminal's autocomplete uses; the browser reimplements it, so it is
// stated here as the shared contract.
//
// Examples (index returned in brackets):
//
//	"/he"        -> 0
//	"hi /he"     -> 3
//	"/help me"   -> -1  (space after the command name)
//	"http://x"   -> -1  (slash is mid-word)
//	"nothing"    -> -1
func SlashWordAt(val string) int {
	idx := strings.LastIndex(val, "/")
	if idx < 0 {
		return -1
	}
	if idx > 0 && val[idx-1] != ' ' {
		return -1
	}
	if strings.ContainsRune(val[idx+1:], ' ') {
		return -1
	}
	return idx
}

// CommandQuery returns the lower-cased command name being typed in val and
// whether a slash word is present. A front end filters its command list by
// HasPrefix on this query.
func CommandQuery(val string) (query string, ok bool) {
	idx := SlashWordAt(val)
	if idx < 0 {
		return "", false
	}
	return strings.ToLower(val[idx+1:]), true
}
