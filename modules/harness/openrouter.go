package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

const (
	providerOpenRouter        = "openrouter"
	openRouterKeyCallback     = "__wllr:openrouter_key"
	openRouterCatalogCallback = "__wllr:openrouter_catalog"
)

// OpenRouterModelChoice is one model from OpenRouter's live catalog.
type OpenRouterModelChoice struct {
	ID            string
	Name          string
	ContextWindow int64
}

type (
	showOpenRouterSetupMsg     struct{}
	openRouterKeyEnteredMsg    struct{ Key string }
	openRouterCatalogResultMsg struct {
		Err    error
		Models []OpenRouterModelChoice
	}
)
type openRouterModelPickedMsg struct{ ID string }

func (m *Model) openOpenRouterKeyPrompt() {
	m.textInput.OpenSecret("OpenRouter API key  (enter · esc)", openRouterKeyCallback)
	m.textInput.SetSize(m.width, m.chatHeight())
}

func (m *Model) fetchOpenRouterCatalogCmd() tea.Cmd {
	fetch := m.FetchOpenRouterModelsFn
	return func() tea.Msg {
		if fetch == nil {
			return openRouterCatalogResultMsg{Err: fmt.Errorf("model discovery is unavailable")}
		}
		models, err := fetch()
		return openRouterCatalogResultMsg{Models: models, Err: err}
	}
}

func (m *Model) openOpenRouterCatalog(models []OpenRouterModelChoice) {
	if len(models) == 0 {
		m.pushNotification("No OpenRouter models were returned.")
		return
	}
	m.openRouterCatalog = models
	items := make([]sdk.ShowPickerItem, 0, len(models))
	for _, model := range models {
		sub := model.ID
		if model.ContextWindow > 0 {
			sub = fmt.Sprintf("%s · %dk ctx", model.ID, model.ContextWindow/1000)
		}
		items = append(items, sdk.ShowPickerItem{ID: model.ID, Label: model.Name, Sublabel: sub})
	}
	m.picker.OpenSearch("Browse OpenRouter models  (type to search)", items, openRouterCatalogCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

func (m *Model) applyOpenRouterModelPick(id string) tea.Cmd {
	for _, model := range m.openRouterCatalog {
		if model.ID != id {
			continue
		}
		if m.AddOpenRouterModelFn == nil {
			m.pushNotification("OpenRouter model setup is unavailable")
			return nil
		}
		if err := m.AddOpenRouterModelFn(model); err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not add OpenRouter model: %v", err))
			return nil
		}
		m.pushNotification("✓ OpenRouter model added: " + model.ID)
		cmd := m.setActiveProviderModel(providerOpenRouter, model.ID)
		if model.ContextWindow <= 0 {
			m.openContextWindowPrompt(providerOpenRouter, model.ID)
		}
		return cmd
	}
	m.pushNotification("OpenRouter model is no longer in the catalog")
	return nil
}
