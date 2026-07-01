package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// showThinkingPickerMsg opens the thinking-level picker. Emitted by the
// /thinking command when invoked without an argument.
type showThinkingPickerMsg struct{}

// setThinkingMsg sets the active reasoning level directly. Emitted by
// /thinking <level> and by the thinking picker on selection.
type setThinkingMsg struct {
	Level string
}

// thinkingPickerCallback is the reserved PickerView.Callback value that routes a
// picker selection to the core setThinkingMsg handler instead of dispatching
// EventOnCommand to a WASM extension. The "__wllr:" prefix is reserved for
// core-owned pickers and must not collide with any extension command name.
const thinkingPickerCallback = "__wllr:thinking"
