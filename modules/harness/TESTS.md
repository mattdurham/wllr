# harness — Test Specifications

## Existing Tests

### model_test.go

| Test | Scenario | Setup | Assertions |
|---|---|---|---|
| `TestModel_Init_ReturnsCmd` | Init returns a non-nil Cmd | `newTestModel()` | `Init()` result is non-nil |
| `TestModel_View_ReturnsNonEmpty` | View produces content | `newTestModel()` | `View().Content != ""` |
| `TestNew_SeedsActiveModelFromMainAgent` | Startup status state reflects the spawned main agent's model | pool with provider/default model and main agent | `activeProvider`, `activeModel`, and `live.model` are seeded |
| `TestModel_Update_TokenMsg_AppendsToChat` | Tokens accumulate in `chat.current` | `m.streaming = true`; send two `TokenMsg` | `chat.current` equals concatenated tokens after each append |
| `TestModel_Update_TokenMsg_UpdatesLiveTokenSnapshot` | Token batches refresh statusline token state during streaming | pool token count set; send `TokenMsg` | `live.tokens` matches `AgentPool.TokenCount()` |
| `TestModel_Update_StreamDoneMsg_ClearsStreaming` | StreamDoneMsg ends stream | `m.streaming = true`, token appended | `streaming == false` after `StreamDoneMsg{Err: nil}` |
| `TestModel_Update_StreamDoneMsg_Error_ShowsError` | Non-canceled error adds notification | `m.streaming = true` | `streaming == false`; `chat.messages` is non-empty (error notification added) |
| `TestModel_Update_StreamDoneMsg_ContextCanceled_NoError` | `context.Canceled` is not shown as error | `m.streaming = true`, partial token | `streaming == false`; no system notification message in chat |
| `TestModel_Update_ReloadMsg_TriggersExtensionReload` | ReloadMsg returns a Cmd that resolves to NotifyMsg | `newTestModel()` (nil host) | `cmd != nil`; executing cmd yields `NotifyMsg` |
| `TestModel_Update_ClearMsg_ClearsHistory` | clearMsg empties history and chat | history + chat pre-populated | `len(m.history) == 0`; `len(m.chat.messages) == 0` |
| `TestModel_Update_SetModelMsg` | setModelMsg updates activeModel and live.model | `newTestModel()` | `m.activeModel` and `m.live.model` equal new model name |
| `TestModel_Update_CommandMsg_Clear` | /clear command dispatched via registry | history pre-populated | `len(m.history) == 0` after processing |
| `TestModel_Update_CommandMsg_UnknownCommand` | Unknown command yields NotifyMsg | `newTestModel()` | `cmd != nil`; msg is `NotifyMsg` with non-empty text |
| `TestModel_Update_SubmitMsg_StartsStream` | SubmitMsg starts streaming | `newTestModel()` with mock provider | `m.streaming == true`; `cmd != nil` |
| `TestModel_Update_SubmitMsg_IgnoredWhileStreaming` | SubmitMsg dropped while streaming | `m.streaming = true` | No panic; model consistent |
| `TestModel_Update_NotifyMsg` | NotifyMsg adds to chat | `newTestModel()` | `len(m.chat.messages) > 0` |
| `TestModel_Update_StatusUpdateMsg` | StatusUpdateMsg updates live.statuses | `newTestModel()` | `m.live.getStatus("foo") == "bar"` |
| `TestModel_Update_WindowSizeMsg` | WindowSizeMsg updates dimensions | `newTestModel()` | `m.width == 120`, `m.height == 40` |
| `TestModel_NilLangModel_StreamError` | nil langModel returns StreamDoneMsg with error | `New(nil, "none", nil)` | `streaming == true` before cmd runs; `StreamDoneMsg.Err != nil` |

---

### ChatView (chat_test.go removed)

The built-in `ChatView` message renderer was removed (the transcript is produced
by a WASM extension via the scene graph). Its rendering tests
(`chat_test.go`, `chat_render_test.go`) were deleted. Transcript behavior is now
covered end-to-end by `test/wasmchat`; the harness-side viewport/scroll, tool
log, and reset behavior are covered by `wasmchat_test.go`, `tui_test.go`, and
`interactions_test.go`.

| Test | Scenario | Setup | Expected |
|---|---|---|---|
| `TestChatView_SetExternalContent_FollowsWhenAtBottom` | transcript content grows while viewport is at bottom | set external content, then replace with more lines | viewport remains at bottom |
| `TestChatView_SetExternalContent_PreservesScrollback` | transcript content grows while user is scrolled up | set external content, scroll up, then replace with more lines | viewport offset is preserved and does not jump to bottom |
| `TestChatView_ToolActivityLines_ShowsLastThreeAndMatchesDoneByID` | compact tool rows use the latest entries and completion matches by ID | add four calls, complete the second by ID | latest three rows render; matching entry is done with its sub-agent label, last pending is unchanged |
| `TestChatView_UpdateToolCall_CreatesEntryForMissingStart` | completion arrives without a visible start (e.g. legacy/sub-agent bridge edge) | update an unknown tool call ID with agent/tool metadata | a completed log row is created and rendered with the sub-agent label |

---

### commands_test.go

| Test | Scenario | Setup | Assertions |
|---|---|---|---|
| `TestRegistry_Register_And_List` | Register two commands; List returns sorted | Two commands registered | `len == 2`; sorted alphabetically |
| `TestRegistry_Dispatch_KnownCommand` | Dispatch calls handler | Handler sets `called = true` | `called == true` |
| `TestRegistry_Dispatch_UnknownCommand` | Dispatch unknown name yields NotifyMsg | Empty registry | `cmd != nil`; msg is `NotifyMsg` with non-empty text |
| `TestBuiltinHistory_DispatchesExtensionEvent` | Host-reserved `/history` reaches the history extension | Built-in command registry | `dispatchOnCommandMsg.Name == "history"` |
| `TestBuiltinHelp` | Builtins are registered | `registerBuiltins(r)` | help, clear, reload, model, models all present |
| `TestBuiltinClear_EmitsMsg` | /clear emits `clearMsg` | `registerBuiltins(r)` | msg is `clearMsg{}` |
| `TestBuiltinReload_EmitsMsg` | /reload emits `ReloadMsg` | `registerBuiltins(r)` | msg is `ReloadMsg{}` |
| `TestBuiltinModel_EmitsMsg` | /model sets model name | args `["claude-haiku-3-5"]` | msg is `setModelMsg{Model: "claude-haiku-3-5"}` |
| `TestBuiltinModel_NoArgs` | /model with no args opens the picker | no args | msg is `showModelPickerMsg` |
| `TestBuiltinModels_NoArgs` | /models opens the picker | no args | msg is `showModelPickerMsg` |
| `TestOpenModelPicker_UsesChoiceSublabel` | model picker uses supplied sublabel | `ModelChoice` with local endpoint sublabel | picker item sublabel includes endpoint/context and current marker |
| `TestBuiltinThinking_EmitsMsg` | /thinking sets level directly | args `["high"]` | msg is `setThinkingMsg{Level: "high"}` |
| `TestBuiltinThinking_NoArgs` | /thinking with no args opens the picker | no args | msg is `showThinkingPickerMsg` |
| `TestRegistry_ExtensionCommand_Callable` | Extension-registered command receives args | custom handler captures args | `gotArgs == ["world"]` |

---

### default_prompt_test.go

| Test | Scenario | Setup | Assertions |
|---|---|---|---|
| `TestBuildDefaultActionPrompt_IncludesLSPGuidance` | LSP code-intelligence tools are available | tools include `lsp_diagnostics`, `lsp_lint`, navigation/reference tools, refactor preview, and `lsp_capabilities` | prompt makes LSP primary for code intelligence, tells agents to call `lsp_capabilities` at the start of repo/code work, tells agents to use LSP before broad shell search/large reads, and describes shell/manual search as fallback |
| `TestBuildDefaultActionPrompt_OmitsLSPGuidanceWithoutDiagnosticsTool` | LSP diagnostics tool is unavailable | tools include only non-LSP tools | prompt does not include Code Intelligence guidance |

---

### integration_test.go

| Test | Scenario | Setup | Assertions |
|---|---|---|---|
| `TestIntegration_FullStreamingFlow` | Full submit → stream → done cycle | mock provider with tokens | streaming true then false; history has 1 user message; `CallCount == 1` |
| `TestIntegration_UserMessageInHistory` | User message recorded immediately on SubmitMsg | mock provider | `history[0].Role == RoleUser`; `history[0].Content == "hello world"`; `chat.messages` has 1 entry |
| `TestIntegration_Esc_CancelsStream` | esc while streaming invokes cancel but does not quit | streaming started | `streaming` remains true (StreamDoneMsg not yet arrived); stream status is `cancelling…` |
| `TestIntegration_NilExtensionHost_Safe` | nil extension host never panics | `New(p, nil)` | No panic on Init, SubmitMsg, stream execution |
| `TestIntegration_ExtensionHost_NoExtensions_Safe` | Real host with no extensions loaded | `extension.NewHost(nil)` | No panic; `StreamDoneMsg.Err == nil` |

---

### authprompt_test.go

| Test | Scenario | Assertions |
|---|---|---|
| `TestApplyAuthChoice_RecordsAndClears` | Selecting an auth method records it and clears the pending provider | `RecordAuthFn` called with (provider, method); `authPromptProvider` cleared |
| `TestApplyAuthChoice_NoProvider_NoOp` | No pending provider ⇒ no record | `RecordAuthFn` not called |
| `TestSetPendingAuthProvider_DrivesInitPrompt` | Pending provider drives the startup prompt | `pendingAuthProvider` set; `Init()` returns a non-nil batch |
| `TestSetPendingSetupWizard_DrivesInitWizard` | Pending setup drives startup wizard | `pendingSetupWizard` set; `Init()` returns a non-nil batch |
| `TestLoginProviderSelected_CloudRecordsOAuthAndBeginsLogin` | Cloud wizard choice records OAuth and starts login | `RecordAuthFn` called with OAuth; `BeginOAuthFn` called; provider/model/modal/capture state set |
| `TestLoginProviderSelected_LocalDoesNotBeginLogin` | Local wizard choice skips auth | no OAuth record; `BeginOAuthFn` not called; provider/model state set |

### oauth_test.go

| Test | Scenario | Assertions |
|---|---|---|
| `TestBeginOAuthLogin_EntersCaptureMode` | Begin returns URL, enters capture mode | `oauthCaptureProvider` set; modal shown |
| `TestBeginOAuthLogin_ErrorNoCapture` | Begin error ⇒ no capture | `oauthCaptureProvider` stays empty |
| `TestCompleteOAuthLogin_CallsCompleteAndClears` | Complete calls fn and clears capture | `CompleteOAuthFn` invoked; capture cleared; success `NotifyMsg` |
| `TestBeginOAuthLogin_EntersCaptureMode` | Begin shows modal + enters capture | `oauthCaptureProvider` set; modal body shown; non-nil cmd |
| `TestBeginOAuthLogin_UnavailableWithoutCallback` | No `AwaitOAuthFn` ⇒ login unavailable | does not enter capture mode |
| `TestCompleteOAuthFromCallback_CompletesWhenCapturing` | Callback code auto-completes login | closes modal; `CompleteOAuthFn` invoked with the query; `NotifyMsg` |
| `TestCompleteOAuthFromCallback_ErrorSurfaced` | Complete error surfaced | error `NotifyMsg` |
| `TestCompleteOAuthFromCallback_IgnoredWhenNotCapturing` | No-op when not capturing or ok=false | nil command; `CompleteOAuthFn` not called |
| `TestBuiltinLogin_EmitsLoginMsg` | /login emits loginMsg | msg is `loginMsg` |

### interactions_test.go (modal wrapping)

| Test | Scenario | Assertions |
|---|---|---|
| `TestModel_Esc_DuringStream_CancelsTurn` | Esc while streaming is handled globally | no follow-up command; stream status is `cancelling…` |
| `TestModel_Esc_DuringStream_CancelsBeforeModalClose` | Esc cancellation has priority over modal close handling | stream status is `cancelling…`; modal remains open |
| `TestModel_Esc_CancelsRunningAgentWhenStreamingStateIsStale` | Esc cancels a running agent even if `m.streaming` is stale false | stream status is `cancelling…`; blocking agent stops |
| `TestModel_Esc_IdlePreservesNormalInputBehavior` | Esc still behaves normally when no turn is active | input clears; stream status is not `cancelling…` |
| `TestWrapModalLines_WrapsLongLine` | 50 runes at width 20 | wraps to 3 lines, none over width, content preserved |
| `TestWrapModalLines_PreservesShortAndBlank` | short + blank + short | unchanged |
| `TestWrapModalLines_WrapsOAuthURL` | long authorize URL | reconstructs exactly; wraps to ≥2 lines |

---

## Missing / Recommended Tests

The following scenarios are not currently covered and should be added:

| Priority | Test Name (suggested) | Scenario |
|---|---|---|
| High | `TestModel_CtrlC_Idle_Quits` | ctrl+c while idle (`streaming == false`) returns `tea.Quit` |
| High | `TestModel_CtrlQ_AlwaysQuits` | ctrl+q always returns `tea.Quit` regardless of streaming state |
| High | `TestModel_AbortStreamMsg_CancelsStream` | `abortStreamMsg{}` while streaming invokes cancel without quitting |
| High | `TestModel_AbortStreamMsg_Idle_NoOp` | `abortStreamMsg{}` while idle (cancelStream nil) does not panic |
| High | `TestModel_AddAssistantMsgToHistoryMsg` | Processing `addAssistantMsgToHistoryMsg` appends to `m.history` with `RoleAssistant` |
| Medium | `TestModel_SetActiveModel_SetsFields` | `SetActiveModel("foo")` updates both `m.activeModel` and `m.statusBar.modelName` |
| Medium | `TestModel_SetActiveModel_Empty_NoOp` | `SetActiveModel("")` leaves both fields unchanged |
| Medium | `TestModel_SetProgram_WiresCallbacks` | After `SetProgram`, `extHost.OnAbort()` sends `abortStreamMsg` to program |
| Medium | `TestModel_StreamDoneMsg_SetsStreamStatusError` | On non-canceled error, `statusBar.statuses["stream"] == "error"` |
| Medium | `TestModel_StreamDoneMsg_DeletesStreamStatus` | On success, `statusBar.statuses` does not contain key `"stream"` |
| Medium | `TestModel_CommandMsg_Help_ShowsHelpText` | `CommandMsg{Name: "help"}` adds HelpText notification without delegating to registry |
| Medium | `TestChatView_SetSize_UpdatesDimensions` | `SetSize` changes width/height and calls refreshContent |
| Medium | `TestChatView_AddNotification_SystemRole` | `AddNotification` appends a message with role `"system"` |
| Low | `TestChatView_RenderMessage_LineWrapping` | Content wider than `width` is wrapped correctly (no panic; line fits width) |
| Low | `TestChatView_FinalizeMessage_CalledTwice` | Double finalize: second call is no-op (current is empty after first) |
| Low | `TestInputArea_Enter_Empty_NoOp` | Pressing Enter on empty textarea emits no message |
| Low | `TestInputArea_Enter_Command_EmitsCommandMsg` | `/foo bar` emits `CommandMsg{Name: "foo", Args: ["bar"]}` |
| Low | `TestInputArea_Enter_PlainText_EmitsSubmitMsg` | `hello world` emits `SubmitMsg{Content: "hello world"}` |
| Low | `TestInputArea_Esc_ResetsTextarea` | Esc clears content without emitting a message |
| Low | `TestStatusBar_View_StreamingStatus` | When `statuses["stream"] == "streaming…"`, view contains that string |
| Low | `TestStatusBar_Update_SetsStatus` | `StatusUpdateMsg` sets the keyed value |
| Low | `TestStatusBar_View_SortedKeys` | Multiple status keys appear in sorted order in view output |

### console_test.go

| Test | Scenario | Setup | Assertions |
|---|---|---|---|
| `TestConsoleView_Append_SingleLine` | Single append makes line visible | `NewConsoleView()` | `View(80, 3)` contains "hello" |
| `TestConsoleView_Append_RingBuffer_EvictsOldest` | Ring buffer wraps | Append more lines than ring size | Oldest line evicted; newest visible |
| `TestConsoleView_Clear_EmptiesBuffer` | Clear removes all lines | Append then Clear | `IsEmpty() == true` |
| `TestConsoleView_View_WidthClamped` | Lines truncated at width | Append 100-char line; `View(20, 1)` | No line exceeds 20 chars |
| `TestConsoleView_Empty_ViewIsEmpty` | Empty view returns empty string | No lines appended | `IsEmpty() == true`; `View(80, 0) == ""` |
| `TestConsoleView_Visible_AfterAppend` | IsEmpty tracks append state | `NewConsoleView` → `Append` | IsEmpty false after Append |

### model_test.go (new additions)

| Test | Scenario | Setup | Assertions |
|---|---|---|---|
| `TestModel_Update_ConsoleMsg_AppendsToConsole` | ConsoleMsg makes console visible | `newTestModel()`; `ConsoleMsg{Line}` | `consoleVisible == true` |
| `TestModel_Update_ConsoleMsg_Clear_ResetsConsole` | `ConsoleMsg{Clear}` empties console | Append then Clear msg | console empty |
| `TestModel_Update_StreamDoneMsg_HidesConsole` | StreamDone hides console | `consoleVisible=true`; `StreamDoneMsg` | `consoleVisible == false` |
| `TestModel_chatHeight_AccountsForConsole` | chatHeight subtracts console height | `consoleVisible` on/off | height changes by `consolePaneLines` |
| `TestModel_ToolActivityPane_AlwaysRendersAndShowsRecentTools` | Tool pane is permanent and displays recent tool state | no tools, then running and completed tool | view always contains tools pane; row changes from running to done |
| `TestModel_ToolActivityPane_RemainsOnStreamDone` | Stream completion does not remove the tool pane | streaming model; pending tool; `StreamDoneMsg` | pane height remains `toolActivityPaneLines` and view contains tools pane |
| `TestRenderQueuedMessages` | Main-agent inbox contains a pending message | queued message snapshot and terminal width | bordered `Queued` pane renders the message separately from chat history |
| `TestModel_QueueLayoutTracksInboxTransitions` | Main-agent inbox gains and then drains a pending message | fixed terminal size and queue transitions | chat viewport shrinks/restores by the queue pane height; rendered output remains within terminal height |
| `TestModel_QueueDisplayIsBounded` | More messages are queued than the visible limit | six pending messages | total count and older-message hint render; the three-row queue body remains bounded; output remains within terminal height |
| `TestModel_QueuePaneHidesWhenItWouldPushInputOffScreen` | Queue exists in a short terminal | 15-row terminal | queue pane is omitted and the input box remains visible |
| `TestModel_QueuePaneConsumesChatHistoryBeforeInput` | Queue exists with only enough room for the lower UI | 16-row terminal | queue pane remains visible, chat height shrinks, and input remains visible |
| `TestModel_View_WithStatusLineFitsHeight` | Statusline plus input stays within terminal height | one-line status scene; small terminal height | rendered view line count equals terminal height; input bottom border present above final gutter row |
| `TestHarnessAgentBridgeListIncludesRuntimeState` | Agent bridge exposes runtime liveness in list results | running blocking agent | `AgentInfo` includes running/working state, `liveness=working`, and non-negative liveness age/duration fields |

### SceneRenderer constraint tests (scene_test.go)

| Test | Scenario | Assertion |
|------|----------|-----------|
| `TestSceneUpdateAreaConstraints` | Create area with constraints; UpdateArea raises max; error on unknown ID | Height clamped before and after update; unknown ID errors |
| `TestSceneUpdateAreaWeight` | Create area; update weight; second update without weight leaves weight unchanged | No error |
| `TestConstrainWidthAbsolute` | MinWidth=20, MaxWidth=80; pass 100, 10, 50 | Returns 80, 20, 50 respectively |
| `TestConstrainWidthPercent` | MinWidth=25%, MaxWidth=75% of 100-wide terminal | Returns 75 for both clamped cases |
| `TestConstrainHeightPercent` | MaxHeight=10% of 50-line terminal; pass 20 | Returns 5 |
| `TestConstrainUnknownAreaPassthrough` | Unknown area ID | ConstrainWidth/ConstrainHeight return inputs unchanged |
| `TestResolveConstraintEdgeCases` | Empty, non-numeric, 0%, 100% inputs | Empty=unconstrained; bad=unconstrained; 0%=0; 100%=terminal size |

## SceneRenderer (scene_test.go)

| Test | Scenario | Assertion |
|------|----------|-----------|
| `TestSceneCreateAndDuplicateArea` | Create an area, create it again, remove it | Duplicate errors; `HasArea` reflects create/remove |
| `TestSceneSetRootAndRender` | `set_root` a vstack with a text child, render | Output contains the text |
| `TestSceneAppendTextStreaming` | Seed a text node, append two deltas | Render shows the concatenated text |
| `TestSceneRenderNodeWithTextOverride` | Render one text node with a temporary text override | Output uses override and live scene remains unchanged |
| `TestSceneRenderAppendTextNode` | Render previous/current states for an appended text node | Previous omits appended suffix; current includes it; live scene remains unchanged |
| `TestSceneInsertAndRemove` | Insert two children (one at index 0), then remove one | Index-0 child renders first; removed node absent |
| `TestSceneBatchAtomicOnError` | Batch with a valid op then an op on a missing node | Batch errors; live tree unchanged (no partial apply) |
| `TestScenePatchUnknownArea` | Patch an area that does not exist | Returns an error |
| `TestSceneAppendTextRejectsNonText` | `append_text` on a vstack node | Returns an error |
| `TestSceneAreasByPlacement` | Create areas across placements | Sidebar areas returned in creation order |
| `TestSceneUnknownNodeTypeRendersEmpty` | `set_root` with an unknown node type | Render does not panic |

### Missing / Recommended Tests

| Priority | Test | Description | Success Criteria |
|---|---|---|---|
| High | `TestConsoleView_Append_LargeCount` | Append 300 lines without panic; ring buffer wraps correctly | Ring stable at 200 entries |
| Medium | `TestModel_View_ConsoleVisible_RendersFeed` | When `consoleVisible`, `View()` contains console header | View output contains "─ console" |

## WASM-Driven Chat (wasmchat_test.go)

| Test | Scenario | Assertion |
|------|----------|-----------|
| `TestRefreshWASMChat_FeedsSceneIntoViewport` | `wasmChat=true`, scene has a `chat` area | `ChatView` enters external mode; viewport content includes the scene text |
| `TestRefreshWASMChat_DisabledIsNoOp` | `wasmChat=false` | `ChatView` does not enter external mode |
| `TestRefreshWASMChat_NoAreaIsNoOp` | `wasmChat=true` but no `chat` area | `ChatView` does not enter external mode |
| `TestSceneDirty_StatuslineDoesNotRefreshChat` | statusline scene area changes | chat viewport external content is unchanged |
| `TestSceneDirty_ChatRefreshesChat` | chat scene area changes | chat viewport external content refreshes |
| `TestSceneDirty_AppendOnlyChatCoalescesRefresh` | append-only chat scene area changes | refresh is scheduled, not immediate; delayed message refreshes viewport |
| `TestSceneDirty_AppendOnlyChatUsesFastSuffixRefresh` | append-only update to trailing assistant text node | delayed refresh updates viewport by replacing the rendered node suffix |
| `TestRenderScenes_SkipsChatAreaInWASMMode` | `wasmChat=true`, `chat` area present | `renderScenes` output excludes the chat transcript (rendered in viewport instead) |
