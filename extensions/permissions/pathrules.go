package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PathRules holds allow and deny lists for a permission type. Deny entries
// may carry an optional message returned verbatim in the denial.
type PathRules struct {
	Allow []string   `json:"allow"`
	Deny  []PathRule `json:"deny"`
}

// checkPermission returns whether path is allowed by rules and, when denied,
// the matched deny rule's message ("" when the rule has none).
// Algorithm:
// 1. If deny list matches, reject with the rule's message.
// 2. If allow list matches, accept.
// 3. If allow list is empty or contains "*", accept.
// 4. Otherwise reject.
func checkPermission(path string, rules PathRules) (bool, string) {
	// Check deny list first.
	for _, rule := range rules.Deny {
		if rule.Path == "" {
			continue // inert: object rule missing its match key
		}
		if matchPath(path, rule.Path) {
			return false, rule.Message
		}
	}

	// Check allow list.
	if len(rules.Allow) == 0 {
		// No allow rules means allow all (if not denied).
		return true, ""
	}
	for _, pattern := range rules.Allow {
		if matchPath(path, pattern) {
			return true, ""
		}
	}

	// No match in allow list.
	return false, ""
}

// matchPath checks if path matches pattern.
// Patterns can be:
//   - "*" — matches everything
//   - absolute path — exact match or prefix match
//   - path with trailing "/" — prefix match
//   - glob pattern (simple * wildcard)
func matchPath(path, pattern string) bool {
	if pattern == "*" {
		return true
	}

	// Clean both paths for comparison.
	path = filepath.Clean(path)
	pattern = filepath.Clean(pattern)

	// Expand ~ to home directory.
	path = expandTilde(path)
	pattern = expandTilde(pattern)

	// Check for exact match.
	if path == pattern {
		return true
	}

	// Check for prefix match (pattern ends with /).
	// e.g., /home/user/source/ matches /home/user/source/file.txt
	if strings.HasSuffix(pattern, string(filepath.Separator)) {
		return strings.HasPrefix(path, pattern)
	}

	// Check if path is under pattern directory.
	// e.g., /home/user/source matches /home/user/source/file.txt
	if strings.HasPrefix(path, pattern+string(filepath.Separator)) {
		return true
	}

	// Simple glob matching with * wildcard.
	matched, _ := filepath.Match(pattern, path)
	return matched
}

// getEnv is the HOME lookup used by expandTilde. Outside the wasm runtime
// (native tests) there is no host, so the default reports the variable as
// unavailable; main wires it to the SDK's GetEnv at startup.
var getEnv = func(name string) (string, error) {
	return "", fmt.Errorf("get_env unavailable outside the wasm runtime")
}

// expandTilde expands ~ to the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	// Get HOME from environment.
	home, err := getEnv("HOME")
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
