package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ModelChoice is one selectable model for the /model picker. ID is the wire
// model identifier passed to the provider; Name is a human label.
type ModelChoice struct {
	ID       string
	Name     string
	Sublabel string
	// Provider is the provider that owns this model. The list spans providers
	// (each configured/pinned model, not just the active provider's), so
	// selecting a model switches provider when it differs from the active one.
	Provider string
	// Tiers lists the model-tier names this model is tagged with (e.g.
	// ["high"]). Rendered in the picker sublabel so the tagging is visible.
	Tiers              []string
	ContextWindow      int64
	ContextWindowKnown bool
	// Active marks the model currently in use, so the list can show which
	// provider/model is live.
	Active bool
}

// tierTagPrefix is the sublabel marker introducing the model's tier tags.
const tierTagPrefix = "tier: "

// openModelPicker builds the picker items from ModelListFn and opens the picker
// with the core model-selection callback. No-op (with a notification) when no
// model lister is wired or the list is empty.
func (m *Model) openModelPicker() {
	if m.ModelListFn == nil {
		m.pushNotification("Model selection is not available.")
		return
	}
	choices := m.ModelListFn()
	if len(choices) == 0 {
		m.pushNotification("No models available for the current provider.")
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		label, sub := modelListLabel(c, m.activeProvider, m.activeModel)
		items = append(items, sdk.ShowPickerItem{
			ID:       modelPickerKey(c.Provider, c.ID),
			Label:    label,
			Sublabel: sub,
		})
	}
	title := m.modelPickerTitle()
	if m.pendingModelPicker {
		title = "Previous model unavailable; select a replacement  (↑↓ · enter · esc)"
		m.pendingModelPicker = false
	}
	m.picker.Open(title, items, modelPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

// addModelKey opens the add-model flow from the model picker.
const addModelKey = "a"

// removeModelKey removes the highlighted model from the list.
const removeModelKey = "d"

// modelPickerTitle builds the picker title, including the tagging hint only
// when tier callbacks are wired.
func (m *Model) modelPickerTitle() string {
	hint := "↑↓ · enter"
	if m.TagModelTierFn != nil || m.ClearModelTierFn != nil {
		hint += " · h=high l=low u=untag"
	}
	if m.AddModelProviderListFn != nil {
		hint += " · a=add"
	}
	if m.RemoveSavedModelFn != nil {
		hint += " · d=remove"
	}
	return "Models  (" + hint + " · esc)"
}

// applyModelSelection switches the active model via SelectModelFn (which rebuilds
// the main agent's language model, updates the context window, and persists the
// choice), then updates the status display. Errors are surfaced as notifications.
func (m *Model) applyModelSelection(modelID string) tea.Cmd {
	if modelID == "" {
		return nil
	}
	// A tier name applies the tier's provider/model/thinking instead of being
	// treated as a model ID, so /model high and /models high work without the
	// user naming the underlying model.
	if tier := m.configuredTier(modelID); tier != "" {
		return m.applyModelTier(tier)
	}
	// The list spans providers, so resolve the choice's owning provider. When it
	// differs from the active one, switch provider instead of treating the ID as
	// a model on the current provider.
	if provider := m.providerForModel(modelID); provider != "" && provider != m.activeProvider {
		return m.applyProviderModelSelection(provider, modelID)
	}
	if m.SelectModelFn != nil {
		if err := m.SelectModelFn(modelID); err != nil {
			if errors.Is(err, ErrContextWindowRequired) {
				m.openContextWindowPrompt(m.activeProvider, modelID)
				return nil
			}
			m.pushNotification(fmt.Sprintf("⚠ could not switch model: %v", err))
			return nil
		}
	}
	cmd := m.setActiveProviderModel("", modelID)
	m.pushNotification("Model set to: " + modelID)
	return cmd
}

// configuredTier returns the configured tier name matching arg
// (case-insensitive), or "" when arg is not a tier name.
func (m *Model) configuredTier(arg string) string {
	if m.TierNamesFn == nil {
		return ""
	}
	want := strings.ToLower(strings.TrimSpace(arg))
	for _, name := range m.TierNamesFn() {
		if strings.ToLower(strings.TrimSpace(name)) == want {
			return name
		}
	}
	return ""
}

// applyModelTier applies a named tier via ApplyModelTierFn, then updates the
// status display. Errors are surfaced as notifications.
func (m *Model) applyModelTier(tier string) tea.Cmd {
	if m.ApplyModelTierFn == nil {
		m.pushNotification("Model tiers are not available.")
		return nil
	}
	provider, modelID, err := m.ApplyModelTierFn(tier)
	if err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not apply tier %s: %v", tier, err))
		return nil
	}
	cmd := m.setActiveProviderModel(provider, modelID)
	m.pushNotification(fmt.Sprintf("Applied tier %s → %s", tier, modelID))
	return cmd
}

// Tier-tagging keys available while the model picker is open.
const (
	modelPickerHighKey  = "h"
	modelPickerLowKey   = "l"
	modelPickerUntagKey = "u"
)

// Reserved tier names with built-in behavior. These mirror the cmd package's
// tierHigh/tierLow values; the harness owns the UI keys that produce them, so
// they are duplicated here rather than imported across the package boundary.
const (
	tierHighName = "high"
	tierLowName  = "low"
)

// modelPickerTierKey maps a picker key press to the tier it tags. The bool is
// false when the key is not a tier key and should be handled as navigation.
func modelPickerTierKey(key string) (string, bool) {
	switch key {
	case modelPickerHighKey:
		return tierHighName, true
	case modelPickerLowKey:
		return tierLowName, true
	case modelPickerUntagKey:
		return "", true
	}
	return "", false
}

// applyModelTierTag tags the highlighted model as tier (or clears its tags when
// untag is true), then reopens the picker so the tag change is visible. The
// picker key carries the owning provider, so tagging works for any row even
// though the list spans providers.
func (m *Model) applyModelTierTag(tier string, untag bool) {
	key, ok := m.picker.Highlighted()
	if !ok {
		return
	}
	provider, modelID, ok := splitModelPickerKey(key)
	if !ok {
		return
	}
	if untag {
		m.untagModelTiers(provider, modelID)
		m.reopenModelPickerAt(key)
		return
	}
	if m.TagModelTierFn == nil {
		m.pushNotification("Model tier tagging is not available.")
		return
	}
	if err := m.TagModelTierFn(tier, provider, modelID); err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not tag tier %s: %v", tier, err))
		m.reopenModelPickerAt(key)
		return
	}
	m.pushNotification(fmt.Sprintf("Tagged %s as tier %s", modelID, tier))
	m.reopenModelPickerAt(key)
}

// modelPickerKeySep separates the provider from the model ID in a picker item
// key. The list spans providers, so the ID alone is ambiguous; a unit separator
// cannot appear in a provider name or a model ID.
const modelPickerKeySep = "\x1f"

// modelPickerKey builds the picker's stable per-row key.
func modelPickerKey(provider, modelID string) string {
	if provider == "" {
		return modelID
	}
	return provider + modelPickerKeySep + modelID
}

// splitModelPickerKey reverses modelPickerKey. A key without a separator is
// reported with an empty provider so unqualified callers still work.
func splitModelPickerKey(key string) (provider, modelID string, ok bool) {
	if key == "" {
		return "", "", false
	}
	if i := strings.Index(key, modelPickerKeySep); i >= 0 {
		return key[:i], key[i+len(modelPickerKeySep):], true
	}
	return "", key, true
}

// reopenModelPickerAt rebuilds the picker (so a changed tag is visible) with
// the given model still highlighted. Without this the reopen would reset the
// cursor to the first row, so tagging one model and then pressing u/h/l would
// silently edit a different model.
func (m *Model) reopenModelPickerAt(modelID string) {
	m.openModelPicker()
	m.picker.Select(modelID)
}

// untagModelTiers clears every configured tier currently pointing at
// provider/modelID.
func (m *Model) untagModelTiers(provider, modelID string) {
	if m.ClearModelTierFn == nil {
		m.pushNotification("Model tier tagging is not available.")
		return
	}
	cleared := 0
	for _, c := range m.currentModelChoices() {
		if c.ID != modelID || (c.Provider != "" && c.Provider != provider) {
			continue
		}
		for _, tier := range c.Tiers {
			if err := m.ClearModelTierFn(tier); err != nil {
				m.pushNotification(fmt.Sprintf("⚠ could not clear tier %s: %v", tier, err))
				continue
			}
			cleared++
		}
	}
	if cleared == 0 {
		m.pushNotification(modelID + " has no tier tags.")
		return
	}
	m.pushNotification(fmt.Sprintf("Cleared %d tier tag(s) from %s", cleared, modelID))
}

// currentModelChoices returns ModelListFn's choices, or nil when unwired.
func (m *Model) currentModelChoices() []ModelChoice {
	if m.ModelListFn == nil {
		return nil
	}
	return m.ModelListFn()
}

// showModelTiers lists the configured model tiers in a modal so the tagging is
// discoverable without opening the picker.
func (m *Model) showModelTiers() {
	labels := []string(nil)
	if m.ModelTierLabelsFn != nil {
		labels = m.ModelTierLabelsFn()
	}
	if len(labels) == 0 {
		m.pushNotification(
			"No model tiers configured. Open /models and press h or l to tag a model.",
		)
		return
	}
	var sb strings.Builder
	sb.WriteString("Model tiers\n")
	sb.WriteString("────────────────────────────────────────\n\n")
	for _, label := range labels {
		sb.WriteString(label)
		sb.WriteString("\n")
	}
	sb.WriteString("\nApply with /model <tier>. Tag models in /models with h (high), l (low), u (untag).")
	m.modalContent = strings.TrimRight(sb.String(), "\n")
	m.modalScroll = 0
}

// applyThinkingLevel applies a provider-agnostic thinking level (e.g. "high")
// via SetThinkingLevelFn, which resolves it to the active provider's mode ID.
// Errors are surfaced as notifications; a missing callback is a no-op.
func (m *Model) applyThinkingLevel(level string) tea.Cmd {
	if m.SetThinkingLevelFn == nil || level == "" {
		return nil
	}
	if err := m.SetThinkingLevelFn(level); err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not set thinking level %s: %v", level, err))
		return nil
	}
	m.pushNotification("Thinking level set to: " + level)
	return nil
}

// providerForModel returns the provider owning the given model ID in the current
// list, or "" when it cannot be determined.
func (m *Model) providerForModel(modelID string) string {
	for _, c := range m.currentModelChoices() {
		if c.ID == modelID && c.Provider != "" {
			return c.Provider
		}
	}
	return ""
}

// applyProviderModelSelection switches to a model on a different provider than
// the active one, via SelectProviderModelFn, then updates the status display.
func (m *Model) applyProviderModelSelection(provider, modelID string) tea.Cmd {
	if m.SelectProviderModelFn == nil {
		m.pushNotification(fmt.Sprintf("Switching to %s is not available.", provider))
		return nil
	}
	if err := m.SelectProviderModelFn(provider, modelID); err != nil {
		if errors.Is(err, ErrContextWindowRequired) {
			m.openContextWindowPrompt(provider, modelID)
			return nil
		}
		m.pushNotification(fmt.Sprintf("⚠ could not switch to %s: %v", provider, err))
		return nil
	}
	cmd := m.setActiveProviderModel(provider, modelID)
	m.pushNotification(fmt.Sprintf("Model set to: %s (%s)", modelID, provider))
	return cmd
}
