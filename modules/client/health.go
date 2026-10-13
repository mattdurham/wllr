package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// Health is the GET /api/health response.
type Health struct {
	OK          bool  `json:"ok"`
	Protocol    int   `json:"protocol"`
	LastSeq     int64 `json:"last_seq"`
	Subscribers int   `json:"subscribers"`
	Drops       int64 `json:"drops"`
}

// Health fetches instance health, including the wire protocol version so a
// caller can gate compatibility.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	err := c.getJSON(ctx, "/api/health", &h)
	return h, err
}
