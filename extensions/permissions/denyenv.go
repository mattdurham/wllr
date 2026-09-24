package main

import "strings"

// findDeniedEnvVar reports the first name in denyVars that is mentioned
// anywhere in command, if any. Matching is case-insensitive so that a
// lowercased pattern such as `env | grep aws_secret_access_key` is caught too,
// and it deliberately looks at the whole command text rather than only
// `$NAME` expansions so that `printenv NAME` and the assignment form
// (`NAME=... go test`) are refused as well.
//
// The list comes from the user's config (exec.deny_env_vars), so an empty list
// disables the check entirely. That keeps it consistent with the other exec
// rules, where an empty rule set allows everything, and makes the policy
// visible and editable in the same place as deny_commands.
func findDeniedEnvVar(command string, denyVars []string) (string, bool) {
	if len(denyVars) == 0 {
		return "", false
	}
	lowered := strings.ToLower(command)
	for _, name := range denyVars {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.Contains(lowered, strings.ToLower(name)) {
			return name, true
		}
	}
	return "", false
}
