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

	// A wholly-trailing "\n\n" always yields 2 blank entries from
	// strings.Split (nothing follows to break up the run) — that's an
	// artifact of counting via Split, not the excessive-gap bug this test
	// guards against. Trailing newlines are intentionally left untrimmed at
	// render time (trimmed only once text is truly final, at the producer),
	// so a stream in progress never has a real paragraph break dropped; see
	// collapseBlankLineRuns in scene.go. A run of 3+ (collapsing to more than
	// 2 blank entries) would still indicate the original bug.
	if maxConsecutiveBlanks > 2 {
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

	// See the matching comment in scenario 1: a wholly-trailing "\n\n" yields
	// 2 blank entries via strings.Split, which is not the excessive-gap bug
	// this test guards against (trailing newlines are trimmed at the
	// producer once text is final, not at render time).
	if maxConsecutiveBlanks2 > 2 {
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
// cause blank lines. Trailing newlines are intentionally left untrimmed at
// render time (see collapseBlankLineRuns / renderNode in scene.go) so a
// stream in progress never has a real paragraph break dropped before more
// text arrives; trimming happens once at the producer when text is final.
// wantMaxBlank reflects that: it bounds excessive (3+) runs via
// collapseBlankLineRuns, not trailing whitespace, which strings.Split
// unavoidably reports as one blank entry per literal trailing newline.
func TestWrapEdgeCases(t *testing.T) {
	testCases := []struct {
		name         string
		input        string
		width        int
		wantMaxBlank int
	}{
		{"empty string with newline", "\n", 40, 2},
		{"multiple newlines", "\n\n\n", 40, 3}, // collapses to "\n\n" (1 blank line), still 3 Split entries
		{"trailing newline", "text\n", 40, 1},
		{"trailing double newline", "text\n\n", 40, 2},
		{"wrap at boundary", "hello world", 11, 0}, // exactly fits
		{"wrap with trailing newline", "hello world\n", 11, 1},
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

			if blankCount > tc.wantMaxBlank {
				t.Errorf("Expected <=%d blank lines, got %d. Output: %q", tc.wantMaxBlank, blankCount, out)
			}

			t.Logf("%s: blank lines=%d, output:\n%s", tc.name, blankCount, out)
		})
	}
}

// TestStreamingParagraphBreakNotDroppedMidStream reproduces the regression
// this commit fixes: a paragraph break ("\n\n") split across two separate
// append_text chunks must not be lost from the rendered node just because a
// render happens to land in the gap between the two chunks arriving —
// trimming trailing newlines at render time (the pre-fix behavior) would
// silently drop it, cramming the next chunk's text against the previous
// paragraph until more text arrived to "restore" it.
func TestStreamingParagraphBreakNotDroppedMidStream(t *testing.T) {
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "a1", Type: sdk.UINodeText, Text: "First paragraph.\n\n"}},
	}}); err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	// A render lands right after the paragraph break arrives, before the next
	// chunk of text ("Second paragraph.") has streamed in.
	out := s.Render("chat", 80)
	if !strings.HasSuffix(out, "\n\n") {
		t.Fatalf("paragraph break was dropped mid-stream: rendered %q, want trailing \"\\n\\n\" preserved", out)
	}

	// The rest of the response streams in; the final render must still show
	// the paragraph break between the two paragraphs.
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "chat", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "a1", Text: "Second paragraph."},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	final := s.Render("chat", 80)
	if !strings.Contains(final, "First paragraph.\n\nSecond paragraph.") {
		t.Errorf("final render lost the paragraph break: %q", final)
	}
}
