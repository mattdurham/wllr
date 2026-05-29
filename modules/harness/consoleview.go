package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

type ConsoleView struct {
	lines [consoleRingSize]string
	head  int
	count int
}
