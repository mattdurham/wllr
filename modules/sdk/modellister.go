package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ModelLister returns the selectable models for a front end to present. A nil
// lister means model selection is unavailable.
type ModelLister interface {
	ModelChoices() []ModelChoice
}
