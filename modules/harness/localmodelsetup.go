package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// providerLocal mirrors cmd/provider.go's constant of the same name. Harness
// cannot import cmd/ (cmd/ imports harness), so the value is duplicated here.
const providerLocal = "local"

// localModelBaseURLCallback routes a submitted base-URL text input to the
// local-model discovery flow instead of dispatching EventOnCommand.
const localModelBaseURLCallback = "__wllr:local_base_url"

// localModelPickerCallback routes a picker selection from the discovered
// local-model list to localModelPickedMsg.
const localModelPickerCallback = "__wllr:local_model_pick"

// localModelManualFieldCallback routes a submitted manual-fallback field to
// the local-model manual entry flow.
const localModelManualFieldCallback = "__wllr:local_manual_field"

// ErrLocalModelSetupNeeded is returned by SelectProviderFn when the local
// provider has no configured local models, redirecting the caller into the
// interactive local-model setup flow instead of failing outright.
var ErrLocalModelSetupNeeded = errors.New("local model setup needed")

// LocalModelChoice is one model discovered by probing a local OpenAI-compatible
// endpoint's /models listing.
type LocalModelChoice struct {
	ID            string
	Name          string
	ContextWindow int64
}

// LocalModelEntry is a local model chosen or manually entered by the user,
// ready to be persisted and applied as the active provider/model.
type LocalModelEntry struct {
	ID            string
	Name          string
	BaseURL       string
	APIKey        string
	ContextWindow int64
}

// showLocalModelSetupMsg starts the local-model setup flow by opening the
// base-URL prompt. Emitted when SelectProviderFn reports ErrLocalModelSetupNeeded.
type showLocalModelSetupMsg struct{}

// localModelBaseURLEnteredMsg carries the base URL submitted from the setup prompt.
type localModelBaseURLEnteredMsg struct{ URL string }

// LocalModelProbeStatus classifies the outcome of probing a base URL, so the
// setup flow can distinguish a wrong/unreachable endpoint (which should be
// re-prompted, not silently downgraded) from one that responded with nothing
// usable (which falls back to manual entry).
type LocalModelProbeStatus int

const (
	// LocalModelProbeOK means the endpoint returned at least one usable model.
	LocalModelProbeOK LocalModelProbeStatus = iota
	// LocalModelProbeUnreachable means the request itself failed to complete
	// (bad URL, connection refused, timeout, DNS failure) — the endpoint is
	// likely misconfigured, so the user should be asked to re-enter it.
	LocalModelProbeUnreachable
	// LocalModelProbeEmpty means the endpoint was reached but returned no
	// usable models (empty list, unexpected shape) — falls back to manual entry.
	LocalModelProbeEmpty
)

// localModelProbeResultMsg carries the outcome of probing a base URL for an
// OpenAI-compatible model list.
type localModelProbeResultMsg struct {
	BaseURL string
	Models  []LocalModelChoice
	Status  LocalModelProbeStatus
}

// localModelPickedMsg carries the model chosen from the discovered-models picker.
type localModelPickedMsg struct {
	ID            string
	Name          string
	ContextWindow int64
}

// localModelManualFieldEnteredMsg carries a single field submitted during the
// manual-entry fallback sequence.
type localModelManualFieldEnteredMsg struct{ Value string }

// localModelManualField describes one step of the manual-entry fallback sequence.
type localModelManualField struct {
	Title       string
	Placeholder string
}

// localModelManualFields is the ordered manual-entry field sequence: model id,
// display name, context window, and an optional API key.
var localModelManualFields = []localModelManualField{
	{Title: "Model ID  (enter · esc)", Placeholder: "llama3.2"},
	{Title: "Display name  (enter · esc)", Placeholder: "Llama 3.2"},
	{Title: "Context window in tokens  (enter · esc)", Placeholder: "131072"},
	{Title: "API key — optional, enter to skip  (enter · esc)", Placeholder: ""},
}

// openLocalModelBaseURLPrompt opens the text input asking for a local
// OpenAI-compatible endpoint base URL.
func (m *Model) openLocalModelBaseURLPrompt() {
	m.textInput.Open(
		"Local model endpoint  (enter · esc)",
		"http://localhost:11434/v1",
		"",
		localModelBaseURLCallback,
	)
	m.textInput.SetSize(m.width, m.chatHeight())
}

// openNextManualField opens the text input for the current manual-entry step.
// No-op if all steps have already been completed.
func (m *Model) openNextManualField() {
	if m.localSetupManualStep < 0 || m.localSetupManualStep >= len(localModelManualFields) {
		return
	}
	field := localModelManualFields[m.localSetupManualStep]
	m.textInput.Open(field.Title, field.Placeholder, "", localModelManualFieldCallback)
	m.textInput.SetSize(m.width, m.chatHeight())
}

// resetLocalModelSetupState clears all in-progress local-model setup fields,
// used on cancel so a later attempt starts fresh.
func (m *Model) resetLocalModelSetupState() {
	m.localSetupBaseURL = ""
	m.localSetupModels = nil
	m.localSetupManualStep = 0
	m.localSetupManualEntry = LocalModelEntry{}
}

// probeLocalModelsCmd returns a tea.Cmd that probes baseURL asynchronously via
// ProbeLocalModelsFn and reports the outcome as localModelProbeResultMsg.
// msg.BaseURL is the resolved base URL that actually worked (which may differ
// from the input baseURL, e.g. with "/v1" appended) when Status is
// LocalModelProbeOK; otherwise it echoes the input baseURL.
func (m *Model) probeLocalModelsCmd(baseURL string) tea.Cmd {
	probe := m.ProbeLocalModelsFn
	return func() tea.Msg {
		if probe == nil {
			return localModelProbeResultMsg{BaseURL: baseURL, Status: LocalModelProbeUnreachable}
		}
		models, resolvedBaseURL, status := probe(baseURL)
		if status != LocalModelProbeOK || resolvedBaseURL == "" {
			resolvedBaseURL = baseURL
		}
		return localModelProbeResultMsg{BaseURL: resolvedBaseURL, Models: models, Status: status}
	}
}

// applyLocalModelPick persists and applies the chosen local model via
// SaveLocalModelFn, notifying success/failure, and updates the active
// provider/model UI state.
func (m *Model) applyLocalModelPick(entry LocalModelEntry) tea.Cmd {
	defer m.resetLocalModelSetupState()
	if m.SaveLocalModelFn == nil {
		m.pushNotification("⚠ could not save local model: local model persistence is not available")
		return nil
	}
	modelID, err := m.SaveLocalModelFn(entry)
	if err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not save local model: %v", err))
		return nil
	}
	m.pushNotification("✓ Local model set to: " + modelID)
	if entry.ContextWindow <= 0 {
		m.openContextWindowPrompt(providerLocal, modelID)
		return nil
	}
	return m.setActiveProviderModel(providerLocal, modelID)
}

// hasUsableLocalModel reports whether the local provider currently has a
// usable model configured, via HasLocalModelFn. Nil-safe: assumes true when
// unset so callers do not redirect unnecessarily.
func (m *Model) hasUsableLocalModel() bool {
	if m.HasLocalModelFn == nil {
		return true
	}
	return m.HasLocalModelFn()
}

// parseContextWindowLoose parses a manual context-window entry as a best-effort
// integer. Context window is optional metadata, so empty or unparseable input
// yields 0 rather than an error.
func parseContextWindowLoose(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// openModelPickerFromProbe builds and opens the discovered-models picker.
func (m *Model) openModelPickerFromProbe(models []LocalModelChoice) {
	m.localSetupModels = models
	items := make([]sdk.ShowPickerItem, 0, len(models))
	for _, choice := range models {
		sublabel := ""
		if choice.ContextWindow > 0 {
			sublabel = fmt.Sprintf("%dk ctx", choice.ContextWindow/1000)
		}
		items = append(items, sdk.ShowPickerItem{ID: choice.ID, Label: choice.Name, Sublabel: sublabel})
	}
	m.picker.Open("Select a local model  (↑↓ · enter · esc)", items, localModelPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}
