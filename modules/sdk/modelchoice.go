package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// This file defines the renderer-neutral model-selection contract shared by
// every front end. The interactive TUI builds a /model picker from it; the
// HTTP front end serves it as JSON and routes the selection back as a core
// callback. Both sides must agree on two things for a selection to work — the
// choice shape and the picker key encoding — so both live here rather than in
// the harness or the server.

import "strings"

// ModelChoice is one selectable model. ID is the wire model identifier passed
// to the provider; Name is a human label.
type ModelChoice struct {
	ID       string
	Name     string
	Sublabel string
	// Provider owns this model. The list spans providers (every configured or
	// pinned model), so selecting one switches provider when it differs from the
	// active provider.
	Provider string
	// Tiers lists the model-tier names this model is tagged with (for example
	// ["high"]).
	Tiers []string
	// ContextWindow is the model's context window in tokens, when known.
	ContextWindow int64
	// ContextWindowKnown reports whether ContextWindow is a real value rather
	// than a zero placeholder, so a list does not render "0 tokens".
	ContextWindowKnown bool
	// Active marks the model currently in use, so a list can show what is live.
	Active bool
}

// ModelPickerCallback is the reserved core callback id under which a model
// selection is routed back to the engine. The harness picker opens with it and
// classifyCoreSelection recognizes it; a browser front end must use the same id
// (served alongside the model list) or its selection would not reach the model
// handler. The "__wllr:" prefix marks it core-owned.
const ModelPickerCallback = "__wllr:model"

// ModelPickerKeySep separates the provider from the model ID in a picker item
// key. The list spans providers, so an ID alone is ambiguous; a unit separator
// cannot appear in a provider name or a model ID. Exported because the browser
// front end sends a key the harness picker must decode identically — a wrong
// separator would silently select nothing.
const ModelPickerKeySep = "\x1f"

// ModelPickerKey builds a picker's stable per-row key. An empty provider yields
// the bare model ID, so an unqualified selection still round-trips.
func ModelPickerKey(provider, modelID string) string {
	if provider == "" {
		return modelID
	}
	return provider + ModelPickerKeySep + modelID
}

// SplitModelPickerKey reverses ModelPickerKey. A key without a separator is
// reported with an empty provider so unqualified callers still work; ok is
// false only for an empty key.
func SplitModelPickerKey(key string) (provider, modelID string, ok bool) {
	if key == "" {
		return "", "", false
	}
	if i := strings.Index(key, ModelPickerKeySep); i >= 0 {
		return key[:i], key[i+len(ModelPickerKeySep):], true
	}
	return "", key, true
}
