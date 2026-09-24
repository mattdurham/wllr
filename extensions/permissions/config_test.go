package main

import (
	"encoding/json"
	"testing"
)

// TestConfigUnmarshalsDenyEnvVars pins the wire shape: the JSON the host returns
// for the "permissions" group (sourced from the shared config file) must populate
// ExecRules.DenyEnvVars, so a config written as exec.deny_env_vars takes effect.
func TestConfigUnmarshalsDenyEnvVars(t *testing.T) {
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
	want := []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	if len(got.Exec.DenyEnvVars) != len(want) {
		t.Fatalf("DenyEnvVars = %#v, want %#v", got.Exec.DenyEnvVars, want)
	}
	for i := range want {
		if got.Exec.DenyEnvVars[i] != want[i] {
			t.Fatalf("DenyEnvVars = %#v, want %#v", got.Exec.DenyEnvVars, want)
		}
	}

	// The configured list must actually drive the check end to end.
	allowed, _ := checkCommandPermission("echo $AWS_SECRET_ACCESS_KEY", got.Exec)
	if allowed {
		t.Fatalf("checkCommandPermission allowed a command naming a configured variable")
	}
}
