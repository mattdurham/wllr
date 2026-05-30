package harness

import (
	"strings"
	"testing"
)

func TestConsoleView_Append_SingleLine(t *testing.T) {
	c := NewConsoleView()
	c.Append("hello")
	got := c.View(80, 3)
	if !strings.Contains(got, "hello") {
		t.Fatal("View(80,3) should contain hello")
	}
}

func TestConsoleView_Append_RingBuffer_EvictsOldest(t *testing.T) {
	c := NewConsoleView()
	c.Append("first")
	for i := 0; i < 199; i++ {
		c.Append("mid")
	}
	c.Append("last")
	got := c.View(80, 201)
	if strings.Contains(got, "first") {
		t.Fatal("first should have been evicted from ring buffer")
	}
	if !strings.Contains(got, "last") {
		t.Fatal("last should appear in view")
	}
}

func TestConsoleView_Clear_EmptiesBuffer(t *testing.T) {
	c := NewConsoleView()
	c.Append("line1")
	c.Clear()
	if !c.IsEmpty() {
		t.Fatal("IsEmpty should be true after Clear")
	}
}

func TestConsoleView_View_WidthClamped(t *testing.T) {
	c := NewConsoleView()
	c.Append(strings.Repeat("x", 100))
	got := c.View(20, 1)
	if strings.Contains(got, strings.Repeat("x", 21)) {
		t.Fatal("View width not clamped: line exceeds width 20")
	}
}

func TestConsoleView_Empty_ViewIsEmpty(t *testing.T) {
	c := NewConsoleView()
	if !c.IsEmpty() {
		t.Fatal("IsEmpty should be true on new ConsoleView")
	}
	got := c.View(80, 0)
	if got != "" {
		t.Fatalf("View(80,0) should be empty, got %q", got)
	}
}

func TestConsoleView_Visible_AfterAppend(t *testing.T) {
	c := NewConsoleView()
	if !c.IsEmpty() {
		t.Fatal("IsEmpty should be true initially")
	}
	c.Append("line1")
	if c.IsEmpty() {
		t.Fatal("IsEmpty should be false after Append")
	}
}
