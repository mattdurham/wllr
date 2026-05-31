package extension_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// fakeAgentBridge is a test double that satisfies AgentBridge.
type fakeAgentBridge struct{}

func (f *fakeAgentBridge) Spawn(_ context.Context, _ extension.SpawnRequest) error { return nil }
func (f *fakeAgentBridge) Close(_ string) error                                    { return nil }
func (f *fakeAgentBridge) SendMessage(_, _ string) error                           { return nil }
func (f *fakeAgentBridge) Run(_ string) error                                      { return nil }
func (f *fakeAgentBridge) List() ([]extension.AgentInfo, error)                    { return nil, nil }
func (f *fakeAgentBridge) TokenCount() int64                                       { return 0 }
func (f *fakeAgentBridge) SetHistory(_ string, _ []sdk.Message) error { return nil }

// compile-time check
var _ extension.AgentBridge = (*fakeAgentBridge)(nil)

func TestAgentBridge_InterfaceSatisfied(t *testing.T) {
	var b extension.AgentBridge = &fakeAgentBridge{}
	if err := b.Spawn(context.Background(), extension.SpawnRequest{ID: "test"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
}

// fakeTeamBridge satisfies TeamBridge.
type fakeTeamBridge struct{}

func (f *fakeTeamBridge) Create(_, _ string) error                { return nil }
func (f *fakeTeamBridge) Close(_ context.Context, _ string) error { return nil }
func (f *fakeTeamBridge) AddMember(_, _ string) error             { return nil }
func (f *fakeTeamBridge) RemoveMember(_, _ string) error          { return nil }
func (f *fakeTeamBridge) GetMembers(_ string) ([]string, error)   { return nil, nil }
func (f *fakeTeamBridge) List() ([]string, error)                 { return nil, nil }

var _ extension.TeamBridge = (*fakeTeamBridge)(nil)

func TestTeamBridge_InterfaceSatisfied(t *testing.T) {
	var b extension.TeamBridge = &fakeTeamBridge{}
	if err := b.Create("t1", "team1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// fakeCapabilityProvider satisfies CapabilityProvider.
type fakeCapabilityProvider struct{}

func (f *fakeCapabilityProvider) Exec(_ context.Context, _, _ string, _ func(string)) (string, error) {
	return "", nil
}
func (f *fakeCapabilityProvider) GetEnv(_ string) (string, error)   { return "", nil }
func (f *fakeCapabilityProvider) ReadFile(_ string) (string, error) { return "", nil }
func (f *fakeCapabilityProvider) WriteFile(_, _ string) error       { return nil }
func (f *fakeCapabilityProvider) HTTPPost(_ string, _ map[string]string, _ []byte) (int, []byte, error) {
	return 200, nil, nil
}
func (f *fakeCapabilityProvider) ConfigRead(_ string) (json.RawMessage, error) { return nil, nil }

var _ extension.CapabilityProvider = (*fakeCapabilityProvider)(nil)

func TestCapabilityProvider_InterfaceSatisfied(t *testing.T) {
	var p extension.CapabilityProvider = &fakeCapabilityProvider{}
	if _, err := p.GetEnv("HOME"); err != nil {
		t.Fatalf("GetEnv: %v", err)
	}
}

// fakeUIBridge satisfies UIBridge.
type fakeUIBridge struct{}

func (f *fakeUIBridge) Notify(_ string)                                       {}
func (f *fakeUIBridge) ShowModal(_ string)                                    {}
func (f *fakeUIBridge) ShowPicker(_ string, _ []sdk.ShowPickerItem, _ string) {}
func (f *fakeUIBridge) Abort()                                                {}
func (f *fakeUIBridge) SetStatus(_, _ string)                                 {}
func (f *fakeUIBridge) GetStatusInfo() sdk.StatusInfo                         { return sdk.StatusInfo{} }
func (f *fakeUIBridge) SendMessage(_ sdk.Message)                             {}
func (f *fakeUIBridge) RegisterCommand(_, _ string) error                     { return nil }
func (f *fakeUIBridge) RegisterTool(_ sdk.Tool) error                         { return nil }
func (f *fakeUIBridge) SetSystemPrompt(_ string)                              {}
func (f *fakeUIBridge) AppendSystemPrompt(_ string)                           {}
func (f *fakeUIBridge) ResetHistory(_ []sdk.Message) error                    { return nil }
func (f *fakeUIBridge) ToolResult(_, _ string, _ bool)                        {}
func (f *fakeUIBridge) AfterToolCall(_, _, _ string, _ bool)                  {}
func (f *fakeUIBridge) ConsoleOutput(_ string)                                {}
func (f *fakeUIBridge) ConsoleClear()                                         {}

var _ extension.UIBridge = (*fakeUIBridge)(nil)

func TestUIBridge_InterfaceSatisfied(t *testing.T) {
	var b extension.UIBridge = &fakeUIBridge{}
	b.Notify("hello")
	b.Abort()
}

// fakeMCPBridge satisfies MCPBridge.
type fakeMCPBridge struct{}

func (f *fakeMCPBridge) Spawn(_, _ string, _ []string, _ map[string]string) error { return nil }
func (f *fakeMCPBridge) Close(_ string) error                                     { return nil }
func (f *fakeMCPBridge) Send(_ string, _ []byte) error                            { return nil }
func (f *fakeMCPBridge) Read(_ string) (json.RawMessage, error)                   { return nil, nil }

var _ extension.MCPBridge = (*fakeMCPBridge)(nil)

func TestMCPBridge_InterfaceSatisfied(t *testing.T) {
	var b extension.MCPBridge = &fakeMCPBridge{}
	if err := b.Spawn("s1", "cmd", nil, nil); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
}
