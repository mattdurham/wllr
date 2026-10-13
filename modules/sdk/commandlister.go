package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// CommandLister returns the registered slash commands for a front end to
// present (autocomplete, help). A nil lister means command discovery is
// unavailable.
type CommandLister interface {
	ListCommands() []CommandInfo
}
