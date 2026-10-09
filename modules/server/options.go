package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "log/slog"

// Options configures a Server.
type Options struct {
	// Logger receives request and lifecycle logs. Defaults to slog.Default.
	Logger *slog.Logger
	// Addr is the listen address used by Serve. Defaults to loopback only
	// (127.0.0.1:8765) so the API is not exposed on the network unless asked.
	Addr string
	// Token, when non-empty, is required on every request as
	// `Authorization: Bearer <token>` or a `?token=` query parameter. Empty
	// disables auth; combined with a loopback bind that is the default.
	Token string
	// MainID is the root agent's ID. Defaults to agent.MainAgentID.
	MainID string
	// Backlog is the number of recent events retained for replay to clients
	// that pass a `since` cursor. Defaults to DefaultBacklog.
	Backlog int
}
