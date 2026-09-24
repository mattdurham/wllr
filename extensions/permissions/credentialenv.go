package main

import "strings"

// credentialEnvVars lists the environment variable names that hold AWS secret
// material. A command naming any of them is refused outright, regardless of
// configuration: the agent has no reason to spell these out, and naming them is
// the first step of copying the value somewhere it can be read.
//
// Profile and path variables (AWS_PROFILE, AWS_SHARED_CREDENTIALS_FILE,
// AWS_CONFIG_FILE, AWS_WEB_IDENTITY_TOKEN_FILE) are deliberately absent. They
// select or locate credentials rather than carry them, so matching them would
// block ordinary workflows such as `AWS_PROFILE=prod aws s3 ls` while protecting
// nothing secret.
var credentialEnvVars = []string{
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"AWS_SECURITY_TOKEN",
}

// findCredentialEnvVar reports the first AWS credential variable named in
// command, if any. Matching is case-insensitive so that a lowercased pattern
// such as `env | grep aws_secret_access_key` is caught too.
func findCredentialEnvVar(command string) (string, bool) {
	lowered := strings.ToLower(command)
	for _, name := range credentialEnvVars {
		if strings.Contains(lowered, strings.ToLower(name)) {
			return name, true
		}
	}
	return "", false
}
