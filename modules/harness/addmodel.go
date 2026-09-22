package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// The add-model flow is the `a` key in the model picker. It lists the providers
// the user can add a model from, then routes each provider to the wizard that
// can configure it:
//
//   - anthropic / openai / gemini: list the catalog and save the choice
//   - openrouter: API-key setup (when needed) then the searchable live catalog
//   - local: the existing base-URL probe + manual-entry wizard
//
// An unauthenticated provider is routed into the existing login flow first, so
// `a` never presents a model list that cannot be used.

// addModelPickerCallback routes a provider selection from the add-model picker.
const addModelPickerCallback = "__wllr:add_model_provider"

// catalogModelPickerCallback routes a catalog-model selection from the
// add-model flow to a save+activate.
const catalogModelPickerCallback = "__wllr:add_model_catalog"

// addModelProviderSelectedMsg carries the provider chosen in the add-model flow.
type addModelProviderSelectedMsg struct{ Provider string }

// catalogModelPickedMsg carries the catalog model chosen in the add-model flow.
type catalogModelPickedMsg struct {
	Provider string
	Choice   ModelChoice
}

// openAddModelProviderPicker opens the provider list for adding a model.
func (m *Model) openAddModelProviderPicker() {
	if m.AddModelProviderListFn == nil {
		m.pushNotification("Adding models is not available.")
		return
	}
	choices := m.AddModelProviderListFn()
	if len(choices) == 0 {
		m.pushNotification("No providers are available to add.")
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		items = append(items, sdk.ShowPickerItem{ID: c.ID, Label: c.Name, Sublabel: c.Sublabel})
	}
	m.picker.Open("Add a model — choose a provider  (↑↓ · enter · esc)", items, addModelPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

// providerReady reports whether the provider is configured enough to list
// models. Unknown (nil callback) is treated as ready so the flow still works
// when readiness is not wired.
func (m *Model) providerReady(provider string) bool {
	if m.ProviderReadyFn == nil {
		return true
	}
	return m.ProviderReadyFn(provider)
}

// addModelForProvider routes a provider to its add-model wizard. A provider that
// is not yet configured goes through login first, which resumes the flow.
func (m *Model) addModelForProvider(provider string) tea.Cmd {
	if provider == "" {
		return nil
	}
	// Local always opens its endpoint wizard: adding a local model means
	// pointing at an endpoint, not authenticating.
	if provider == providerLocal {
		return func() tea.Msg { return showLocalModelSetupMsg{} }
	}
	if !m.providerReady(provider) {
		// Record which wizard to resume once the provider is usable. The login
		// handlers re-enter addModelForProvider via the pending field.
		m.pendingAddModelProvider = provider
		if provider == providerOpenRouter {
			return func() tea.Msg { return showOpenRouterSetupMsg{} }
		}
		m.openAuthPrompt(provider)
		return nil
	}
	if provider == providerOpenRouter {
		// A ready OpenRouter provider already has a key; go straight to search.
		return m.fetchOpenRouterCatalogCmd()
	}
	return func() tea.Msg { return showCatalogModelPickerMsg{Provider: provider} }
}

// showCatalogModelPickerMsg opens the catalog list for a provider in the
// add-model flow.
type showCatalogModelPickerMsg struct{ Provider string }

// openCatalogModelPicker lists a provider's addable models.
func (m *Model) openCatalogModelPicker(provider string) {
	if m.CatalogModelsFn == nil {
		m.pushNotification("Adding models is not available.")
		return
	}
	choices := m.CatalogModelsFn(provider)
	if len(choices) == 0 {
		m.pushNotification(fmt.Sprintf("No models are available for %s.", provider))
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		sub := c.Sublabel
		if sub == "" {
			sub = c.ID
		}
		// The sublabel already carries the context window for catalog models
		// (modelChoiceSublabel); only add it when the caller did not.
		if c.ContextWindow > 0 && !strings.Contains(sub, "ctx") {
			sub += fmt.Sprintf("  %dk ctx", c.ContextWindow/1000)
		}
		items = append(items, sdk.ShowPickerItem{ID: c.ID, Label: c.Name, Sublabel: sub})
	}
	m.catalogPickerProvider = provider
	m.picker.Open(
		fmt.Sprintf("Add a %s model  (↑↓ · enter · esc)", provider),
		items,
		catalogModelPickerCallback,
	)
	m.picker.SetSize(m.width, m.chatHeight())
}

// applyCatalogModelPick saves the chosen catalog model and makes it active.
func (m *Model) applyCatalogModelPick(provider, modelID string) tea.Cmd {
	var chosen ModelChoice
	found := false
	if m.CatalogModelsFn != nil {
		for _, c := range m.CatalogModelsFn(provider) {
			if c.ID == modelID {
				chosen, found = c, true
				break
			}
		}
	}
	if !found {
		chosen = ModelChoice{ID: modelID, Name: modelID, Provider: provider}
	}
	if m.SaveCatalogModelFn == nil {
		m.pushNotification("Adding models is not available.")
		return nil
	}
	savedID, err := m.SaveCatalogModelFn(provider, chosen)
	if err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not add model: %v", err))
		return nil
	}
	m.pushNotification(fmt.Sprintf("✓ Added %s (%s)", savedID, provider))
	cmd := m.setActiveProviderModel(provider, savedID)
	if chosen.ContextWindow <= 0 {
		m.openContextWindowPrompt(provider, savedID)
		return nil
	}
	return cmd
}

// removeModelFromList removes a model from the persisted list, so an added
// model can be taken back out. The active model is never removed: it is what
// the session is running on, and dropping it would leave the config pointing at
// a model the list no longer offers.
func (m *Model) removeModelFromList(provider, modelID string) {
	if m.RemoveSavedModelFn == nil {
		m.pushNotification("Removing models is not available.")
		return
	}
	if modelID == m.activeModel && (provider == "" || provider == m.activeProvider) {
		m.pushNotification("Cannot remove the active model; switch to another model first.")
		return
	}
	for _, c := range m.currentModelChoices() {
		if c.ID == modelID && (provider == "" || c.Provider == provider) && len(c.Tiers) > 0 {
			m.pushNotification(fmt.Sprintf(
				"⚠ %s is tagged %s; remove the tier tag first (u in /models).",
				modelID, strings.Join(c.Tiers, ", "),
			))
			return
		}
	}
	if err := m.RemoveSavedModelFn(provider, modelID); err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not remove model: %v", err))
		return
	}
	m.pushNotification("Removed " + modelID)
}

// modelListLabel renders one model row: name plus provider, current marker, tier
// tags, and a missing-window warning.
func modelListLabel(c ModelChoice, activeProvider, activeModel string) (string, string) {
	sub := c.Sublabel
	if sub == "" {
		sub = c.ID
	}
	if c.Provider != "" {
		sub = c.Provider + " · " + sub
	}
	if c.Active || (c.ID == activeModel && (c.Provider == "" || c.Provider == activeProvider)) {
		sub += "  (current)"
	}
	if len(c.Tiers) > 0 {
		sub += "  " + tierTagPrefix + strings.Join(c.Tiers, ", ")
	}
	if !c.ContextWindowKnown {
		sub += "  (context window required)"
	}
	return c.Name, sub
}

// resumePendingAddModel continues an add-model flow that had to detour through
// login. Returns a command when a catalog picker should open, and clears the
// pending provider in all cases so a later flow is not hijacked.
func (m *Model) resumePendingAddModel() tea.Cmd {
	provider := m.pendingAddModelProvider
	m.pendingAddModelProvider = ""
	if provider == "" {
		return nil
	}
	if !m.providerReady(provider) {
		m.pushNotification(fmt.Sprintf("%s is still not configured; run /login to finish setup.", provider))
		return nil
	}
	if provider == providerOpenRouter {
		return m.fetchOpenRouterCatalogCmd()
	}
	return func() tea.Msg { return showCatalogModelPickerMsg{Provider: provider} }
}

// resumeAddModelMsg re-enters the add-model flow for a provider after a login
// completed, so the wizard continues instead of dropping the user.
type resumeAddModelMsg struct{ Provider string }
