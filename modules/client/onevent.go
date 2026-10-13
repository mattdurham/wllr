package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// OnEvent is the callback a follower invokes for each event. OnEvent is called
// from the follower's goroutine and must not block indefinitely (a slow callback
// stalls only this instance).
type OnEvent func(Tagged)
