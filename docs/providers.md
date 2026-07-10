# Providers — LLM Backend Interface

Bob communicates with language model backends through the `Provider` interface
defined in `bob/provider`. Swapping providers or models requires no changes to
the harness or extension code.

---

## Provider Interface

```go
// bob/provider/provider.go

type Provider interface {
    Name() string
    Models() []string
    Stream(ctx context.Context, req Request, fn StreamCallback) error
}
```

### `Name() string`

Returns a short, human-readable identifier for the provider (e.g. `"anthropic"`
or `"mock"`). The name is displayed in the status bar.

### `Models() []string`

Returns the ordered list of model names the provider supports. The first element
is used as the default model when no explicit model is configured. The list is
also used to populate auto-completion (if any) and validation warnings.

### `Stream(ctx context.Context, req Request, fn StreamCallback) error`

Sends `req` to the LLM backend and delivers the streamed response one token at
a time by calling `fn` for each token.

- Blocks until the stream is fully consumed or an error occurs.
- Context cancellation (e.g. Ctrl+C in the TUI) must abort the stream promptly.
- If `fn` returns a non-nil error, `Stream` must stop and return that error.
- Returns `nil` on success, or a wrapped error describing what went wrong.

---

## Request Struct

```go
type Request struct {
    Model        string        // Active model identifier.
    SystemPrompt string        // Optional system prompt.
    Messages     []sdk.Message // Full conversation history.
    Tools        []sdk.Tool    // Tools available for this request.
    MaxTokens    int           // 0 means use provider default (4096 for Anthropic).
}
```

| Field          | Type           | Description                                                   |
|----------------|----------------|---------------------------------------------------------------|
| `Model`        | string         | Model name, e.g. `"claude-sonnet-4-5"`.                      |
| `SystemPrompt` | string         | Prepended to the conversation as a system-level instruction.  |
| `Messages`     | []sdk.Message  | Ordered conversation history (user and assistant turns).      |
| `Tools`        | []sdk.Tool     | Tool definitions registered by extensions.                    |
| `MaxTokens`    | int            | Maximum tokens in the response. Provider default if `0`.      |

`sdk.Message` fields:

| Field     | Type        | Description                        |
|-----------|-------------|------------------------------------|
| `Role`    | sdk.Role    | `"user"` or `"assistant"`.         |
| `Content` | string      | The message text.                  |

---

## StreamCallback Contract

```go
type StreamCallback func(token string) error
```

- Called once per streamed token (may be a single character or a short string,
  depending on the backend).
- **Returning a non-nil error aborts the stream.** `Stream` propagates the error
  as its return value.
- The callback is called synchronously within `Stream`; it is safe to update
  shared state only if that state is otherwise protected.
- Context cancellation is checked between tokens inside `Stream` independently
  of the callback; callers do not need to check `ctx.Done()` inside the callback
  (though doing so is harmless).

---

## Built-in Providers

### Anthropic (`bob/provider/anthropic`)

Backed by the official [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)
client. Uses the Anthropic Messages API with streaming.

**Authentication:** set the `ANTHROPIC_API_KEY` environment variable. Bob
refuses to start with `BOB_PROVIDER=anthropic` and an empty key.

**Supported models (v1):**

| Model name              | Notes            |
|-------------------------|------------------|
| `claude-opus-4-5`       | Highest capability |
| `claude-sonnet-4-5`     | Default           |
| `claude-haiku-3-5`      | Fastest           |

The default `MaxTokens` when `Request.MaxTokens` is `0` is `4096`.

**Tools:** The Anthropic provider converts `sdk.Tool` values to
`anthropic.ToolParam`. The `input_schema` field must be a valid JSON Schema
`"object"` type with a `properties` map.

**Instantiation:**

```go
import "github.com/mattdurham/bob/bob/provider/anthropic"

p := anthropic.New(os.Getenv("ANTHROPIC_API_KEY"))
```

For tests, use `anthropic.NewWithTransport(apiKey, transport)` to inject a
custom `http.RoundTripper`.

---

## Runtime Selection

Bob reads provider and model from environment variables at startup.

| Variable              | Default              | Description                         |
|-----------------------|----------------------|-------------------------------------|
| `WLLR_PROVIDER`       | `anthropic`          | Provider name (`anthropic`, `openai`, `gemini`, `local`). |
| `WLLR_MODEL`          | `claude-sonnet-4-6`  | Default model. See precedence below. |
| `ANTHROPIC_API_KEY`   | (none)               | Required when `WLLR_PROVIDER=anthropic`. |
| `WLLR_EXTENSIONS_DIR` | (none)               | Directory scanned for `.wasm` files.|

### Selecting a model at runtime

Run `/model` with no argument to open a **picker** listing the current
provider's available models (the active one is marked). Selecting a model:

- rebuilds the main agent's language model,
- updates the context window,
- **persists** the choice to `~/.config/wllr/config.json` (`wllr.model`), so it
  is the default next launch.

```
/model                 # open the picker
/model claude-opus-4-8 # set directly by ID
```

**Model precedence at startup:** `WLLR_MODEL` (env) > persisted selection
(`config.json`) > built-in default (`claude-sonnet-4-6`).

### Selecting a thinking level

Run `/thinking` (no argument) to open a picker of reasoning levels, or
`/thinking <level>` to set one directly:

```
/thinking          # open the picker
/thinking high     # set directly
```

Levels are provider-agnostic and map to each provider's native mechanism:

| Level | Anthropic (budget tokens) | OpenAI (`reasoning_effort`) | Gemini (budget tokens) |
|---------|---------|---------|---------|
| `off`     | disabled | none | disabled |
| `minimal` | 2048 | minimal | 512 |
| `low`     | 4096 | low | 4096 |
| `medium`  | 16384 | medium | 16384 |
| `high`    | 32768 | high | 32768 |
| `xhigh`   | 65536 | xhigh | 65536 |

The selection applies to the running main agent immediately and is **persisted**
to `config.json` (`wllr.thinking`), so it survives restarts. Startup precedence:
persisted level, else `off`.

The model list is a curated catalog (`cmd/modelcatalog.go`) covering Anthropic,
OpenAI, and Gemini, sourced from charmbracelet's Catwalk model-metadata service.
For `provider: "local"`, wllr uses the configured `local_models` entries. Each
entry supplies the OpenAI-compatible endpoint for that specific model, so `/models`
can switch between local instances.

### Local provider config

Configure local models in `~/.config/wllr/config.json` under the `wllr` group:

```json
{
  "wllr": {
    "provider": "local",
    "model": "qwen/qwen3-coder-next",
    "local_models": [
      {
        "id": "qwen/qwen3-coder-next",
        "name": "Qwen3 Coder Next",
        "base_url": "http://localhost:1234/v1",
        "context_window": 262144
      },
      {
        "id": "deepseek-v4-flash",
        "name": "Dwarfstar 4 Flash",
        "base_url": "http://localhost:8000/v1",
        "context_window": 300000
      }
    ]
  }
}
```

`api_key` is optional per local model and omitted for endpoints that do not
require authorization. `WLLR_MODEL` can still override the selected model, but
the model ID must match a `local_models` entry so wllr knows which endpoint to
use.

### Provider authentication

On a blank first run with no configured provider/model and no credentials, wllr
opens a setup wizard in the TUI. The wizard lets you choose:

- **ChatGPT** — starts the OpenAI/Codex device-code OAuth flow.
- **Anthropic** — starts the Claude OAuth flow.
- **Local model** — uses an OpenAI-compatible local endpoint and skips auth.

The wizard persists both `wllr.provider` and a provider-appropriate default
`wllr.model` in `~/.config/wllr/config.json`.

You can also authenticate before starting the TUI:

```sh
wllr login --provider anthropic
wllr login --provider openai
```

Or set the provider's API key environment variable directly. Once wllr can
start with an explicitly configured cloud provider that has no recorded auth
choice, it asks once how you want to authenticate that provider:

- **Set up OAuth / login** — sign in via the provider
- **Use an API key** — from the environment or auth file

The choice is recorded in a dedicated `0600` auth file
(`~/.config/wllr/auth.json`), keyed by provider:

```json
{ "anthropic": { "type": "oauth" }, "openai": { "type": "api_key" } }
```

The **presence of an entry is the record** — the prompt is shown at most once
per provider and never again once a choice exists.

### OAuth login (Anthropic / Claude Pro·Max)

Choosing **OAuth** (or running `/login` any time) starts an interactive sign-in:

1. wllr shows an authorize URL in a modal (also copied to your clipboard). Open
   it in a browser **on the same machine as wllr**.
2. Approve access. You're logged in **automatically** once the browser redirects
   back — wllr runs a local callback server on `127.0.0.1:53692` that captures
   the code, and the modal closes on its own. No copy/paste.

> **Running over SSH?** The browser redirect goes to *your* `localhost`, which
> can't reach wllr on the remote host, so login won't complete. Forward the port
> first — e.g. `ssh -L 53692:localhost:53692 …` — so the redirect reaches the
> remote callback server. (There is no manual paste-back fallback.)

wllr exchanges the code for an access + refresh token
(authorization-code + PKCE, matching the Claude Code client), stores them in
`auth.json`, and switches
the running provider to the new token — no restart. On the next launch the
stored token is applied automatically and **refreshed if expired**. The access
token is an `sk-ant-oat…` subscription token; wllr sends the Claude-Code beta
headers for it automatically.

```json
{ "anthropic": { "type": "oauth", "access": "sk-ant-oat…", "refresh": "…", "expires": 1750000000000 } }
```

You can still authenticate by setting `ANTHROPIC_API_KEY` directly (a normal
API key or a pre-obtained `sk-ant-oat…` token).

### OAuth login (OpenAI Codex / ChatGPT Plus·Pro)

With `WLLR_PROVIDER=openai`, choosing **OAuth** (or `/login`) uses the
**device-code** flow — which works anywhere, including over SSH, with no local
server or port forwarding:

1. wllr shows a verification URL (copied to your clipboard) **and a short user
   code**.
2. Open the URL in a browser on any machine, enter the code, approve.
3. wllr is polling in the background; login completes automatically once you
   approve, and the box closes on its own.

The access token is a ChatGPT subscription token. wllr routes Codex requests
through the ChatGPT backend (`chatgpt.com/backend-api/codex`) with the
`chatgpt-account-id` header (extracted from the token's JWT claims), matching how
Codex authenticates. Tokens are stored in `auth.json` and refreshed on expiry.

```json
{ "openai": { "type": "oauth", "access": "…", "refresh": "…", "expires": 1750000000000, "account_id": "…" } }
```

You can still use a plain `OPENAI_API_KEY` instead. Gemini remains API-key only;
OAuth login is available for Anthropic and OpenAI (Codex).

---

## Custom Provider

Implement the `Provider` interface and pass the value to `harness.New`:

```go
import (
    "github.com/mattdurham/bob/bob/harness"
    "github.com/mattdurham/bob/bob/extension"
)

type MyProvider struct{}

func (p *MyProvider) Name() string          { return "myprovider" }
func (p *MyProvider) Models() []string      { return []string{"my-model-v1"} }
func (p *MyProvider) Stream(ctx context.Context, req provider.Request, fn provider.StreamCallback) error {
    // Iterate tokens and call fn(token) for each.
    return nil
}

// Wire it up:
host := extension.NewHost(nil)
model := harness.New(&MyProvider{}, host)
```

No registration step is needed. The harness uses whatever provider is passed to
`harness.New(p, h)`.

---

## Mock Provider (`bob/provider/mock`)

`bob/provider/mock` contains a scripted provider for use in tests. It emits a
configured list of tokens and optionally returns a terminal error.

```go
import "github.com/mattdurham/bob/bob/provider/mock"

p := &mock.Provider{
    Tokens: []string{"Hello", ", ", "world", "!"},
    Err:    nil, // returned after last token
}
```

**Fields:**

| Field            | Type              | Description                                                  |
|------------------|-------------------|--------------------------------------------------------------|
| `Tokens`         | `[]string`        | Tokens emitted in order.                                     |
| `Err`            | `error`           | Returned from `Stream` after all tokens are emitted.         |
| `StreamErr`      | `error`           | Returned mid-stream (after `StreamErrAfter` tokens).         |
| `StreamErrAfter` | `int`             | Token index at which `StreamErr` is returned.                |
| `CallCount`      | `int`             | Incremented on each `Stream` call. Useful for assertions.    |
| `LastRequest`    | `provider.Request`| The most recent request; inspect in assertions.              |

Context cancellation is honoured between tokens. The mock provider respects
`ctx.Done()` in the same way as the real providers.
