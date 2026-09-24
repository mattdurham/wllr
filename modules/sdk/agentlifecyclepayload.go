package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentLifecyclePayload describes an agent being added to or removed from the
// host's agent pool. Live is the number of agents remaining after the change.
type AgentLifecyclePayload struct {
	AgentID string `json:"agent_id"`
	Live    int64  `json:"live"`
	Main    bool   `json:"main"`
	Spawned bool   `json:"spawned"`
}
