package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// toolResult holds the result of a tool execution sent back by an extension.
type toolResult struct {
	Result  string
	IsError bool
}
