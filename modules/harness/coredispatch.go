package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// coredispatch.go — the shared dispatch for core-owned ("__wllr:") picker
// selections that carry all the state they need in the callback id and value.
//
// The interactive Model resolves these inside updateKeyPressPicker by emitting
// an internal tea.Msg (setModelMsg, setThinkingMsg, …) that the update loop
// turns into an apply call. A non-terminal front end (the HTTP server's browser
// client, an embedder) has no event loop, so it needs to run the same apply
// call directly. classifyCoreSelection is the single source of the
// callback-id → apply-action mapping; both front ends consume it, so a picker
// behaves identically whether the selection came from the terminal or a
// browser.
//
// Only the stateless selections are classified here (model, provider-model,
// thinking level, OpenRouter routing). The multi-step setup flows — auth,
// local-model base-URL/manual entry, context-window entry, catalog selection,
// add-model — carry live Model state built up across earlier interactions
// (pendingAuthProvider, localSetupModels, …) and stay TUI-only; a headless
// front end cannot start them from a cold callback.

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// coreSelectionKind identifies the apply action a core picker selection maps to.
type coreSelectionKind int

const (
	// coreSelectNone means the callback is not a stateless core selection (it is
	// an extension command, a stateful setup flow, or unknown).
	coreSelectNone coreSelectionKind = iota
	// coreSelectModel selects a model on the active provider (bare picker key).
	coreSelectModel
	// coreSelectProviderModel selects a model owned by a specific provider
	// (provider-qualified picker key).
	coreSelectProviderModel
	// coreSelectThinking sets the reasoning level.
	coreSelectThinking
	// coreSelectSpeed sets the OpenRouter provider-routing preference.
	coreSelectSpeed
)

// coreSelection is the classified form of a stateless core picker callback.
type coreSelection struct {
	provider string
	value    string
	kind     coreSelectionKind
}

// classifyCoreSelection maps a core picker callback and its selected value (a
// picker item id or a submitted value) to the apply action it triggers. It
// returns coreSelectNone for anything that is not a stateless core selection,
// so callers fall through to extension dispatch.
func classifyCoreSelection(callback, value string) coreSelection {
	switch callback {
	case modelPickerCallback:
		// The model list spans providers, so the item id encodes the owner. A
		// provider-qualified key routes through the cross-provider switch; a
		// bare key is a same-provider model (or tier) selection. The interactive
		// Model applies the same split before choosing setProviderModelMsg vs
		// setModelMsg.
		if provider, modelID, ok := splitModelPickerKey(value); ok && provider != "" {
			return coreSelection{kind: coreSelectProviderModel, provider: provider, value: modelID}
		}
		_, modelID, _ := splitModelPickerKey(value)
		return coreSelection{kind: coreSelectModel, value: modelID}
	case thinkingPickerCallback:
		return coreSelection{kind: coreSelectThinking, value: value}
	case openRouterSpeedPickerCallback:
		return coreSelection{kind: coreSelectSpeed, value: value}
	}
	return coreSelection{}
}

// applyCoreSelection runs the apply call for a classified selection, reusing
// the same apply methods the interactive update loop calls. The returned tea.Cmd
// is the statusline repaint command (nil for the non-model selections); a
// headless caller has no event loop to run it, so ResolveCoreCallback dispatches
// the model-changed notification itself while the engine and local display state
// are updated synchronously here.
func (m *Model) applyCoreSelection(sel coreSelection) tea.Cmd {
	switch sel.kind {
	case coreSelectModel:
		return m.applyModelSelection(sel.value)
	case coreSelectProviderModel:
		if sel.provider == m.activeProvider {
			return m.applyModelSelection(sel.value)
		}
		return m.applyProviderModelSelection(sel.provider, sel.value)
	case coreSelectThinking:
		m.applyThinkingSelection(sel.value)
	case coreSelectSpeed:
		m.applySpeedSelection(sel.value)
	}
	return nil
}

// ResolveCoreCallback applies a core-owned ("__wllr:") selection to the engine
// without the tea event loop, for a front end that has none. It reports whether
// the callback was a stateless core selection it handled; false means the
// caller should fall through to extension dispatch. A declared apply error is
// surfaced as a notification (matching the interactive behavior) rather than
// returned, so the boolean — not the error — is the "handled here" signal. A
// failure to notify extensions of a model change IS returned, since there is no
// notification path for it here.
func (m *Model) ResolveCoreCallback(ctx context.Context, callback string, args []string) (bool, error) {
	sel := classifyCoreSelection(callback, firstArg(args))
	if sel.kind == coreSelectNone {
		return false, nil
	}
	m.applyCoreSelection(sel)
	// A model/provider-model selection returns a statusline repaint command
	// whose only effect is to dispatch EventModelChanged. With no event loop to
	// run it, dispatch directly so a headless front end (and the bundled
	// statusline extension) sees the new provider/model.
	if sel.kind == coreSelectModel || sel.kind == coreSelectProviderModel {
		if err := m.DispatchModelChanged(ctx); err != nil {
			return true, err
		}
	}
	return true, nil
}

// CoreCallbackFunc adapts ResolveCoreCallback to the sdk.CoreCallbackHandler
// contract a callback router installs. An unhandled callback yields
// sdk.ErrNoCoreHandler so the router can report "not implemented here" instead
// of a fault — the same sentinel a nil handler produces.
func (m *Model) CoreCallbackFunc() sdk.CoreCallbackFunc {
	return func(ctx context.Context, callback string, args []string) error {
		handled, err := m.ResolveCoreCallback(ctx, callback, args)
		if err != nil {
			return err
		}
		if !handled {
			return sdk.ErrNoCoreHandler
		}
		return nil
	}
}

// firstArg returns args[0], or "" when args is empty. Core selections carry a
// single value; a picker sends the item id and a text input sends the value.
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
