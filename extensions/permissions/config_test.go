package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestConfigUnmarshalsBareStringRules pins the legacy wire shape: rules written
// as bare strings keep parsing exactly as before, with empty messages.
func TestConfigUnmarshalsBareStringRules(t *testing.T) {
	const cfg = `{
		"read": {"allow": ["*"]},
		"write": {"allow": ["*"]},
		"exec": {
			"deny_commands": ["sed", "ruby"],
			"deny_env_vars": ["AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"],
			"deny_shell_operators": false
		}
	}`
	var got Config
	if err := json.Unmarshal([]byte(cfg), &got); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	wantCmds := []string{"sed", "ruby"}
	if len(got.Exec.DenyCommands) != len(wantCmds) {
		t.Fatalf("DenyCommands = %#v, want %d rules", got.Exec.DenyCommands, len(wantCmds))
	}
	for i, want := range wantCmds {
		if got.Exec.DenyCommands[i].Command != want {
			t.Fatalf("DenyCommands[%d] = %+v, want command %q", i, got.Exec.DenyCommands[i], want)
		}
		if got.Exec.DenyCommands[i].Message != "" {
			t.Fatalf("DenyCommands[%d].Message = %q, want empty for a bare-string rule", i, got.Exec.DenyCommands[i].Message)
		}
	}
	wantVars := []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	if len(got.Exec.DenyEnvVars) != len(wantVars) {
		t.Fatalf("DenyEnvVars = %#v, want %d rules", got.Exec.DenyEnvVars, len(wantVars))
	}
	for i, want := range wantVars {
		if got.Exec.DenyEnvVars[i].EnvVar != want || got.Exec.DenyEnvVars[i].Message != "" {
			t.Fatalf("DenyEnvVars[%d] = %+v, want (%q, no message)", i, got.Exec.DenyEnvVars[i], want)
		}
	}

	// The configured list must actually drive the check end to end.
	allowed, _ := checkCommandPermission("echo $AWS_SECRET_ACCESS_KEY", got.Exec)
	if allowed {
		t.Fatalf("checkCommandPermission allowed a command naming a configured variable")
	}
}

// TestConfigUnmarshalsObjectRulesWithMessages pins the issue-#49 wire shape:
// object rules carry a match key plus an optional message, and bare strings and
// objects mix freely within one list.
func TestConfigUnmarshalsObjectRulesWithMessages(t *testing.T) {
	const cfg = `{
		"read": {
			"allow": ["*"],
			"deny": [
				"~/.aws/",
				{"path": "~/.ssh/", "message": "SSH material is off limits."}
			]
		},
		"write": {
			"deny": [{"path": "~/.config/wllr/", "message": "wllr's own config is read-only for agents."}]
		},
		"exec": {
			"deny_commands": [
				"sed",
				{"command": "ruby", "message": "ruby is not permitted here."},
				{"command": "terraform apply", "message": "Ask a human before applying infrastructure changes."}
			],
			"deny_env_vars": [
				"AWS_SECRET_ACCESS_KEY",
				{"env_var": "GITHUB_TOKEN", "message": "The GitHub token must not be echoed."}
			]
		}
	}`
	var got Config
	if err := json.Unmarshal([]byte(cfg), &got); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	// Mixed exec.deny_commands: bare string first, two object rules after.
	if got.Exec.DenyCommands[0].Command != "sed" || got.Exec.DenyCommands[0].Message != "" {
		t.Fatalf("DenyCommands[0] = %+v, want bare sed with no message", got.Exec.DenyCommands[0])
	}
	if got.Exec.DenyCommands[1].Message != "ruby is not permitted here." {
		t.Fatalf("DenyCommands[1].Message = %q, want the object rule's message", got.Exec.DenyCommands[1].Message)
	}
	if got.Exec.DenyCommands[2].Message != "Ask a human before applying infrastructure changes." {
		t.Fatalf("DenyCommands[2].Message = %q, want the object rule's message", got.Exec.DenyCommands[2].Message)
	}

	// deny_env_vars mixes forms too.
	if got.Exec.DenyEnvVars[0].EnvVar != "AWS_SECRET_ACCESS_KEY" || got.Exec.DenyEnvVars[0].Message != "" {
		t.Fatalf("DenyEnvVars[0] = %+v, want bare var with no message", got.Exec.DenyEnvVars[0])
	}
	if got.Exec.DenyEnvVars[1].Message != "The GitHub token must not be echoed." {
		t.Fatalf("DenyEnvVars[1].Message = %q, want the object rule's message", got.Exec.DenyEnvVars[1].Message)
	}

	// Path deny rules on both read and write.
	if got.Read.Deny[0].Path != "~/.aws/" || got.Read.Deny[0].Message != "" {
		t.Fatalf("Read.Deny[0] = %+v, want bare path with no message", got.Read.Deny[0])
	}
	if got.Read.Deny[1].Message != "SSH material is off limits." {
		t.Fatalf("Read.Deny[1].Message = %q, want the object rule's message", got.Read.Deny[1].Message)
	}
	if got.Write.Deny[0].Message != "wllr's own config is read-only for agents." {
		t.Fatalf("Write.Deny[0].Message = %q, want the object rule's message", got.Write.Deny[0].Message)
	}

	// End-to-end: an object command rule's message reaches the denial reason.
	_, reason := checkCommandPermission("ruby -e 'puts 1'", got.Exec)
	if reason != "ruby is not permitted here." {
		t.Fatalf("reason = %q, want the object rule's message verbatim", reason)
	}

	// End-to-end: a path object rule's message reaches checkPermission. Stub
	// the HOME lookup so tilde patterns expand like they do inside the runtime.
	oldGetEnv := getEnv
	getEnv = func(name string) (string, error) {
		if name == "HOME" {
			return "/Users/x", nil
		}
		return oldGetEnv(name)
	}
	defer func() { getEnv = oldGetEnv }()
	allowed, pathReason := checkPermission("/Users/x/.ssh/id_rsa", got.Read)
	if allowed {
		t.Fatalf("checkPermission allowed ~/.ssh/id_rsa, want refused")
	}
	if pathReason != "SSH material is off limits." {
		t.Fatalf("pathReason = %q, want the object rule's message verbatim", pathReason)
	}
}

// TestDecodeRuleDegenerateForms pins that decodeRule degrades inertly instead of
// failing the whole config: a malformed object or a missing match key yields an
// empty match — which never matches anything, so the rule is inert — and a
// non-string message is ignored. Only a rule that is not a string or object at
// all is rejected.
func TestDecodeRuleDegenerateForms(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantMatch string
		wantMsg   string
		wantErr   bool
	}{
		{name: "bare string", data: `"sed"`, wantMatch: "sed"},
		{
			name:      "object with match and message",
			data:      `{"command": "sed", "message": "no sed"}`,
			wantMatch: "sed",
			wantMsg:   "no sed",
		},
		{name: "object without message", data: `{"command": "sed"}`, wantMatch: "sed"},
		// Missing match key and non-string match both leave the match empty, so
		// the rule never fires; any message payload is irrelevant but may parse.
		{name: "object missing match key is inert", data: `{"message": "oops"}`, wantMsg: "oops"},
		{name: "object with non-string match is inert", data: `{"command": 42}`},
		{name: "object with non-string message is ignored", data: `{"command": "sed", "message": 42}`, wantMatch: "sed"},
		// Truncated JSON cannot be repaired, so the rule stays inert rather than
		// failing the whole config.
		{name: "malformed object is inert", data: `{"command": "sed"`},
		{name: "number rule is rejected", data: `42`, wantErr: true},
		{name: "empty rule is rejected", data: `   `, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var match, message string
			err := decodeRule([]byte(tt.data), "command", &match, &message)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeRule(%s) err = %v, wantErr = %v", tt.data, err, tt.wantErr)
			}
			if match != tt.wantMatch {
				t.Fatalf("decodeRule(%s) match = %q, want %q", tt.data, match, tt.wantMatch)
			}
			if !tt.wantErr && message != tt.wantMsg {
				t.Fatalf("decodeRule(%s) message = %q, want %q", tt.data, message, tt.wantMsg)
			}
		})
	}
}

// TestConfigWithMessagesStillDrivesEnforcement pins that a message-bearing
// config refuses exactly where the bare-string config did — messages change the
// wording, not the enforcement.
func TestConfigWithMessagesStillDrivesEnforcement(t *testing.T) {
	const cfg = `{
		"read": {"allow": ["*"]},
		"write": {"allow": ["*"]},
		"exec": {"deny_commands": [{"command": "sed", "message": "custom message"}]}
	}`
	var got Config
	if err := json.Unmarshal([]byte(cfg), &got); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if allowed, _ := checkCommandPermission("sed -i file", got.Exec); allowed {
		t.Fatalf("message-bearing config allowed a denied command, want refused")
	}
	allowed, reason := checkCommandPermission("go test ./...", got.Exec)
	if !allowed {
		t.Fatalf("message-bearing config refused an unlisted command (reason %q), want allowed", reason)
	}
	if strings.Contains(reason, "custom message") {
		t.Fatalf("allowed command produced a denial message %q", reason)
	}
}
