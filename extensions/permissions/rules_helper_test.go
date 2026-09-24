package main

// Helpers for building typed rules in tests. The bare-string config form maps
// to a rule with an empty message; the object form carries one.

func cmdRule(command string) CommandRule {
	return CommandRule{Command: command}
}

func cmdMsg(command, message string) CommandRule {
	return CommandRule{Command: command, Message: message}
}

func envRule(name string) EnvVarRule {
	return EnvVarRule{EnvVar: name}
}

func envMsg(name, message string) EnvVarRule {
	return EnvVarRule{EnvVar: name, Message: message}
}

func pathRule(path string) PathRule {
	return PathRule{Path: path}
}

func pathMsg(path, message string) PathRule {
	return PathRule{Path: path, Message: message}
}

// awsDenyEnvVars mirrors a typical user configuration: the secret-bearing AWS
// variables listed under exec.deny_env_vars.
var awsDenyEnvVars = []EnvVarRule{
	envRule("AWS_ACCESS_KEY_ID"),
	envRule("AWS_SECRET_ACCESS_KEY"),
	envRule("AWS_SESSION_TOKEN"),
	envRule("AWS_SECURITY_TOKEN"),
}
