package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// HistoryOptions narrows a history fetch. Zero values mean "all".
type HistoryOptions struct {
	Agent  string
	Offset int
	Limit  int
}
