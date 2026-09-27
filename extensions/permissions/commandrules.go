package main

import (
	"path/filepath"
	"strings"
)

// ExecRules contains only user-configured command policy. Empty rules allow all
// commands, so a command is refused only by something the user configured:
// an executable in DenyCommands, one missing from a non-empty AllowCommands,
// a shell operator when DenyShellOperators is set, or an environment variable
// named in DenyEnvVars (see findDeniedEnvVar).
type ExecRules struct {
	AllowCommands      []string      `json:"allow_commands"`
	DenyCommands       []CommandRule `json:"deny_commands"`
	DenyEnvVars        []EnvVarRule  `json:"deny_env_vars"`
	DenyShellOperators bool          `json:"deny_shell_operators"`
}

func checkCommandPermission(command string, rules ExecRules) (bool, string) {
	if name, msg, found := findDeniedEnvVar(command, rules.DenyEnvVars); found {
		if msg != "" {
			return false, msg
		}
		return false, "command references denied environment variable " + name
	}
	if rules.DenyShellOperators && hasShellOperator(command) {
		return false, "shell operators are not allowed"
	}
	commands := commandNames(command)
	if len(commands) == 0 {
		return len(rules.AllowCommands) == 0, "no executable command found"
	}
	for _, name := range commands {
		if rule, ok := matchCommandRule(name, rules.DenyCommands); ok {
			if rule.Message != "" {
				return false, rule.Message
			}
			return false, "command " + name + " is denied"
		}
		if len(rules.AllowCommands) > 0 && !matchesCommandRule(name, rules.AllowCommands) {
			return false, "command " + name + " is not allowlisted"
		}
	}
	return true, ""
}

// commandNames finds the executable at the start of each simple command. It
// handles common separators and wrappers without attempting full shell parsing.
func commandNames(command string) []string {
	names := make([]string, 0)
	for _, segment := range splitShellSegments(command) {
		fields := strings.Fields(segment)
		for len(fields) > 0 {
			field := strings.Trim(fields[0], "'\"")
			if strings.Contains(field, "=") && !strings.HasPrefix(field, "=") {
				fields = fields[1:]
				continue
			}
			if field == "env" || field == "command" || field == "sudo" || field == "--" ||
				strings.HasPrefix(field, "-") {
				fields = fields[1:]
				continue
			}
			name := filepath.Base(field)
			if name != "" && name != "." && name != string(filepath.Separator) {
				names = append(names, name)
			}
			break
		}
	}
	return names
}

func matchesCommandRule(name string, rules []string) bool {
	for _, rule := range rules {
		rule = filepath.Base(strings.Trim(strings.TrimSpace(rule), "'\""))
		if rule == "*" || strings.EqualFold(name, rule) {
			return true
		}
		if matched, _ := filepath.Match(rule, name); matched {
			return true
		}
	}
	return false
}

// matchCommandRule returns the first deny rule whose pattern matches name, so
// the caller can use the rule's message when set. Normalization mirrors
// matchesCommandRule. An empty pattern (an object rule missing its match key)
// never matches — the rule is inert.
func matchCommandRule(name string, rules []CommandRule) (CommandRule, bool) {
	for _, rule := range rules {
		pattern := filepath.Base(strings.Trim(strings.TrimSpace(rule.Command), "'\""))
		if pattern == "" {
			continue
		}
		if pattern == "*" || strings.EqualFold(name, pattern) {
			return rule, true
		}
		if matched, _ := filepath.Match(pattern, name); matched {
			return rule, true
		}
	}
	return CommandRule{}, false
}

func hasShellOperator(command string) bool {
	quote := rune(0)
	escaped := false
	for _, r := range command {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if strings.ContainsRune(";&|<>\n", r) {
			return true
		}
	}
	return false
}

func splitShellSegments(command string) []string {
	segments := make([]string, 0, 1)
	runes := []rune(command)
	start := 0
	quote := rune(0)
	escaped := false
	for i, r := range runes {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if !strings.ContainsRune(";|&\n", r) {
			continue
		}
		if segment := strings.TrimSpace(string(runes[start:i])); segment != "" {
			segments = append(segments, segment)
		}
		// A doubled operator (&&, ||) leaves the second rune to start the next
		// segment, which is empty and therefore skipped.
		start = i + 1
	}
	if segment := strings.TrimSpace(string(runes[start:])); segment != "" {
		segments = append(segments, segment)
	}
	return segments
}
