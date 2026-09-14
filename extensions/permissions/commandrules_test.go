package main

import "testing"

func TestCheckCommandPermission(t *testing.T) {
	tests := []struct {
		name    string
		command string
		rules   ExecRules
		allow   bool
	}{
		{name: "empty config remains permissive", command: "sed -i file", allow: true},
		{name: "denies executable", command: "sed -i file", rules: ExecRules{DenyCommands: []string{"sed"}}},
		{name: "denies executable path", command: "/usr/bin/sed -i file", rules: ExecRules{DenyCommands: []string{"sed"}}},
		{name: "denies command in pipeline", command: "cat file | sed -n 1p", rules: ExecRules{DenyCommands: []string{"sed"}}},
		{name: "allowlist", command: "go test ./...", rules: ExecRules{AllowCommands: []string{"go"}}, allow: true},
		{name: "allowlist rejects command", command: "sed -n 1p file", rules: ExecRules{AllowCommands: []string{"go"}}},
		{name: "optional shell operator restriction", command: "go test ./... && go vet ./...", rules: ExecRules{AllowCommands: []string{"go"}, DenyShellOperators: true}},
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
