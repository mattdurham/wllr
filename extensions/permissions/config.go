package main

import (
	"encoding/json"
	"fmt"
)

// Config holds the permission rules loaded from the extension config.
type Config struct {
	Read  PathRules `json:"read"`
	Write PathRules `json:"write"`
	Exec  ExecRules `json:"exec"`
}

// CommandRule is one entry of exec.deny_commands: either a bare string
// ("sed") or an object carrying the command pattern plus an optional message
// returned verbatim in the denial:
//
//   - command: sed
//     message: "sed is not permitted here; use the built-in file editing tools."
type CommandRule struct {
	Command string
	Message string
}

// EnvVarRule is one entry of exec.deny_env_vars: a bare variable name or an
// object with `env_var` plus an optional denial message.
type EnvVarRule struct {
	EnvVar  string
	Message string
}

// PathRule is one entry of read.deny / write.deny: a bare path pattern or an
// object with `path` plus an optional denial message.
type PathRule struct {
	Path    string
	Message string
}

func (r *CommandRule) UnmarshalJSON(data []byte) error {
	return decodeRule(data, "command", &r.Command, &r.Message)
}

func (r *EnvVarRule) UnmarshalJSON(data []byte) error {
	return decodeRule(data, "env_var", &r.EnvVar, &r.Message)
}

func (r *PathRule) UnmarshalJSON(data []byte) error {
	return decodeRule(data, "path", &r.Path, &r.Message)
}

// decodeRule accepts a rule as either a JSON string (the bare match pattern,
// no message) or an object carrying the family's match key plus an optional
// message. A malformed object, or one missing its match key, decodes to an
// empty match — inert rather than fatal — so a single bad rule cannot wipe the
// whole policy (a config error makes loadConfig fall back to permissive
// defaults). A non-string message is ignored.
func decodeRule(data []byte, matchKey string, match, message *string) error {
	*match = ""
	*message = ""
	i := 0
	for i < len(data) && isJSONSpace(data[i]) {
		i++
	}
	if i >= len(data) {
		return fmt.Errorf("empty rule")
	}
	switch data[i] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*match = s
		return nil
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil // inert: malformed object rule
		}
		if raw, ok := obj[matchKey]; ok {
			_ = json.Unmarshal(raw, match) // non-string match → stays ""
		}
		if raw, ok := obj["message"]; ok {
			_ = json.Unmarshal(raw, message) // non-string message → stays ""
		}
		return nil
	default:
		return fmt.Errorf("rule must be a string or an object")
	}
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
