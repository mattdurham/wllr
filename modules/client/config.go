package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "net/http"

// Config describes one wllr instance to connect to.
type Config struct {
	// HTTPClient overrides the default client (timeouts, transport, TLS). The
	// stream uses this client's transport too, with a context-scoped request.
	HTTPClient *http.Client
	// ID identifies the instance in merged output. It must be non-empty and
	// unique across a set of instances; it is the disambiguator when two
	// instances both have an agent named "main".
	ID string
	// BaseURL is the instance's origin, e.g. "http://127.0.0.1:8765". A trailing
	// slash is trimmed.
	BaseURL string
	// Token, when set, is sent as `Authorization: Bearer <token>`.
	Token string
}
