package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// ProviderRequestInterceptor transforms or blocks an outgoing provider request
// just before an agent turn streams to the LLM. It is the agent-side hook for
// the before_provider_request interceptor chain; the harness installs an
// implementation that routes to the extension host's DispatchEventChain.
//
// Given the agent ID, the messages about to be sent, and the model that would
// be used, it returns the (possibly transformed) messages and model, whether
// the request is blocked, and a block reason. A nil interceptor, or a return of
// the same messages/model with blocked=false, leaves the turn unchanged.
//
// Implementations run on the agent's turn goroutine and must be safe to call
// concurrently across agents.
type ProviderRequestInterceptor func(
	agentID string,
	messages []sdk.Message,
	model string,
) (outMessages []sdk.Message, outModel string, blocked bool, reason string)

// ProviderRequestBlockedError is returned from a turn when an interceptor blocks
// the provider request. It carries the reason supplied by the interceptor.
type ProviderRequestBlockedError struct {
	Reason string
}

func (e *ProviderRequestBlockedError) Error() string {
	if e.Reason == "" {
		return "provider request blocked by interceptor"
	}
	return "provider request blocked: " + e.Reason
}
