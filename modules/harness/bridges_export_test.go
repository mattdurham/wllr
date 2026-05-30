package harness

// Test helpers that expose internal bridge construction to the harness_test package.

import (
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
)

func NewHarnessAgentBridgeFromPool(pool *agent.AgentPool) extension.AgentBridge {
	return &harnessAgentBridge{pool: pool}
}

func NewHarnessAgentBridgeNilPool() extension.AgentBridge {
	return &harnessAgentBridge{pool: nil}
}
