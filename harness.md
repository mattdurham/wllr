I want to create a folder in bob named bob, this will be a simple coding harness like pi.dev or crush. I want to use charmbracelet v2 for the core. I want extensions to be written in wasm compiled language using wazero with a /reload, and I want it to work with multuple providers.

in ~/source/pi-mono is pi.dev sourcecode for inspiration. Ideally I want a basic TUI, provider hooks and lifecycle hooks to be written in go code, and most other functionality exported via extensions. The SDK/API should have similiar scope to pi.dev. 

Goals
* Simple
* Extensible
* Core functionality that is very limited
* Core in go and charmbracelet
* Extensible via wasm/wazero
* Good Docs on the API
* Core functionality should be UI, handling the lifecycle and handling extensions. Ideally the most basic is you run the app and you get a system that can communicate with a provider but honestly not even be able to read and write files.

## Addendums

* Extensions should be able to load/intercept at several levels. Process level, agent level and tool call level.

* Process Level
  * An extension to load skills
  * Add keyboard shortcuts
  * Adjust theme
  * Add / commands
  * Add providers
* Agent Level
  * Inject prompt
* Tool calls
  * Show or hide bash scripts
  * Add new tool calls
  * Intercept before and after, think of something like PII detection or raw keys or something or allow the ability to read/write files with allow list or permissions.

## Interactions

* Store history in ~/.bob/history.jsonl which is parsed by the go code and allow resume, by default show first the most recent ones that match the current directory via --resume cli flag

## Built in commands

* /reload - Reload all extensions and skills
* /quit /exit - Leave the program, also ctrl+c should work
* /model - show all the models we have API keys for

## Built in tools that are just extensions shipped

Extensions need a json file that describes the keyword, short description and at what level to load. These are just tools/hooks that the agents can call.

* read - tinygo code to read a file and return the contents
* write - tinygo code to write a file
* bash - tinygo code run a bash script

# Config
Exists in ~/.bob, extensions get ~/.bob/extensions/<name> where they can read/write whatever they want. This should be available through bespoke read/writes that sandbox with the new go fs that doesnt allow .. traversal. Soemthing like ReadConfig(NAME) []byte and WriteConfig(NAME,[]byte)

# More extensions but not built in, registered via make all

## skills

I want to be able to read claude code compatible skills and make them available via /skill this should entirely be an extension, ala registering /skills and then loading them, and offering it as a tool to llms. So /skills lists and /skill:bob:work should tell it explicitly to load it. This may mean we inject knowledge of skills into the agent so this might be an extension that is at an agent level injection.

## subagents

I want subagents that have there own lifecycle and that should not be fire and forget. Instead there is a leader that creates the team and each team member has a mailbox that is a queue of messages. Messages are sent and then added as followup so that when the llm is done with a tool call it will see the incoming message. Teams can have teams. Team leads can shutdown a team or team member.


These will be long running and have task lists so all that needs to stay resident inside the extension. This also means this extension can get instantiated multiple times.

## Ordering

We likely want to think about how extensions are loaded, built ins happen first (and can be override). Then we probably want to add some sort of rough ordering. For instance think about how an AGENTS.md gets loaded on the main subagent, or how multiple get loaded? Ideally even though the subagent extension is loaded first we dont want to instantiate it before a conceptual which is probably likely skills and loading agents.md is a PER PROCESS and then can register items to interject into new agents. So we likely need some new tools to load context.
