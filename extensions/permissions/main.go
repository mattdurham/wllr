//go:build wasip1

package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Config holds the permission rules loaded from the extension config.
type Config struct {
	Read  PathRules `json:"read"`
	Write PathRules `json:"write"`
}

// PathRules holds allow and deny lists for a permission type.
type PathRules struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

var config Config

func init() {
	// Load configuration from the host.
	if err := loadConfig(); err != nil {
		Logf("error", "permissions: failed to load config: %v", err)
		// Default to permissive mode if config fails to load.
		config = Config{
			Read:  PathRules{Allow: []string{"*"}},
			Write: PathRules{Allow: []string{"*"}},
		}
	}

	// Set the event handler.
	OnEvent = handleEvent

	// Subscribe to before_tool_call to intercept read_file and write_file.
	Subscribe("before_tool_call")

	SetStatus("permissions", "active")
	Logf("info", "permissions: initialized (read allow=%v deny=%v, write allow=%v deny=%v)",
		config.Read.Allow, config.Read.Deny, config.Write.Allow, config.Write.Deny)
}

// loadConfig reads the extension configuration from the host.
func loadConfig() error {
	data, err := ConfigRead()
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "{}" {
		// No config provided; use defaults.
		config = Config{
			Read:  PathRules{Allow: []string{"*"}},
			Write: PathRules{Allow: []string{"*"}},
		}
		return nil
	}
	return json.Unmarshal(data, &config)
}

// handleEvent is called by the host for subscribed events.
func handleEvent(evt Event) *EventResponse {
	if evt.Type != "before_tool_call" {
		return nil
	}

	var payload struct {
		ToolCallID string          `json:"tool_call_id"`
		ToolName   string          `json:"tool_name"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		Logf("error", "permissions: unmarshal before_tool_call: %v", err)
		return nil
	}

	// Only intercept read_file and write_file.
	var rules PathRules
	switch payload.ToolName {
	case "read_file":
		rules = config.Read
	case "write_file":
		rules = config.Write
	default:
		// Not a file operation — allow.
		return nil
	}

	// Extract path from input.
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(payload.Input, &input); err != nil {
		Logf("error", "permissions: unmarshal %s input: %v", payload.ToolName, err)
		return nil
	}

	path := input.Path
	// Clean and resolve the path.
	path = filepath.Clean(path)

	// Check permission.
	allowed := checkPermission(path, rules)
	if !allowed {
		Logf("warn", "permissions: blocked %s to %s", payload.ToolName, path)
		// Return a tool_result error immediately to block the operation.
		ToolResult(payload.ToolCallID, "Permission denied: "+path, true)
		// Return a response that blocks the event from proceeding.
		return &EventResponse{Block: true}
	}

	Logf("debug", "permissions: allowed %s to %s", payload.ToolName, path)
	return nil
}

// checkPermission returns true if path is allowed by rules.
// Algorithm:
// 1. If deny list matches, reject.
// 2. If allow list matches, accept.
// 3. If allow list is empty or contains "*", accept.
// 4. Otherwise reject.
func checkPermission(path string, rules PathRules) bool {
	// Check deny list first.
	for _, pattern := range rules.Deny {
		if matchPath(path, pattern) {
			return false
		}
	}

	// Check allow list.
	if len(rules.Allow) == 0 {
		// No allow rules means allow all (if not denied).
		return true
	}
	for _, pattern := range rules.Allow {
		if matchPath(path, pattern) {
			return true
		}
	}

	// No match in allow list.
	return false
}

// matchPath checks if path matches pattern.
// Patterns can be:
//  - "*" — matches everything
//  - absolute path — exact match or prefix match
//  - path with trailing "/" — prefix match
//  - glob pattern (simple * wildcard)
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

// expandTilde expands ~ to the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	// Get HOME from environment.
	home, err := GetEnv("HOME")
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

func main() {}
