package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UICreateAreaParams is the params blob for the ui_create_area host_call.
type UICreateAreaParams struct {
	Area UIArea `json:"area"`
}
