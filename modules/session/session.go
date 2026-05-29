package session

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// Session manages a single conversation: wiring, lifecycle, turn dispatch.
// It is the single coordinator that knows about agents, extensions, and the UI
// without being tied to bubbletea.
type Session interface {
	// Start fires session_start events and assembles the initial system prompt.
	// Must be called once before Submit.
	Start(ctx context.Context) error

	// Submit sends user input to the main agent. Non-blocking; results arrive
	// via the Renderer callbacks set up in Wire.
	Submit(ctx context.Context, content, display string) error

	// Cancel cancels the active agent turn. No-op if no turn is in progress.
	Cancel()

	// ReloadExtensions hot-reloads all WASM extensions from the given paths.
	ReloadExtensions(ctx context.Context, paths []string) error

	// Close shuts down agents, extensions, and releases resources.
	Close(ctx context.Context) error
}
