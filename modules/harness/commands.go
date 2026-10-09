package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Registry holds registered slash commands and dispatches them.

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

// Register adds a command to the registry. Duplicate names are silently overwritten.
func (r *Registry) Register(cmd Command) {
	r.commands[cmd.Name] = cmd
}

// Dispatch looks up name and calls its handler with args.
// Returns an error Cmd if the command is not found.
func (r *Registry) Dispatch(name string, args []string) tea.Cmd {
	cmd, ok := r.commands[name]
	if !ok {
		msg := fmt.Sprintf("unknown command: /%s", name)
		return func() tea.Msg { return NotifyMsg{Text: msg} }
	}
	return cmd.Handler(args)
}

// Get returns the Command registered under name and whether it was found.
func (r *Registry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// List returns all registered commands sorted by name.
func (r *Registry) List() []Command {
	out := make([]Command, 0, len(r.commands))
	for _, c := range r.commands {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListInfo returns the registered commands as the renderer-neutral
// sdk.CommandInfo list (sorted by name). A non-terminal front end serves it so
// a browser can offer the same autocomplete dropdown and help the terminal
// does; the command set is the same because it is read from this one registry.
func (r *Registry) ListInfo() []sdk.CommandInfo {
	cmds := r.List()
	out := make([]sdk.CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, sdk.CommandInfo{Name: c.Name, Desc: c.Desc, Instant: c.Instant})
	}
	return out
}

// HelpText returns a formatted list of all commands.
func (r *Registry) HelpText() string {
	cmds := r.List()
	if len(cmds) == 0 {
		return "No commands registered."
	}
	var sb strings.Builder
	for _, c := range cmds {
		fmt.Fprintf(&sb, "  /%s — %s\n", c.Name, c.Desc)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// registerBuiltins installs the built-in commands into r.
func registerBuiltins(r *Registry) {
	r.Register(Command{
		Name:    commandHelp,
		Desc:    "Show available commands",
		Instant: true,
		Handler: func(_ []string) tea.Cmd {
			return func() tea.Msg {
				return NotifyMsg{Text: "Use the commands listed in the help text above."}
			}
		},
	})

	r.Register(Command{
		Name:    "clear",
		Desc:    "Clear the conversation history",
		Instant: true,
		Handler: func(_ []string) tea.Cmd {
			return func() tea.Msg { return clearMsg{} }
		},
	})

	r.Register(Command{
		Name:    "reload",
		Desc:    "Hot-reload all extensions",
		Instant: true,
		Handler: func(_ []string) tea.Cmd {
			return func() tea.Msg { return ReloadMsg{} }
		},
	})

	r.Register(Command{
		Name:    commandModel,
		Desc:    "Select the active model or apply a tier (/model <name|tier>; /model tiers lists tags)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			if len(args) == 0 {
				return func() tea.Msg { return showModelPickerMsg{} }
			}
			if args[0] == "tiers" {
				return func() tea.Msg { return showModelTiersMsg{} }
			}
			return func() tea.Msg { return setModelMsg{Model: args[0]} }
		},
	})

	r.Register(Command{
		Name:    commandModels,
		Desc:    "Select the active model or tag tiers (alias for /model)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			if len(args) > 0 {
				if args[0] == "tiers" {
					return func() tea.Msg { return showModelTiersMsg{} }
				}
				return func() tea.Msg { return setModelMsg{Model: args[0]} }
			}
			return func() tea.Msg { return showModelPickerMsg{} }
		},
	})

	r.Register(Command{
		Name:    "thinking",
		Desc:    "Set the reasoning level (opens a picker; /thinking <level> sets directly)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			if len(args) == 0 {
				return func() tea.Msg { return showThinkingPickerMsg{} }
			}
			return func() tea.Msg { return setThinkingMsg{Level: args[0]} }
		},
	})

	r.Register(Command{
		Name:    "openrouter-speed",
		Desc:    "Set the OpenRouter provider routing preference (opens a picker; /openrouter-speed <option> sets directly)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			if len(args) == 0 {
				return func() tea.Msg { return showOpenRouterSpeedPickerMsg{} }
			}
			return func() tea.Msg { return setOpenRouterSpeedMsg{ID: args[0]} }
		},
	})

	r.Register(Command{
		Name:    "login",
		Desc:    "Choose a provider, including local model setup (/login auth to authenticate)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			if len(args) > 0 && args[0] == "auth" {
				return func() tea.Msg { return loginMsg{} }
			}
			return func() tea.Msg { return showLoginProviderPickerMsg{} }
		},
	})

	r.Register(Command{
		Name:    "status",
		Desc:    "Override the status line (/status my text) or clear it (/status)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			text := strings.Join(args, " ")
			return func() tea.Msg {
				return StatusUpdateMsg{Key: "_override", Value: text}
			}
		},
	})

	r.Register(Command{
		Name:    "tools",
		Desc:    "Show tool calls from the current agent turn",
		Instant: true,
		Handler: func(_ []string) tea.Cmd {
			return func() tea.Msg { return showToolsMsg{} }
		},
	})

	r.Register(Command{
		Name:    "steer",
		Desc:    "Inject guidance into the running turn at the next step boundary (/steer <text>)",
		Instant: true,
		Handler: func(args []string) tea.Cmd {
			text := strings.Join(args, " ")
			if strings.TrimSpace(text) == "" {
				return func() tea.Msg {
					return NotifyMsg{Text: "usage: /steer <message> — guidance delivered mid-turn"}
				}
			}
			return func() tea.Msg { return steerSubmitMsg{Content: text} }
		},
	})

	r.Register(Command{
		Name:    "context",
		Desc:    "Show the context-window breakdown for the main agent",
		Instant: true,
		Handler: func(_ []string) tea.Cmd {
			return func() tea.Msg { return showContextMsg{} }
		},
	})

	// The history extension owns the implementation, but reserving its command
	// here keeps /history usable if a generated WASM artifact is stale or absent.
	r.Register(Command{
		Name:    "history",
		Desc:    "Browse previous conversations and resume from any point",
		Instant: true,
		Handler: func(_ []string) tea.Cmd {
			return func() tea.Msg { return dispatchOnCommandMsg{Name: "history"} }
		},
	})
}

// CommandLister exposes the command registry through the renderer-neutral
// sdk.CommandLister contract, so the HTTP front end can serve the list without
// reaching into the harness registry type.
func (m *Model) CommandLister() sdk.CommandLister {
	return sdk.CommandListerFunc(func() []sdk.CommandInfo {
		if m.commands == nil {
			return nil
		}
		return m.commands.ListInfo()
	})
}

// RegisterCommand records an extension-registered slash command in the same
// registry the built-ins live in, so a command registered after serve-mode
// wiring still appears in the browser's autocomplete and help. The handler
// routes through dispatchOnCommandMsg, exactly as the terminal sink does, so an
// extension command behaves identically whichever front end registered it.
func (m *Model) RegisterCommand(name, desc string, instant bool) {
	if m.commands == nil {
		return
	}
	m.commands.Register(Command{
		Name:    name,
		Desc:    desc,
		Instant: instant,
		Handler: func(args []string) tea.Cmd {
			return func() tea.Msg {
				return dispatchOnCommandMsg{Name: name, Args: args}
			}
		},
	})
}

// Internal message types used by built-in command handlers.
