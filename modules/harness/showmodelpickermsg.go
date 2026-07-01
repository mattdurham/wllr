package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// showModelPickerMsg opens the model-selection picker. Emitted by the /model
// command when invoked without an argument.
type showModelPickerMsg struct{}

// modelPickerCallback is the reserved PickerView.Callback value that routes a
// picker selection to the core setModelMsg handler instead of dispatching
// EventOnCommand to a WASM extension. It must not collide with any extension
// command name (the "__wllr:" prefix is reserved for core-owned pickers).
const modelPickerCallback = "__wllr:model"
