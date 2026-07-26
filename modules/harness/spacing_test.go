package harness

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestExcessiveSpacingInRenderedText reproduces issue #26 where text
// rendering produces large blank gaps between lines.
func TestExcessiveSpacingInRenderedText(t *testing.T) {
	// Scenario 1: Simple text with trailing newlines
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}

	testText := "Hello world\n\n"
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: testText}},
	}}); err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	out := s.Render("chat", 40)
	t.Logf("Simple text output:\n%s", out)

	// Check for excessive blank lines
	lines := strings.Split(out, "\n")
	blankCount := 0
	maxConsecutiveBlanks := 0
	currentBlank := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankCount++
			currentBlank++
			if currentBlank > maxConsecutiveBlanks {
				maxConsecutiveBlanks = currentBlank
			}
		} else {
			currentBlank = 0
		}
	}

	t.Logf("Total blank lines: %d, Max consecutive: %d", blankCount, maxConsecutiveBlanks)

	if maxConsecutiveBlanks > 1 {
		t.Errorf("Detected %d consecutive blank lines in simple text render", maxConsecutiveBlanks)
	}

	// Scenario 2: Text with wrap enabled (like chat assistant responses)
	s2 := NewSceneRenderer()
	if err := s2.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}

	wrapText := "This is a longer line of text that should wrap properly without creating extra blank lines when rendered. "

	if err := s2.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{
			ID:    "txt",
			Type:  sdk.UINodeText,
			Text:  wrapText + "\n\n",
			Props: &sdk.UIProps{Wrap: true},
		}},
	}}); err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	out2 := s2.Render("chat", 40)
	t.Logf("Wrapped text output:\n%s", out2)

	lines2 := strings.Split(out2, "\n")
	blankCount2 := 0
	maxConsecutiveBlanks2 := 0

	for _, line := range lines2 {
		if strings.TrimSpace(line) == "" {
			blankCount2++
			maxConsecutiveBlanks2++
		}
	}

	t.Logf("Wrapped - Total blank lines: %d, Max consecutive: %d", blankCount2, maxConsecutiveBlanks2)

	if maxConsecutiveBlanks2 > 1 {
		t.Errorf("Detected %d consecutive blank lines in wrapped text render", maxConsecutiveBlanks2)
	}

	// Scenario 3: Multiple children in a VStack with trailing newlines
	s3 := NewSceneRenderer()
	if err := s3.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}

	if err := s3.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{
			ID:    "root",
			Type:  sdk.UINodeVStack,
			Props: &sdk.UIProps{Border: "rounded"},
			Children: []sdk.UINode{
				{ID: "line1", Type: sdk.UINodeText, Text: "First line\n"},
				{ID: "line2", Type: sdk.UINodeText, Text: "Second line\n"},
			},
		}},
	}}); err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	out3 := s3.Render("chat", 40)
	t.Logf("VStack output:\n%s", out3)

	lines3 := strings.Split(out3, "\n")
	blankCount3 := 0
	maxConsecutiveBlanks3 := 0

	for _, line := range lines3 {
		if strings.TrimSpace(line) == "" {
			blankCount3++
			maxConsecutiveBlanks3++
		}
	}

	t.Logf("VStack - Total blank lines: %d, Max consecutive: %d", blankCount3, maxConsecutiveBlanks3)

	if maxConsecutiveBlanks3 > 1 {
		t.Errorf("Detected %d consecutive blank lines in VStack render", maxConsecutiveBlanks3)
	}
}

// TestAppendTextTrailingNewlines tests that appending text doesn't introduce
// blank lines when the previous content had trailing newlines.
func TestAppendTextTrailingNewlines(t *testing.T) {
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}

	initialText := "Initial text\n"
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: initialText}},
	}}); err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	// Append more text
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "appended text"},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}

	out := s.Render("chat", 40)
	t.Logf("Append output:\n%s", out)

	// Count blank lines
	lines := strings.Split(out, "\n")
	maxConsecutiveBlanks := 0
	currentBlank := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			currentBlank++
			if currentBlank > maxConsecutiveBlanks {
				maxConsecutiveBlanks = currentBlank
			}
		} else {
			currentBlank = 0
		}
	}

	t.Logf("Append - Max consecutive blank lines: %d", maxConsecutiveBlanks)

	if maxConsecutiveBlanks > 1 {
		t.Errorf("Detected %d consecutive blank lines after append", maxConsecutiveBlanks)
	}
}

// TestWrapEdgeCases tests specific edge cases with lipgloss.Wrap that might
// cause blank lines.
func TestWrapEdgeCases(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		width int
	}{
		{"empty string with newline", "\n", 40},
		{"multiple newlines", "\n\n\n", 40},
		{"trailing newline", "text\n", 40},
		{"trailing double newline", "text\n\n", 40},
		{"wrap at boundary", "hello world", 11}, // exactly fits
		{"wrap with trailing newline", "hello world\n", 11},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSceneRenderer()
			if err := s.CreateArea(sdk.UIArea{ID: "test", Placement: sdk.UIAreaMain}); err != nil {
				t.Fatalf("create area: %v", err)
			}

			if err := s.ApplyPatch(sdk.UIPatchParams{Area: "test", Ops: []sdk.UIPatchOp{
				{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{
					ID:    "txt",
					Type:  sdk.UINodeText,
					Text:  tc.input,
					Props: &sdk.UIProps{Wrap: true},
				}},
			}}); err != nil {
				t.Fatalf("apply patch: %v", err)
			}

			out := s.Render("test", tc.width)
			lines := strings.Split(out, "\n")

			blankCount := 0
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					blankCount++
				}
			}

			if blankCount > 1 {
				t.Errorf("Expected <=1 blank line, got %d. Output: %q", blankCount, out)
			}

			t.Logf("%s: blank lines=%d, output:\n%s", tc.name, blankCount, out)
		})
	}
}
