package harness_test

import (
	"testing"

	"github.com/mattdurham/wllr/harness"
	"github.com/mattdurham/wllr/sdk"
)

// fakeRenderer satisfies Renderer for tests.
type fakeRenderer struct{ calls []string }

func (f *fakeRenderer) AppendToken(t string)                                           { f.calls = append(f.calls, "token:"+t) }
func (f *fakeRenderer) FinalizeMessage()                                               { f.calls = append(f.calls, "finalize") }
func (f *fakeRenderer) AddUserMessage(_, _ string)                                     { f.calls = append(f.calls, "user") }
func (f *fakeRenderer) AddNotification(_ string)                                       { f.calls = append(f.calls, "notify") }
func (f *fakeRenderer) SetStreaming(_ bool, _ error)                                   {}
func (f *fakeRenderer) ShowModal(_ string)                                             {}
func (f *fakeRenderer) ShowPicker(_ string, _ []sdk.ShowPickerItem, _ string)          {}
func (f *fakeRenderer) AddToolCall(_, _, _ string)                                     {}
func (f *fakeRenderer) UpdateToolCall(_ string, _ bool, _ string)                      {}
func (f *fakeRenderer) SetStatus(_, _ string)                                          {}
func (f *fakeRenderer) AppendConsoleLine(_ string)                                     {}
func (f *fakeRenderer) ClearConsole()                                                  {}
func (f *fakeRenderer) Abort()                                                         {}
func (f *fakeRenderer) ResetHistory(_ []sdk.Message) error                             { return nil }

// compile-time check
var _ harness.Renderer = (*fakeRenderer)(nil)

func TestRenderer_InterfaceSatisfied(t *testing.T) {
	var r harness.Renderer = &fakeRenderer{}
	r.AppendToken("hello")
	r.FinalizeMessage()
}
