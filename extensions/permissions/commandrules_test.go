package main

import (
	"strings"
	"testing"
)

func TestCheckCommandPermission(t *testing.T) {
	tests := []struct {
		name    string
		command string
		rules   ExecRules
		allow   bool
	}{
		{name: "empty config remains permissive", command: "sed -i file", allow: true},
		{name: "denies executable", command: "sed -i file", rules: ExecRules{DenyCommands: []CommandRule{cmdRule("sed")}}},
		{
			name:    "denies executable path",
			command: "/usr/bin/sed -i file",
			rules:   ExecRules{DenyCommands: []CommandRule{cmdRule("sed")}},
		},
		{
			name:    "denies command in pipeline",
			command: "cat file | sed -n 1p",
			rules:   ExecRules{DenyCommands: []CommandRule{cmdRule("sed")}},
		},
		{name: "allowlist", command: "go test ./...", rules: ExecRules{AllowCommands: []string{"go"}}, allow: true},
		{name: "allowlist rejects command", command: "sed -n 1p file", rules: ExecRules{AllowCommands: []string{"go"}}},
		{
			name:    "optional shell operator restriction",
			command: "go test ./... && go vet ./...",
			rules:   ExecRules{AllowCommands: []string{"go"}, DenyShellOperators: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, _ := checkCommandPermission(tt.command, tt.rules)
			if allowed != tt.allow {
				t.Fatalf("checkCommandPermission(%q) = %v, want %v", tt.command, allowed, tt.allow)
			}
		})
	}
}

// TestCheckCommandPermissionDenyMessage pins that an object deny rule's message
// replaces the generic denial reason, and that a message-less rule keeps the
// generic wording (issue #49).
func TestCheckCommandPermissionDenyMessage(t *testing.T) {
	const command = "sed -i file"

	allowed, reason := checkCommandPermission(command, ExecRules{
		DenyCommands: []CommandRule{cmdMsg("sed", "sed is not permitted here; use the built-in file editing tools.")},
	})
	if allowed {
		t.Fatalf("checkCommandPermission(%q) allowed, want refused", command)
	}
	if !strings.Contains(reason, "built-in file editing tools") {
		t.Fatalf("reason = %q, want the configured rule message verbatim", reason)
	}
	if strings.Contains(reason, "command sed is denied") {
		t.Fatalf("reason = %q, want the message to replace the generic wording", reason)
	}

	// A bare-string rule has no message and keeps today's wording.
	_, reason = checkCommandPermission(command, ExecRules{DenyCommands: []CommandRule{cmdRule("sed")}})
	if reason != "command sed is denied" {
		t.Fatalf("reason = %q, want the generic wording for a message-less rule", reason)
	}
}

// TestCheckCommandPermissionEnvVarDenyMessage pins the same message propagation
// for deny_env_vars object rules.
func TestCheckCommandPermissionEnvVarDenyMessage(t *testing.T) {
	const command = "echo $AWS_SECRET_ACCESS_KEY"

	allowed, reason := checkCommandPermission(command, ExecRules{
		DenyEnvVars: []EnvVarRule{envMsg("AWS_SECRET_ACCESS_KEY", "Never print AWS secrets.")},
	})
	if allowed {
		t.Fatalf("checkCommandPermission(%q) allowed, want refused", command)
	}
	if reason != "Never print AWS secrets." {
		t.Fatalf("reason = %q, want the configured rule message verbatim", reason)
	}
}

// TestMatchCommandRuleInertRules pins that an object rule missing its match key
// never matches (so it cannot turn into a wildcard deny).
func TestMatchCommandRuleInertRules(t *testing.T) {
	if _, ok := matchCommandRule("sed", []CommandRule{{Message: "oops"}}); ok {
		t.Fatalf("empty-pattern rule matched, want inert")
	}
	if _, ok := matchCommandRule("sed", nil); ok {
		t.Fatalf("no rules matched, want no match")
	}
}

func TestCommandNames(t *testing.T) {
	got := commandNames("FOO=bar env sudo /usr/bin/sed -n 1p; go test ./...")
	want := []string{"sed", "go"}
	if len(got) != len(want) {
		t.Fatalf("commandNames = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commandNames = %#v, want %#v", got, want)
		}
	}
}
