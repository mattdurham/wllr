package harness

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSelectProviderFn_LocalSetupNeeded_ShowsSetupFlow(t *testing.T) {
	m := newTestModel()
	m.SelectProviderFn = func(provider string) (string, bool, error) {
		return "", false, ErrLocalModelSetupNeeded
	}

	cmd := m.applyLoginProviderSelection(providerLocal)
	if cmd == nil {
		t.Fatal("expected a non-nil command redirecting into local model setup")
	}
	msg := cmd()
	if _, ok := msg.(showLocalModelSetupMsg); !ok {
		t.Fatalf("msg = %T, want showLocalModelSetupMsg", msg)
	}

	m2, cmd2 := callUpdate(m, msg)
	if !m2.textInput.IsActive() {
		t.Error("text input should be active after showLocalModelSetupMsg")
	}
	if m2.textInput.Callback != localModelBaseURLCallback {
		t.Errorf("callback = %q, want %q", m2.textInput.Callback, localModelBaseURLCallback)
	}
	_ = cmd2
}

func TestLocalModelSetup_DiscoverySuccess_FullFlow(t *testing.T) {
	m := newTestModel()
	var savedEntry LocalModelEntry
	m.ProbeLocalModelsFn = func(baseURL string) ([]LocalModelChoice, string, LocalModelProbeStatus) {
		if baseURL != "http://localhost:11434/v1" {
			t.Errorf("probe baseURL = %q", baseURL)
		}
		return []LocalModelChoice{{ID: "llama3.2", Name: "Llama 3.2", ContextWindow: 131072}}, baseURL, LocalModelProbeOK
	}
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		savedEntry = entry
		return entry.ID, nil
	}

	m, cmd := callUpdate(m, localModelBaseURLEnteredMsg{URL: "http://localhost:11434/v1"})
	if m.localSetupBaseURL != "http://localhost:11434/v1" {
		t.Fatalf("localSetupBaseURL = %q", m.localSetupBaseURL)
	}
	if cmd == nil {
		t.Fatal("expected probe command")
	}
	probeMsg := cmd()
	result, ok := probeMsg.(localModelProbeResultMsg)
	if !ok {
		t.Fatalf("probe msg = %T, want localModelProbeResultMsg", probeMsg)
	}
	if result.Status != LocalModelProbeOK || len(result.Models) != 1 {
		t.Fatalf("probe result = %+v", result)
	}

	m, _ = callUpdate(m, result)
	if !m.picker.IsActive() || m.picker.Callback != localModelPickerCallback {
		t.Fatalf("expected local model picker open, got active=%v callback=%q", m.picker.IsActive(), m.picker.Callback)
	}
	if len(m.localSetupModels) != 1 || m.localSetupModels[0].ID != "llama3.2" {
		t.Fatalf("localSetupModels = %+v", m.localSetupModels)
	}

	// Simulate the picker selection routing (as updateKeyPressPicker would do).
	m, cmd = callUpdate(m, localModelPickedMsg{ID: "llama3.2", Name: "Llama 3.2", ContextWindow: 131072})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Logf("post-save cmd msg = %T", msg)
		}
	}

	if savedEntry.ID != "llama3.2" || savedEntry.BaseURL != "http://localhost:11434/v1" || savedEntry.ContextWindow != 131072 {
		t.Fatalf("SaveLocalModelFn entry = %+v", savedEntry)
	}
	if m.localSetupBaseURL != "" || m.localSetupModels != nil {
		t.Errorf("local setup state should be cleared after save, got baseURL=%q models=%v", m.localSetupBaseURL, m.localSetupModels)
	}
}

func TestLocalModelSetup_ManualFallback_FullFlow(t *testing.T) {
	m := newTestModel()
	var savedEntry LocalModelEntry
	saveCalled := 0
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		saveCalled++
		savedEntry = entry
		return entry.ID, nil
	}

	m, _ = callUpdate(m, localModelBaseURLEnteredMsg{URL: "http://localhost:8000/v1"})
	m, _ = callUpdate(m, localModelProbeResultMsg{BaseURL: "http://localhost:8000/v1", Status: LocalModelProbeEmpty})
	if !m.textInput.IsActive() || m.textInput.Callback != localModelManualFieldCallback {
		t.Fatalf("expected manual field prompt open, got active=%v callback=%q", m.textInput.IsActive(), m.textInput.Callback)
	}
	if m.localSetupManualStep != 0 {
		t.Fatalf("localSetupManualStep = %d, want 0", m.localSetupManualStep)
	}

	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "my-model-id"})
	if saveCalled != 0 {
		t.Fatal("SaveLocalModelFn should not be called before all fields are entered")
	}
	if m.localSetupManualStep != 1 {
		t.Fatalf("localSetupManualStep = %d, want 1", m.localSetupManualStep)
	}

	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "My Model"})
	if saveCalled != 0 {
		t.Fatal("SaveLocalModelFn should not be called before all fields are entered")
	}

	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "8192"})
	if saveCalled != 0 {
		t.Fatal("SaveLocalModelFn should not be called before all fields are entered")
	}

	m, cmd := callUpdate(m, localModelManualFieldEnteredMsg{Value: ""})
	_ = cmd

	if saveCalled != 1 {
		t.Fatalf("SaveLocalModelFn called %d times, want 1", saveCalled)
	}
	if savedEntry.ID != "my-model-id" || savedEntry.Name != "My Model" ||
		savedEntry.BaseURL != "http://localhost:8000/v1" || savedEntry.ContextWindow != 8192 || savedEntry.APIKey != "" {
		t.Fatalf("SaveLocalModelFn entry = %+v", savedEntry)
	}
	if m.localSetupManualStep != 0 {
		t.Errorf("localSetupManualStep should reset to 0, got %d", m.localSetupManualStep)
	}
}

func TestLocalModelSetup_ManualFallback_DefaultsNameToID(t *testing.T) {
	m := newTestModel()
	var savedEntry LocalModelEntry
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		savedEntry = entry
		return entry.ID, nil
	}

	m, _ = callUpdate(m, localModelBaseURLEnteredMsg{URL: "http://localhost:8000/v1"})
	m, _ = callUpdate(m, localModelProbeResultMsg{Status: LocalModelProbeEmpty})
	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "id-only"})
	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: ""}) // blank name -> defaults to ID
	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "not-a-number"})
	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "key123"})

	if savedEntry.Name != "id-only" {
		t.Errorf("Name = %q, want default to ID", savedEntry.Name)
	}
	if savedEntry.ContextWindow != 0 {
		t.Errorf("ContextWindow = %d, want 0 for unparseable input", savedEntry.ContextWindow)
	}
	if savedEntry.APIKey != "key123" {
		t.Errorf("APIKey = %q, want key123", savedEntry.APIKey)
	}
}

func TestLocalModelSetup_ManualFallback_EmptyIDReprompts(t *testing.T) {
	m := newTestModel()
	saveCalled := false
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		saveCalled = true
		return entry.ID, nil
	}

	m, _ = callUpdate(m, localModelBaseURLEnteredMsg{URL: "http://localhost:8000/v1"})
	m, _ = callUpdate(m, localModelProbeResultMsg{Status: LocalModelProbeEmpty})
	m, _ = callUpdate(m, localModelManualFieldEnteredMsg{Value: "   "})

	if m.localSetupManualStep != 0 {
		t.Errorf("localSetupManualStep should stay at 0 on empty id, got %d", m.localSetupManualStep)
	}
	if !m.textInput.IsActive() {
		t.Error("text input should remain open re-prompting for model id")
	}
	if saveCalled {
		t.Error("SaveLocalModelFn should not be called")
	}
}

func TestLocalModelSetup_EscCancels_NoSave(t *testing.T) {
	m := newTestModel()
	saveCalled := false
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		saveCalled = true
		return entry.ID, nil
	}

	m.openLocalModelBaseURLPrompt()
	m.localSetupBaseURL = "http://stale.example/v1"
	m.localSetupModels = []LocalModelChoice{{ID: "stale"}}

	m2, _, handled := m.updateKeyPressTextInput(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("esc should be handled")
	}
	if m2.textInput.IsActive() {
		t.Error("text input should close on esc")
	}
	if m2.localSetupBaseURL != "" || m2.localSetupModels != nil {
		t.Errorf("local setup state should reset on esc, got baseURL=%q models=%v", m2.localSetupBaseURL, m2.localSetupModels)
	}
	if saveCalled {
		t.Error("SaveLocalModelFn should not be called on cancel")
	}
}

func TestLocalModelSetup_Unreachable_RepromptsForURL(t *testing.T) {
	m := newTestModel()
	saveCalled := false
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		saveCalled = true
		return entry.ID, nil
	}

	m, _ = callUpdate(m, localModelBaseURLEnteredMsg{URL: "http://192.168.4.20/v1"})
	if m.localSetupBaseURL != "http://192.168.4.20/v1" {
		t.Fatalf("localSetupBaseURL = %q", m.localSetupBaseURL)
	}

	m, _ = callUpdate(m, localModelProbeResultMsg{BaseURL: "http://192.168.4.20/v1", Status: LocalModelProbeUnreachable})

	if !m.textInput.IsActive() || m.textInput.Callback != localModelBaseURLCallback {
		t.Fatalf("expected re-prompt for base URL, got active=%v callback=%q", m.textInput.IsActive(), m.textInput.Callback)
	}
	if m.localSetupBaseURL != "" {
		t.Errorf("localSetupBaseURL should reset on unreachable, got %q", m.localSetupBaseURL)
	}
	if m.localSetupManualStep != 0 || (LocalModelEntry{}) != m.localSetupManualEntry {
		t.Error("manual-entry fallback should not start on an unreachable endpoint")
	}
	if saveCalled {
		t.Error("SaveLocalModelFn should not be called when the endpoint is unreachable")
	}
}

func TestApplyLocalModelPick_SaveError_Notifies(t *testing.T) {
	m := newTestModel()
	m.SaveLocalModelFn = func(entry LocalModelEntry) (string, error) {
		return "", errors.New("boom")
	}
	m.localSetupBaseURL = "http://localhost:11434/v1"
	m.localSetupModels = []LocalModelChoice{{ID: "x"}}

	cmd := m.applyLocalModelPick(LocalModelEntry{ID: "x"})
	if cmd != nil {
		t.Error("expected nil cmd on save error")
	}
	if m.localSetupBaseURL != "" || m.localSetupModels != nil {
		t.Error("local setup state should still reset after a save error")
	}
}
