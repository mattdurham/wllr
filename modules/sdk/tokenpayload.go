package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// TokenPayload is the payload for EventToken. It carries a batch of streamed
// assistant text and the ID of the agent that produced it. Batches are
// coalesced by the harness (~75ms) so the per-token WASM crossing rate stays
// bounded regardless of provider speed.
type TokenPayload struct {
	AgentID string `json:"agent_id"`
	Text    string `json:"text"`
}
