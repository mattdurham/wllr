package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
