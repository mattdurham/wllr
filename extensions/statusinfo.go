package main

// StatusInfo holds a snapshot of the current status bar state.
// Returned by GetStatusInfo.
type StatusInfo struct {
	Statuses map[string]string `json:"statuses"`
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Tokens   int               `json:"tokens"`
	Working  bool              `json:"working"`
}
