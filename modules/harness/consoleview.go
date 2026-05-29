package harness

type ConsoleView struct {
	lines [consoleRingSize]string
	head  int
	count int
}
