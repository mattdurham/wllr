package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// FollowOptions configures Follow.
type FollowOptions struct {
	// OnConnect, when non-nil, is called after each successful (re)connection
	// with the stream's reported protocol version. Returning an error aborts the
	// follower, so a caller can enforce a version gate.
	OnConnect func(protocol int) error
	// OnError, when non-nil, is called on each connection fault before the
	// backoff wait. It does not stop the follower.
	OnError func(error)
	// Backoff overrides the reconnect schedule. The zero value uses the
	// defaults (Base=500ms, Max=30s, 20% jitter).
	Backoff Backoff
	// Since resumes from a sequence cursor: the instance replays events with
	// Seq > Since. Zero means "from the backlog start".
	Since int64
}
