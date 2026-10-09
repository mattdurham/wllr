package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// MultiplexerOptions configures a Multiplexer.
type MultiplexerOptions struct {
	// Follow is the per-instance follower configuration (backoff, resume
	// cursor, connect/error hooks).
	Follow FollowOptions
	// Buffer is the merged channel's capacity. A consumer that falls behind
	// applies backpressure to the follower goroutines rather than dropping
	// events, so the buffer bounds how much slack a slow consumer has.
	// Defaults to DefaultMergeBuffer.
	Buffer int
}
