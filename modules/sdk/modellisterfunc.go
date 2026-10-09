package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ModelListerFunc adapts a function to ModelLister.
type ModelListerFunc func() []ModelChoice

// ModelChoices implements ModelLister.
func (f ModelListerFunc) ModelChoices() []ModelChoice { return f() }
