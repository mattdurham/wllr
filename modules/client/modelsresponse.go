package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "context"

// ModelsResponse is the GET /api/models response.
type ModelsResponse struct {
	Current  map[string]string `json:"current"`
	Callback string            `json:"callback"`
	Models   []Model           `json:"models"`
}

// Models lists the models a user can switch to.
func (c *Client) Models(ctx context.Context) (ModelsResponse, error) {
	var m ModelsResponse
	err := c.getJSON(ctx, "/api/models", &m)
	return m, err
}
