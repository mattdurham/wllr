package sdk

// StatusInfo is returned by the get_status_info host_call.
// It gives extensions a read-only snapshot of the current status bar state
// so they can compose a fully custom status line.
type StatusInfo struct {
	Statuses map[string]string `json:"statuses"`
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Tokens   int               `json:"tokens"`
	Working  bool              `json:"working"`
}
