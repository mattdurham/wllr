package harness

import (
	"charm.land/fantasy"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// sdkToolAdapter adapts an sdk.Tool to the fantasy.AgentTool interface.
// When Run is called by the fantasy agent, it dispatches the tool call
// to the extension host via ExecuteTool and waits for the result.
type sdkToolAdapter struct {
	host            *extension.Host
	params          map[string]any
	providerOptions fantasy.ProviderOptions
	agentID         string
	required        []string
	tool            sdk.Tool
}
