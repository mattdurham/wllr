package harness

// contextmodal_test.go covers the /context modal rendering: section layout,
// the reported-vs-estimate reconciliation note, and number formatting.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestRenderContextBreakdown_Empty(t *testing.T) {
	out := renderContextBreakdown(agent.ContextBreakdown{ContextWindow: 200_000})
	for _, want := range []string{
		"Context window: 200,000 tokens",
		"No completed turn yet",
		"Next request estimate",
		"estimated total   0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Gap of") {
		t.Errorf("reconciliation gap should not appear with no reported usage:\n%s", out)
	}
}

func TestRenderContextBreakdown_Full(t *testing.T) {
	b := agent.ContextBreakdown{
		ContextWindow:    200_000,
		SystemTokens:     2_100,
		ToolTokens:       9_800,
		ToolCount:        23,
		HistoryTokens:    68_000,
		UserTokens:       21_000,
		AssistantTokens:  47_000,
		TurnCount:        41,
		FilteredTokens:   500,
		CanonicalChars:   412_300,
		CanonicalEntries: 138,
		ToolsByTool: []agent.ToolSize{
			{Name: "edit_file", Tokens: 1_800},
			{Name: "bash", Tokens: 1_400},
		},
		LargestMessages: []agent.MessageSize{
			{Role: "user", Tokens: 18_000},
			{Role: "assistant", Tokens: 9_500},
		},
		CanonicalByKind: map[string]int64{
			agent.TranscriptKindMessage:    62_000,
			agent.TranscriptKindToolCall:   30_000,
			agent.TranscriptKindToolResult: 320_300,
		},
		CanonicalMessagesByRole: map[string]int64{
			"user":      21_000,
			"assistant": 41_000,
		},
		CanonicalByTool: []agent.ToolSize{
			{Name: "read_file", Chars: 150_000},
			{Name: "exec", Chars: 80_000},
		},
	}
	b.LastRequest.InputTokens = 84_120
	b.LastRequest.OutputTokens = 1_980
	b.LastRequest.CacheReadTokens = 61_000
	b.LastRequest.CacheCreationTokens = 2_100

	out := renderContextBreakdown(b)
	for _, want := range []string{
		"input 84,120",
		"cache read 61,000",
		"cache write 2,100",
		"output 1,980",
		"42% of window",
		"tools (23)",
		"41 turns: user 21,000, assistant 47,000",
		"largest prompts:",
		"user           18,000",
		"assistant      9,500",
		"filtered (control, not sent) 500",
		"canonical transcript: 412,300 chars across 138 entries",
		"by type:",
		"messages          62,000",
		"user        21,000",
		"assistant   41,000",
		"tool calls        30,000",
		"tool results      320,300",
		"tool traffic by tool:",
		"read_file         150,000",
		"exec              80,000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	// Ownerless tool rows keep the flat listing.
	for _, want := range []string{
		"edit_file         1,800",
		"bash              1,400",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	// 80k estimated vs 84k reported is a small gap — no reconciliation note.
	if strings.Contains(out, "Gap of") {
		t.Errorf("small estimate gap should not trigger the reconciliation note:\n%s", out)
	}
}

func TestRenderContextBreakdown_TranscriptToolListCap(t *testing.T) {
	// More tools than the display cap: the first 10 render, then an "and N
	// more" line instead of an unbounded wall of tools.
	var byTool []agent.ToolSize
	for i := 0; i < 14; i++ {
		byTool = append(byTool, agent.ToolSize{
			Name:  fmt.Sprintf("tool_%02d", i),
			Chars: int64(1000 - i),
		})
	}
	b := agent.ContextBreakdown{
		CanonicalChars:   10_000,
		CanonicalEntries: 14,
		CanonicalByKind:  map[string]int64{agent.TranscriptKindToolResult: 10_000},
		CanonicalByTool:  byTool,
	}
	out := renderContextBreakdown(b)
	for _, want := range []string{"tool_00", "tool_09", "… and 4 more tools"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tool_10") || strings.Contains(out, "tool_13") {
		t.Errorf("tools beyond the cap must not render:\n%s", out)
	}
}

func TestRenderContextBreakdown_LegacySnapshotNoAttribution(t *testing.T) {
	// A snapshot taken before per-kind attribution exists carries nil maps:
	// the modal must show totals only, without by-type/by-tool sections.
	b := agent.ContextBreakdown{CanonicalChars: 500, CanonicalEntries: 2}
	out := renderContextBreakdown(b)
	if strings.Contains(out, "by type:") || strings.Contains(out, "tool traffic") {
		t.Errorf("nil attribution maps must not render sections:\n%s", out)
	}
	if !strings.Contains(out, "canonical transcript: 500 chars across 2 entries") {
		t.Errorf("totals must still render:\n%s", out)
	}
}

func TestRenderContextBreakdown_ReconciliationNote(t *testing.T) {
	// Reported input far above the estimate sum: the gap note must appear and
	// explain the transient within-turn tool traffic.
	b := agent.ContextBreakdown{
		ContextWindow: 200_000,
		SystemTokens:  2_100,
		ToolTokens:    9_800,
		ToolCount:     23,
		HistoryTokens: 8_000,
	}
	b.LastRequest.InputTokens = 120_000

	out := renderContextBreakdown(b)
	if !strings.Contains(out, "Gap of ~100,100 tokens") {
		t.Errorf("expected gap note, got:\n%s", out)
	}
	if !strings.Contains(out, "within-turn tool traffic") {
		t.Errorf("expected tool-traffic explanation, got:\n%s", out)
	}
}

func TestRenderContextBreakdown_SeedLabeledNotProviderReported(t *testing.T) {
	// A fresh session's LastRequest is the chars/4 baseline seed: the modal
	// must label it as an estimate, never "provider-reported".
	b := agent.ContextBreakdown{ContextWindow: 200_000, LastRequestIsEstimate: true}
	b.LastRequest.InputTokens = 1_234
	out := renderContextBreakdown(b)
	if !strings.Contains(out, "chars/4 baseline") {
		t.Errorf("seeded LastRequest must be labeled as baseline:\n%s", out)
	}
	if strings.Contains(out, "provider-reported") {
		t.Errorf("seeded LastRequest must not claim provider-reported:\n%s", out)
	}
	if !strings.Contains(out, "input 1,234") {
		t.Errorf("seed value must still render:\n%s", out)
	}
}

func TestComma(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		999:       "999",
		1_000:     "1,000",
		84_120:    "84,120",
		1_234_567: "1,234,567",
		-42:       "-42",
	}
	for in, want := range cases {
		if got := comma(in); got != want {
			t.Errorf("comma(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderContextBreakdown_SystemComponents(t *testing.T) {
	b := agent.ContextBreakdown{
		ContextWindow: 200_000,
		SystemTokens:  625,
		SystemComponents: []sdk.SystemPromptComponent{
			{Source: "built-in rules", Chars: 1600},
			{Source: "file:/Users/me/proj/AGENTS.md", Chars: 400},
			{Source: "cwd note", Chars: 100},
			{Source: "skills", Chars: 400},
		},
	}
	out := renderContextBreakdown(b)
	for _, want := range []string{
		"by source:",
		// %-30s padding + separator: 14-char source → 17 spaces before the value.
		"built-in rules                 400",
		"file:/Users/me/proj/AGENTS.md  100",
		"cwd note                       25",
		"skills                         100",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}

	// Arrival order, not sorted: the ledger reflects how the prompt reads.
	builtIn := strings.Index(out, "built-in rules")
	skills := strings.Index(out, "skills ")
	if builtIn == -1 || skills == -1 || builtIn > skills {
		t.Errorf("components out of arrival order:\n%s", out)
	}

	// Empty ledger renders nothing (plain total only).
	b.SystemComponents = nil
	out = renderContextBreakdown(b)
	if strings.Contains(out, "by source:") {
		t.Errorf("empty ledger should not render a by-source section:\n%s", out)
	}
}

func TestRenderContextBreakdown_ToolsByExtension(t *testing.T) {
	b := agent.ContextBreakdown{
		ContextWindow: 200_000,
		ToolTokens:    3_200,
		ToolCount:     4,
		ToolsByTool: []agent.ToolSize{
			{Name: "edit_file", Tokens: 1_800}, // harness-native
			{Name: "lsp_diagnostics", Extension: "lsp", Tokens: 900},
			{Name: "queue_peek", Extension: "queue", Tokens: 300},
			{Name: "get_skill", Extension: "skills", Tokens: 200},
		},
	}
	out := renderContextBreakdown(b)
	// Heaviest extension first with a subtotal, tools indented within it.
	// %-17s padding + separator spacing, harness-native tools grouped under
	// the "harness" header.
	for _, want := range []string{
		"harness           1,800",
		"edit_file         1,800",
		"lsp               900",
		"lsp_diagnostics   900",
		"queue             300",
		"skills            200",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	// Subtotal ordering: harness (1800) before lsp (900) before queue (300)
	// before skills (200).
	positions := []int{
		strings.Index(out, "harness "),
		strings.Index(out, "lsp "),
		strings.Index(out, "queue "),
		strings.Index(out, "skills "),
	}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] == -1 || positions[i] == -1 || positions[i-1] > positions[i] {
			t.Errorf("extension groups out of subtotal order at %v:\n%s", positions, out)
		}
	}
}
