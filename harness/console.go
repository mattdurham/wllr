package harness

import "strings"

const consoleRingSize = 200

type ConsoleView struct {
	lines [consoleRingSize]string
	head  int
	count int
}

func NewConsoleView() ConsoleView {
	return ConsoleView{}
}
func (c *ConsoleView) Append(line string) {
	c.lines[c.head] = line
	c.head = (c.head + 1) % consoleRingSize
	if c.count < consoleRingSize {
		c.count++
	}
}
func (c *ConsoleView) Clear() {
	c.count = 0
	c.head = 0
}
func (c *ConsoleView) IsEmpty() bool {
	return c.count == 0
}
func (c *ConsoleView) View(width, height int) string {
	if height <= 0 || c.count == 0 {
		return ""
	}
	show := height
	if show > c.count {
		show = c.count
	}
	start := ((c.head - show) + consoleRingSize) % consoleRingSize
	sb := strings.Builder{}
	for i := 0; i < show; i++ {
		idx := (start + i) % consoleRingSize
		line := c.lines[idx]
		runes := []rune(line)
		if width > 0 && len(runes) > width {
			runes = runes[:width]
		}
		sb.WriteString(string(runes))
		sb.WriteByte('\n')
	}
	return sb.String()
}
