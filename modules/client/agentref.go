package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "strings"

// AgentRef is a globally unambiguous agent identity: an instance plus the agent
// ID within it.
type AgentRef struct {
	Instance string
	Agent    string
}

// String renders the reference as "instance/agent".
func (r AgentRef) String() string {
	if r.Agent == "" {
		return r.Instance
	}
	return r.Instance + "/" + r.Agent
}

// ParseAgentRef splits an "instance/agent" key produced by Tagged.AgentKey back
// into its parts. A key with no separator is treated as an instance with an
// empty agent.
func ParseAgentRef(key string) AgentRef {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return AgentRef{Instance: key[:i], Agent: key[i+1:]}
	}
	return AgentRef{Instance: key}
}
