package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// FuncSink is a programmatic UISink backed by optional function fields.
//
// It is the "programmatic driven" front end: no terminal and no HTTP server,
// just callbacks an embedder wires. A nil field is a no-op (or a zero return
// for the value-returning methods), so an embedder implements only the effects
// it cares about. Because every field is optional, FuncSink is also the
// convenient test double for code that emits engine effects.
//
// FuncSink is safe for concurrent use as long as the functions it holds are.
type FuncSink struct {
	OnNotify          func(text string)
	OnShowModal       func(text string)
	OnShowPicker      func(params ShowPickerParams)
	OnShowAgentTree   func(params ShowAgentTreeParams)
	OnFocusAgent      func(id string)
	OnShowTextInput   func(title, placeholder, initialValue, callback string)
	OnAbort           func()
	OnSetStatus       func(key, value string)
	OnStatusInfo      func() StatusInfo
	OnSubmit          func(content, display string)
	OnSetModel        func(value, thinking string) error
	OnRegisterCommand func(name, desc string, instant bool) error
	OnConsole         func(line string, clear bool)
	OnResetHistory    func(messages []Message)
	OnToken           func(text string)
	OnToolCallStart   func(agentID, id, toolName, input string)
	OnToolCallDone    func(agentID, id, toolName, output string, isError bool)
	OnStreamDone      func(err error)
	OnAgentWakeup     func(agentID string)
	OnSceneCreate     func(area UIArea) error
	OnScenePatch      func(params UIPatchParams) error
	OnSceneUpdate     func(params UIUpdateAreaParams) error
	OnSceneRemove     func(id string)
}

// Proof FuncSink satisfies the renderer-neutral sink at compile time.
var _ UISink = (*FuncSink)(nil)

func (s *FuncSink) Notify(text string) {
	if s.OnNotify != nil {
		s.OnNotify(text)
	}
}

func (s *FuncSink) ShowModal(text string) {
	if s.OnShowModal != nil {
		s.OnShowModal(text)
	}
}

func (s *FuncSink) ShowPicker(params ShowPickerParams) {
	if s.OnShowPicker != nil {
		s.OnShowPicker(params)
	}
}

func (s *FuncSink) ShowAgentTree(params ShowAgentTreeParams) {
	if s.OnShowAgentTree != nil {
		s.OnShowAgentTree(params)
	}
}

func (s *FuncSink) FocusAgent(id string) {
	if s.OnFocusAgent != nil {
		s.OnFocusAgent(id)
	}
}

func (s *FuncSink) ShowTextInput(title, placeholder, initialValue, callback string) {
	if s.OnShowTextInput != nil {
		s.OnShowTextInput(title, placeholder, initialValue, callback)
	}
}

func (s *FuncSink) Abort() {
	if s.OnAbort != nil {
		s.OnAbort()
	}
}

func (s *FuncSink) SetStatus(key, value string) {
	if s.OnSetStatus != nil {
		s.OnSetStatus(key, value)
	}
}

func (s *FuncSink) StatusInfo() StatusInfo {
	if s.OnStatusInfo != nil {
		return s.OnStatusInfo()
	}
	return StatusInfo{Statuses: map[string]string{}}
}

func (s *FuncSink) Submit(content, display string) {
	if s.OnSubmit != nil {
		s.OnSubmit(content, display)
	}
}

func (s *FuncSink) SetModel(value, thinking string) error {
	if s.OnSetModel != nil {
		return s.OnSetModel(value, thinking)
	}
	return nil
}

func (s *FuncSink) RegisterCommand(name, desc string, instant bool) error {
	if s.OnRegisterCommand != nil {
		return s.OnRegisterCommand(name, desc, instant)
	}
	return nil
}

func (s *FuncSink) Console(line string, clear bool) {
	if s.OnConsole != nil {
		s.OnConsole(line, clear)
	}
}

func (s *FuncSink) ResetHistory(messages []Message) {
	if s.OnResetHistory != nil {
		s.OnResetHistory(messages)
	}
}

func (s *FuncSink) Token(text string) {
	if s.OnToken != nil {
		s.OnToken(text)
	}
}

func (s *FuncSink) ToolCallStart(agentID, id, toolName, input string) {
	if s.OnToolCallStart != nil {
		s.OnToolCallStart(agentID, id, toolName, input)
	}
}

func (s *FuncSink) ToolCallDone(agentID, id, toolName, output string, isError bool) {
	if s.OnToolCallDone != nil {
		s.OnToolCallDone(agentID, id, toolName, output, isError)
	}
}

func (s *FuncSink) StreamDone(err error) {
	if s.OnStreamDone != nil {
		s.OnStreamDone(err)
	}
}

func (s *FuncSink) AgentWakeup(agentID string) {
	if s.OnAgentWakeup != nil {
		s.OnAgentWakeup(agentID)
	}
}

func (s *FuncSink) SceneCreate(area UIArea) error {
	if s.OnSceneCreate != nil {
		return s.OnSceneCreate(area)
	}
	return nil
}

func (s *FuncSink) ScenePatch(params UIPatchParams) error {
	if s.OnScenePatch != nil {
		return s.OnScenePatch(params)
	}
	return nil
}

func (s *FuncSink) SceneUpdate(params UIUpdateAreaParams) error {
	if s.OnSceneUpdate != nil {
		return s.OnSceneUpdate(params)
	}
	return nil
}

func (s *FuncSink) SceneRemove(id string) {
	if s.OnSceneRemove != nil {
		s.OnSceneRemove(id)
	}
}
