package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// This file retains the harness-visible API surface for BuildFantasyTools
// while the implementation has moved to github.com/mattdurham/wllr/tools.

import (
	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/extension"
	htools "github.com/mattdurham/wllr/modules/tools"
)

// BuildFantasyTools delegates to the tools package.
func BuildFantasyTools(extHost *extension.Host, agentID string, logFn func(int, string)) []fantasy.AgentTool {
	return htools.BuildFantasyTools(extHost, agentID, logFn)
}
