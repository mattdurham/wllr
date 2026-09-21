package harness

import (
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestOpenRouterSetupPromptsForMaskedKey(t *testing.T) {
	m := newTestModel()
	m.HasOpenRouterKeyFn = func() bool { return false }
	cmd := m.applyLoginProviderSelection(providerOpenRouter)
	if cmd == nil {
		t.Fatal("expected setup command")
	}
	m, _ = callUpdate(m, cmd())
	if !m.textInput.IsActive() || m.textInput.Callback != openRouterKeyCallback ||
		m.textInput.input.EchoMode != textinput.EchoPassword {
		t.Fatal("OpenRouter key prompt was not masked")
	}
}

func TestOpenRouterSetupFetchesAndPinsModel(t *testing.T) {
	m := newTestModel()
	var savedKey, pinned string
	m.SaveOpenRouterKeyFn = func(key string) error { savedKey = key; return nil }
	m.FetchOpenRouterModelsFn = func() ([]OpenRouterModelChoice, error) {
		return []OpenRouterModelChoice{{ID: "openai/gpt", Name: "GPT", ContextWindow: 128000}}, nil
	}
	m.AddOpenRouterModelFn = func(choice OpenRouterModelChoice) error { pinned = choice.ID; return nil }
	m, cmd := callUpdate(m, openRouterKeyEnteredMsg{Key: " key "})
	if savedKey != "key" || cmd == nil {
		t.Fatalf("savedKey=%q cmd=%v", savedKey, cmd)
	}
	m, _ = callUpdate(m, cmd())
	if !m.picker.IsActive() || !m.picker.searchable || m.picker.Callback != openRouterCatalogCallback {
		t.Fatal("searchable catalog picker was not opened")
	}
	m, _ = callUpdate(m, openRouterModelPickedMsg{ID: "openai/gpt"})
	if pinned != "openai/gpt" || m.activeProvider != providerOpenRouter || m.activeModel != "openai/gpt" {
		t.Fatalf("pinned=%q provider=%q model=%q", pinned, m.activeProvider, m.activeModel)
	}
}

func TestSearchablePickerFiltersByNameAndID(t *testing.T) {
	var picker PickerView
	picker.OpenSearch("Search", []sdk.ShowPickerItem{
		{ID: "anthropic/claude", Label: "Claude"},
		{ID: "openai/gpt", Label: "GPT"},
	}, openRouterCatalogCallback)
	picker.SetSize(80, 20)
	picker.HandleKey(tea.KeyPressMsg{Text: "g"})
	picker.HandleKey(tea.KeyPressMsg{Text: "p"})
	selected, id, _ := picker.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !selected || id != "openai/gpt" {
		t.Fatalf("selected=%v id=%q", selected, id)
	}
	picker.HandleKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if picker.query != "g" {
		t.Fatalf("query after backspace = %q", picker.query)
	}
	picker.HandleKey(tea.KeyPressMsg{Text: "zzzz"})
	selected, _, cancelled := picker.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if selected || cancelled || !picker.IsActive() {
		t.Fatal("empty search result should keep the picker open")
	}
}
