package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/wire"

// Tagged is one instance's event, tagged with the instance it came from. Because
// two instances can both have an agent named "main", the instance ID is the
// only unambiguous key; AgentKey combines the two for a globally unique agent
// identity.
type Tagged struct {
	// Instance is the configured instance ID.
	Instance string
	// Event is the decoded frame. Event.Seq is monotonic within this instance
	// only.
	Event wire.Event
	// Gap is true when this event's Seq did not follow the previous one, so
	// events were missed (a dropped backlog or an overflowed subscriber buffer).
	// A consumer can re-fetch history to resynchronize.
	Gap bool
}

// AgentKey returns a globally unambiguous "<instance>/<agent>" identity for the
// event's agent. It is stable across instances and suitable as a map key.
func (t Tagged) AgentKey() string {
	if t.Event.AgentID == "" {
		return t.Instance
	}
	return t.Instance + "/" + t.Event.AgentID
}
