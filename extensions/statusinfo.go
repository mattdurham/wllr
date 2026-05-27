package main

// StatusInfo holds a snapshot of the current status bar state.
// Returned by GetStatusInfo.
type StatusInfo struct {
	Tokens   int               `json:"tokens"`
	Working  bool              `json:"working"`
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Statuses map[string]string `json:"statuses"`
}
