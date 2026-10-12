package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// contexttoolong_test.go tests isContextTooLong, the predicate the reactive
// compaction fallback keys off. It lives in package agent (white-box) to reach
// the unexported function.
//
// The cases below reproduce what the fantasy provider adapters actually put on
// the wire — the structured ProviderError fields they set, and the wrapping the
// retry middleware applies — rather than restating the function's own logic.

import (
	"errors"
	"fmt"
	"testing"

	"charm.land/fantasy"
)

// TestIsContextTooLong_Nil covers the nil short-circuit.
func TestIsContextTooLong_Nil(t *testing.T) {
	if isContextTooLong(nil) {
		t.Fatal("isContextTooLong(nil) = true, want false")
	}
}

// TestIsContextTooLong_StructuredProviderError is the core regression guard:
// the adapter parses its own rejection format and sets the flag, so detection
// must not depend on wllr recognising the wording. The message here is kronk's
// "input tokens [N] exceed context window [M]", which the previous
// string-matching implementation did not match at all.
func TestIsContextTooLong_StructuredProviderError(t *testing.T) {
	err := &fantasy.ProviderError{
		Title:              "kronk context window exceeded",
		Message:            "input tokens [9000] exceed context window [8192]",
		ContextTooLargeErr: true,
		ContextUsedTokens:  9000,
		ContextMaxTokens:   8192,
	}
	if !isContextTooLong(err) {
		t.Fatal("isContextTooLong(structured context error) = false, want true")
	}
}

// TestIsContextTooLong_StructuredProviderErrorWrapped covers the error chain:
// fantasy wraps non-retryable failures in RetryError (whose Unwrap exposes the
// last error) and callers may add their own context with %w. Detection must
// survive both, since that is the shape the error actually arrives in.
func TestIsContextTooLong_StructuredProviderErrorWrapped(t *testing.T) {
	base := &fantasy.ProviderError{
		Message:            "input token count 1500000 exceeds the maximum 1048576",
		ContextTooLargeErr: true,
		ContextUsedTokens:  1500000,
		ContextMaxTokens:   1048576,
	}
	cases := map[string]error{
		"fmt.Errorf %w":     fmt.Errorf("provider request failed: %w", base),
		"RetryError":        &fantasy.RetryError{Errors: []error{base}},
		"RetryError nested": &fantasy.RetryError{Errors: []error{fmt.Errorf("attempt: %w", base)}},
	}
	for name, err := range cases {
		if !isContextTooLong(err) {
			t.Errorf("isContextTooLong(%s) = false, want true", name)
		}
	}
}

// TestIsContextTooLong_TokenFieldsOnly covers ProviderError.IsContextTooLarge's
// full contract: a token field alone marks the error, even without the explicit
// flag. The adapters set these from the numbers embedded in the rejection.
func TestIsContextTooLong_TokenFieldsOnly(t *testing.T) {
	cases := map[string]*fantasy.ProviderError{
		"max only":  {ContextMaxTokens: 131072},
		"used only": {ContextUsedTokens: 200000},
	}
	for name, err := range cases {
		if !isContextTooLong(err) {
			t.Errorf("isContextTooLong(%s) = false, want true", name)
		}
	}
}

// TestIsContextTooLong_NonContextProviderError guards against over-matching: a
// provider failure unrelated to the window must not trigger a compaction.
func TestIsContextTooLong_NonContextProviderError(t *testing.T) {
	err := &fantasy.ProviderError{
		Title:      "internal server error",
		Message:    "upstream is having a bad day",
		StatusCode: 500,
	}
	if isContextTooLong(err) {
		t.Fatal("isContextTooLong(non-context ProviderError) = true, want false")
	}
}

// TestIsContextTooLong_StringFallback covers the narrow fallback for a
// rejection that reaches us as a bare string with no ProviderError in the
// chain.
func TestIsContextTooLong_StringFallback(t *testing.T) {
	cases := []string{
		"prompt is too long: 202630 tokens > 200000 maximum",
		`{"error":{"code":"context_length_exceeded"}}`,
		"maximum context length is 128000 tokens",
		"213370 tokens > 200000 maximum",
	}
	for _, msg := range cases {
		if !isContextTooLong(errors.New(msg)) {
			t.Errorf("isContextTooLong(%q) = false, want true", msg)
		}
	}
}

// TestIsContextTooLong_UnrelatedError covers the plain-error miss path.
func TestIsContextTooLong_UnrelatedError(t *testing.T) {
	if isContextTooLong(errors.New("connection refused")) {
		t.Fatal("isContextTooLong(unrelated error) = true, want false")
	}
}

// TestIsContextTooLong_ProviderFormats pins detection for every rejection
// format the fantasy adapters recognise, using the structured fields each
// adapter sets. Six of these nine were missed by the previous
// string-matching implementation, so this table is the coverage contract for
// the local provider in particular: providerLocal is OpenAI-compatible, and
// five of the nine formats belong to that adapter.
func TestIsContextTooLong_ProviderFormats(t *testing.T) {
	cases := []struct {
		provider string
		err      *fantasy.ProviderError
	}{
		{"anthropic", &fantasy.ProviderError{
			Message:            "prompt is too long: 202630 tokens > 200000 maximum",
			ContextTooLargeErr: true, ContextUsedTokens: 202630, ContextMaxTokens: 200000,
		}},
		{"openai", &fantasy.ProviderError{
			Message:            "maximum context length is 128000 tokens. However, your messages resulted in 200000 tokens",
			ContextTooLargeErr: true, ContextUsedTokens: 200000, ContextMaxTokens: 128000,
		}},
		{"google", &fantasy.ProviderError{
			Message:            "input token count 1500000 exceeds the maximum 1048576",
			ContextTooLargeErr: true, ContextUsedTokens: 1500000, ContextMaxTokens: 1048576,
		}},
		{"kronk", &fantasy.ProviderError{
			Message:            "input tokens [9000] exceed context window [8192]",
			ContextTooLargeErr: true, ContextUsedTokens: 9000, ContextMaxTokens: 8192,
		}},
		{"kronk-full", &fantasy.ProviderError{
			Message:            "the context window is full",
			ContextTooLargeErr: true,
		}},
		{"alibaba", &fantasy.ProviderError{
			Message:            "Range of input length should be [1, 131072]",
			ContextTooLargeErr: true, ContextMaxTokens: 131072,
		}},
		{"baseten", &fantasy.ProviderError{
			Message:            "Input length 200000 exceeds the maximum allowed input length of 128000 tokens",
			ContextTooLargeErr: true, ContextUsedTokens: 200000, ContextMaxTokens: 128000,
		}},
		{"fireworks", &fantasy.ProviderError{
			Message:            "The prompt is too long: 200000, model maximum context length: 128000",
			ContextTooLargeErr: true, ContextUsedTokens: 200000, ContextMaxTokens: 128000,
		}},
		{"vercel", &fantasy.ProviderError{
			Message:            "Input too long: 200000 input tokens, limit is 128000",
			ContextTooLargeErr: true, ContextUsedTokens: 200000, ContextMaxTokens: 128000,
		}},
	}
	for _, tc := range cases {
		if !isContextTooLong(tc.err) {
			t.Errorf("isContextTooLong(%s) = false, want true", tc.provider)
		}
	}
}
