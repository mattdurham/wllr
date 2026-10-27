package main

import "strings"

// terminalEnvironment supplies Zellij's terminal type when a pane environment
// omits TERM. Bubble Tea still performs color detection, so NO_COLOR,
// TERM=dumb, and explicit terminal capabilities retain their normal meaning.
func terminalEnvironment(environ []string) []string {
	term := ""
	termIndex := -1
	zellij := false
	for i, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "TERM":
			term = value
			termIndex = i
		case "ZELLIJ":
			zellij = value != ""
		}
	}
	if term != "" || !zellij {
		return environ
	}

	env := append([]string(nil), environ...)
	if termIndex >= 0 {
		env[termIndex] = "TERM=xterm-256color"
		return env
	}
	return append(env, "TERM=xterm-256color")
}
