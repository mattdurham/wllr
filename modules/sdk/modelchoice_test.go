package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"
)

// TestModelPickerKeyRoundTrip proves a provider-qualified key survives the
// encode/decode pair the browser and the harness picker share. A mismatch here
// would silently select nothing.
func TestModelPickerKeyRoundTrip(t *testing.T) {
	key := ModelPickerKey("openai", "gpt-4o")
	if !strings.Contains(key, ModelPickerKeySep) {
		t.Fatalf("key %q does not carry the separator", key)
	}
	provider, id, ok := SplitModelPickerKey(key)
	if !ok || provider != "openai" || id != "gpt-4o" {
		t.Errorf("split(%q) = (%q,%q,%v), want (openai,gpt-4o,true)", key, provider, id, ok)
	}
}

// TestModelPickerKeyBare proves an unqualified key (no provider) round-trips to
// an empty provider rather than being rejected, so older callers still work.
func TestModelPickerKeyBare(t *testing.T) {
	if got := ModelPickerKey("", "m1"); got != "m1" {
		t.Errorf("bare key = %q, want m1", got)
	}
	provider, id, ok := SplitModelPickerKey("m1")
	if !ok || provider != "" || id != "m1" {
		t.Errorf("split(m1) = (%q,%q,%v), want (\"\",m1,true)", provider, id, ok)
	}
}

// TestSplitModelPickerKeyEmpty proves an empty key is rejected, so a missing
// selection is distinguishable from a bare model id.
func TestSplitModelPickerKeyEmpty(t *testing.T) {
	if _, _, ok := SplitModelPickerKey(""); ok {
		t.Error("empty key should not decode")
	}
}

// TestModelListerFunc proves the function adapter satisfies the interface.
func TestModelListerFunc(t *testing.T) {
	var l ModelLister = ModelListerFunc(func() []ModelChoice {
		return []ModelChoice{{ID: "a"}}
	})
	if got := l.ModelChoices(); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("ModelChoices = %+v, want one choice 'a'", got)
	}
}

// TestModelPickerCallbackIsCore proves the model picker callback is recognized
// as core-owned, which is the router split that sends it to the model handler
// rather than to an extension.
func TestModelPickerCallbackIsCore(t *testing.T) {
	if !IsCoreCallback(ModelPickerCallback) {
		t.Errorf("%q must be a core callback", ModelPickerCallback)
	}
}
