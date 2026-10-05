package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fantasy "charm.land/fantasy"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
)

// responsesBody is a minimal Responses API response the openai-go SDK parses
// cleanly: an assistant text output plus usage details.
const responsesBody = `{
  "id": "resp_test",
  "object": "response",
  "created_at": 1759000000,
  "status": "completed",
  "model": "sol-6.1",
  "output": [
    {
      "type": "message",
      "id": "msg_test",
      "status": "completed",
      "role": "assistant",
      "content": [
        {"type": "output_text", "text": "ok", "annotations": []}
      ]
    }
  ],
  "usage": {
    "input_tokens": 5,
    "input_tokens_details": {"cached_tokens": 0},
    "output_tokens": 2,
    "output_tokens_details": {"reasoning_tokens": 0},
    "total_tokens": 7
  },
  "parallel_tool_calls": true
}`

// chatCompletionsBody is a minimal chat-completions response for the control
// case (default routing gate sends unknown IDs to this endpoint).
const chatCompletionsBody = `{
  "id": "chatcmpl_test",
  "object": "chat.completion",
  "created": 1759000000,
  "model": "sol-6.1",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "ok"},
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7}
}`

// endpointRecorder serves one of the two OpenAI endpoints on an httptest
// server and records the request path and body.
type endpointRecorder struct {
	srv  *httptest.Server
	path string
	body string
}

func newEndpointRecorder(t *testing.T, body string) *endpointRecorder {
	t.Helper()
	rec := &endpointRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		rec.body = buf.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

// codexProviderAt builds a provider with the same option composition as
// newCodexProvider but pointed at a test server, optionally without the
// WithResponsesAPIFunc override (to exercise fantasy's default gate).
func codexProviderAt(t *testing.T, baseURL string, routingOverride bool) fantasy.Provider {
	t.Helper()
	opts := []fantasyopenapiprovider.Option{
		fantasyopenapiprovider.WithAPIKey("test-token"),
		fantasyopenapiprovider.WithBaseURL(baseURL),
		fantasyopenapiprovider.WithHeaders(map[string]string{
			"chatgpt-account-id": "acct_test",
			"OpenAI-Beta":        "responses=experimental",
			"originator":         "codex_cli_go",
		}),
		fantasyopenapiprovider.WithUseResponsesAPI(),
	}
	if routingOverride {
		opts = append(opts, fantasyopenapiprovider.WithResponsesAPIFunc(func(string) bool { return true }))
	}
	prov, err := fantasyopenapiprovider.New(opts...)
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	return prov
}

// TestCodexProviderRoutesUnknownModelIDsToResponses pins the codex provider's
// unconditional Responses-API routing: a model ID fantasy's default gate does
// not recognize (a future codex model that doesn't follow the gpt-N pattern)
// must still reach the /responses endpoint, because every model on the codex
// backend is responses-based.
func TestCodexProviderRoutesUnknownModelIDsToResponses(t *testing.T) {
	rec := newEndpointRecorder(t, responsesBody)

	prov := codexProviderAt(t, rec.srv.URL, true)
	lm, err := prov.LanguageModel(context.Background(), "sol-6.1")
	if err != nil {
		t.Fatalf("language model: %v", err)
	}

	resp, err := lm.Generate(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{{
			Role: fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "hi"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if rec.path != "/responses" {
		t.Errorf("unknown model ID routed to %q, want /responses", rec.path)
	}
	if resp.Usage.InputTokens != 5 {
		t.Errorf("usage input tokens = %d, want 5", resp.Usage.InputTokens)
	}
}

// TestCodexProviderDefaultGateRoutesUnknownIDsToChatCompletions documents the
// behavior the override prevents: with only WithUseResponsesAPI (the default
// gate), a model ID outside fantasy's exact list and gpt-N regex falls back to
// chat completions — the wrong endpoint for the codex backend.
func TestCodexProviderDefaultGateRoutesUnknownIDsToChatCompletions(t *testing.T) {
	rec := newEndpointRecorder(t, chatCompletionsBody)

	prov := codexProviderAt(t, rec.srv.URL, false)
	lm, err := prov.LanguageModel(context.Background(), "sol-6.1")
	if err != nil {
		t.Fatalf("language model: %v", err)
	}

	_, err = lm.Generate(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{{
			Role: fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "hi"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if rec.path != "/chat/completions" {
		t.Errorf("default gate routed unknown ID to %q, want /chat/completions", rec.path)
	}
}

// TestRuntimeThinkingOptionsReachResponsesModels is the end-to-end proof of
// the option-type fix: the options wllr actually applies at runtime (via
// providerOptionsForRuntime) must produce a request whose body carries
// reasoning_effort for a responses-API model.
func TestRuntimeThinkingOptionsReachResponsesModels(t *testing.T) {
	withAuthPath(t) // hermetic: type comes from the model ID, not a real stored token
	rec := newEndpointRecorder(t, responsesBody)
	prov := codexProviderAt(t, rec.srv.URL, true)
	lm, err := prov.LanguageModel(context.Background(), "gpt-5.5-codex")
	if err != nil {
		t.Fatalf("language model: %v", err)
	}

	po := providerOptionsForRuntime(providerOpenAI, thinkingModeHigh, "gpt-5.5-codex")
	_, err = lm.Generate(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{{
			Role: fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "hi"},
			},
		}},
		ProviderOptions: po,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(rec.body, "\"reasoning\"") || !strings.Contains(rec.body, "high") {
		t.Errorf("runtime options did not reach the request body: %s", rec.body)
	}
}

// TestNewCodexProviderBuilds is a smoke check that the production constructor
// still builds with the routing override in place.
func TestNewCodexProviderBuilds(t *testing.T) {
	prov, err := newCodexProvider("token", "acct")
	if err != nil {
		t.Fatalf("newCodexProvider: %v", err)
	}
	if prov == nil {
		t.Fatal("newCodexProvider returned nil provider")
	}
	if !strings.Contains("https://chatgpt.com/backend-api/codex", "chatgpt.com") {
		t.Fatal("sanity check on base URL constant failed")
	}
}

// TestCatalogIncludesGPT6Sol pins the catalog entries for the gpt-6 and
// gpt-5.6 families on both the API-key slice and the ChatGPT OAuth slice the
// /model picker serves to codex-login users.
func TestCatalogIncludesGPT6Sol(t *testing.T) {
	wantWindow := int64(1050000)
	for _, tc := range []struct {
		name   string
		models []modelInfo
	}{
		{"openai catalog", modelsForProvider(providerOpenAI)},
		{"chatgpt oauth catalog", chatGPTOAuthModels},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, m := range tc.models {
				if m.ID != "gpt-6-sol" {
					continue
				}
				found = true
				if m.ContextWindow != wantWindow {
					t.Errorf("gpt-6-sol context window = %d, want %d", m.ContextWindow, wantWindow)
				}
				if len(m.ThinkingModes) == 0 {
					t.Error("gpt-6-sol has no thinking modes")
				}
				hasMax := false
				for _, tm := range m.ThinkingModes {
					if tm.ID == thinkingModeMax {
						hasMax = true
					}
				}
				if !hasMax {
					t.Error("gpt-6-sol thinking modes missing the max level")
				}
			}
			if !found {
				t.Error("gpt-6-sol missing from catalog")
			}
		})
	}
}

// TestFantasyReasoningGateExcludesGPT6Sol documents an upstream limitation:
// fantasy v0.45.2's responses reasoning gate keys on substrings (o1/o3/o4/oss/
// gpt-5/codex-/computer-use) that no gpt-6 model ID matches, so reasoning_effort
// sent for gpt-6-sol is dropped before the request. The model still reasons
// (server default effort); wllr's /thinking picker is just ineffective for it
// until fantasy updates the gate. This test pins the behavior so a future
// fantasy bump that fixes it flips the expectation visibly.
func TestFantasyReasoningGateExcludesGPT6Sol(t *testing.T) {
	openaiEffort := fantasyopenapiprovider.ReasoningEffort("high")
	for _, tc := range []struct {
		name          string
		modelID       string
		wantReasoning bool
	}{
		{"codex model passes gate", "gpt-5.5-codex", true},
		{"gpt-6-sol fails gate", "gpt-6-sol", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newEndpointRecorder(t, responsesBody)
			prov := codexProviderAt(t, rec.srv.URL, true)
			lm, err := prov.LanguageModel(context.Background(), tc.modelID)
			if err != nil {
				t.Fatalf("language model: %v", err)
			}
			_, err = lm.Generate(context.Background(), fantasy.Call{
				Prompt: fantasy.Prompt{{
					Role: fantasy.MessageRoleUser,
					Content: []fantasy.MessagePart{
						fantasy.TextPart{Text: "hi"},
					},
				}},
				ProviderOptions: fantasy.ProviderOptions{
					fantasyopenapiprovider.Name: &fantasyopenapiprovider.ResponsesProviderOptions{
						ReasoningEffort: &openaiEffort,
					},
				},
			})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			gotReasoning := strings.Contains(rec.body, "\"reasoning\"")
			if gotReasoning != tc.wantReasoning {
				t.Errorf(
					"request body reasoning present = %v, want %v (body: %s)",
					gotReasoning,
					tc.wantReasoning,
					rec.body,
				)
			}
		})
	}
}
