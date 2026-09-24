package main

import "strings"

// findDeniedEnvVar reports the first rule in denyVars whose variable name is
// mentioned anywhere in command, along with the rule's message ("" when the
// rule has none). Matching is case-insensitive so that a lowercased pattern
// such as `env | grep aws_secret_access_key` is caught too, and it deliberately
// looks at the whole command text rather than only `$NAME` expansions so that
// `printenv NAME` and the assignment form (`NAME=... go test`) are refused as
// well.
//
// The list comes from the user's config (exec.deny_env_vars), so an empty list
// disables the check entirely. That keeps it consistent with the other exec
// rules, where an empty rule set allows everything, and makes the policy
// visible and editable in the same place as deny_commands. Rules with an empty
// name (an object rule missing its env_var key) are skipped.
func findDeniedEnvVar(command string, denyVars []EnvVarRule) (name, message string, found bool) {
	if len(denyVars) == 0 {
		return "", "", false
	}
	lowered := strings.ToLower(command)
	for _, rule := range denyVars {
		name := strings.TrimSpace(rule.EnvVar)
		if name == "" {
			continue
		}
		if strings.Contains(lowered, strings.ToLower(name)) {
			return name, rule.Message, true
		}
	}
	return "", "", false
}
