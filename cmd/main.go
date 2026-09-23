package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	fantasy "charm.land/fantasy"
	yaml "gopkg.in/yaml.v3"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/harness"
	"github.com/mattdurham/wllr/modules/mcp"
	"github.com/mattdurham/wllr/modules/sdk"
)

// builtinFS embeds generated built-in WASM modules when they are present.
// `make build` runs `make extensions` first, so release binaries include them.
// Clean checkouts still compile because cmd/builtins contains a tracked README.
// read_file, write_file, exec, and get_env are native Go tools registered via
// RegisterNativeTool; no WASM is needed for stateless I/O.
//
//go:embed builtins/*
var builtinFS embed.FS

func main() { //nolint:gocyclo // main wires CLI, providers, extensions, and TUI callbacks in one startup path.
	if len(os.Args) > 1 && os.Args[1] == "login" {
		os.Exit(runLoginCommand(context.Background(), os.Args[2:], os.Stdout, os.Stderr))
	}

	execPrompt := flag.String("exec", "", "run a single prompt non-interactively and print the response to stdout")
	pprofAddr := flag.String(
		"pprof",
		defaultPprofAddr(),
		"listen address for net/http/pprof debug server (use empty string to disable)",
	)
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
		os.Exit(1)
	}

	ctx := context.Background()
	localModelReplaced := resolveLocalProviderConfig(ctx, cfg)

	missingAuthEnv, missingAuth := missingProviderAuth(cfg)
	_, knownOpenRouterModel := cfg.openRouterModel(cfg.Model)
	needsOpenRouterModel := cfg.Provider == providerOpenRouter && !knownOpenRouterModel
	if missingAuth && *execPrompt != "" {
		fmt.Fprintf(os.Stderr, "wllr: %v\n", missingAuthError(cfg.Provider, missingAuthEnv))
		os.Exit(1)
	}
	if needsOpenRouterModel && *execPrompt != "" {
		fmt.Fprintln(os.Stderr, "wllr: choose an OpenRouter model in the interactive setup before using --exec")
		os.Exit(1)
	}

	var fantasyProv fantasy.Provider
	var langModel fantasy.LanguageModel
	if !missingAuth && !needsOpenRouterModel {
		fantasyProv, langModel, err = buildProvider(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wllr: %v\n", err)
			os.Exit(1)
		}
	}

	// Create agent pool and spawn the main agent.
	pool := agent.NewPool()
	if fantasyProv != nil {
		pool.SetProvider(fantasyProv)
	}
	pool.SetModelFactory(
		func(modelCtx context.Context, currentProvider fantasy.Provider, model, endpoint string) (fantasy.LanguageModel, error) {
			lm, modelErr := subagentLanguageModel(modelCtx, cfg, currentProvider, model, endpoint)
			if modelErr == nil && cfg.Provider == providerLocal {
				if entry, ok := cfg.localModelByID(model); ok && entry.ContextWindow > 0 {
					pool.SetModelContextWindow(model, entry.ContextWindow)
				}
			}
			return lm, modelErr
		},
	)
	pool.SetProviderName(cfg.Provider)
	pool.SetDefaultModelName(cfg.Model)
	// Sub-agents without an explicit model default to the configured working
	// tier ("low"), which may live on a different provider than the session
	// model. An explicit model in create_agent still wins (the pool resolves
	// that before consulting this resolver), and an unconfigured low tier falls
	// back to the session model.
	pool.SetSubagentResolver(
		func(resolveCtx context.Context, _ string) (fantasy.LanguageModel, string, error) {
			resolveProvider, modelID, _, tierErr := resolveModelTier(tierLow, cfg.Provider, cfg)
			if tierErr != nil {
				return sessionModel(resolveCtx, pool)
			}
			// A tier model must have a resolved window or it cannot stream, and
			// the window is what sub-agent compaction sizes against. Local
			// models may only advertise it over the endpoint, so remember the
			// model (as selecting it from /models would) and resolve its own
			// window through the same path the interactive switch uses.
			cw := contextWindowForSelection(resolveProvider, modelID, cfg)
			if resolveProvider == providerLocal {
				applyLocalModelChoice(resolveCtx, cfg, modelID)
				cw = resolveLocalModelWindow(resolveCtx, cfg, modelID)
			}
			if cw <= 0 {
				return sessionModel(resolveCtx, pool)
			}
			lm, err := modelForProviderTier(resolveCtx, cfg, resolveProvider, modelID)
			if err != nil {
				// A misconfigured working tier must not break every sub-agent
				// spawn; fall back to the session model so delegation still works.
				return sessionModel(resolveCtx, pool)
			}
			pool.SetModelContextWindow(modelID, cw)
			return lm, modelID, nil
		},
	)
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = contextWindowForSelection(cfg.Provider, cfg.Model, cfg)
	}
	if cfg.ContextWindow <= 0 && *execPrompt != "" {
		fmt.Fprintf(
			os.Stderr,
			"wllr: context window for model %q is unknown; set WLLR_CONTEXT_WINDOW or configure it in interactive mode\n",
			cfg.Model,
		)
		os.Exit(1)
	}
	if cfg.ContextWindow > 0 {
		pool.SetModelContextWindow(cfg.Model, cfg.ContextWindow)
	}
	for _, model := range cfg.OpenRouterModels {
		if model.ContextWindow > 0 {
			pool.SetModelContextWindow(model.ID, model.ContextWindow)
		}
	}

	if _, spawnErr := pool.Spawn(agent.MainAgentID, langModel, agent.SpawnOpts{ModelName: cfg.Model, ContextWindow: cfg.ContextWindow, TurnTimeout: -1}); spawnErr != nil {
		fmt.Fprintf(os.Stderr, "wllr: spawn main agent: %v\n", spawnErr)
		os.Exit(1)
	}

	// Build extension host — extension logs flow through slog with "extension" attribute.
	h := extension.NewHost(nil)

	// Configure logging now that the host exists. stderr (exec/headless only) stays
	// in core; the rolling log FILE is written by the bundled `logging` WASM
	// extension, fed by the dispatchLogHandler via EventLog. See cmd/loghandler.go.
	cleanupLog := setupLogging(h, *execPrompt == "")
	// Usage metrics are served alongside pprof on the debug listener (at
	// /metrics), so a local Prometheus scrape needs no second address.
	metrics := newWllrMetrics()
	pool.SetUsageObserver(metrics.recordTurn)
	pool.SetLifecycleObserver(metrics.recordLifecycle)
	cleanupPprof := startDebugServer(*pprofAddr, metrics)
	defer cleanupPprof()
	// Route the host's own diagnostic logs through the configured default handler
	// too (the dispatch handler's reentrancy guard makes this safe).
	h.SetLogger(slog.Default())

	// Wire OS capabilities via the CapabilityProvider interface.
	h.SetCapabilities(newOSCapabilityProvider(pool))

	// Create the harness model BEFORE loading extensions so that
	// OnRegisterCommand (wired in harness.New) is set when _init and
	// session_start handlers call register_command.
	m := harness.New(pool, agent.MainAgentID, h)
	if cfg.ContextWindow <= 0 && *execPrompt == "" && !needsOpenRouterModel {
		m.SetPendingContextWindow(cfg.Provider, cfg.Model)
	}

	// Register stateless tools as native Go functions — bypasses WASM entirely.
	registerNativeTools(h)
	registerAgentStatusTool(h, pool)

	loadBuiltinExtensions(ctx, h)

	closeMCP := startMCPBridge(ctx, h)
	defer closeMCP()

	// Load extensions from ~/.wllr/extensions/ and WLLR_EXTENSIONS_DIR.
	var extPaths []string
	extPaths = append(extPaths, loadExtensionsFromSubdirs(ctx, h, wllrExtensionsDir())...)
	if cfg.ExtensionsDir != "" && cfg.ExtensionsDir != wllrExtensionsDir() {
		extPaths = append(extPaths, loadExtensionsFlat(ctx, h, cfg.ExtensionsDir)...)
	}

	defer func() {
		cleanupLog()
		if err := h.Close(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "wllr: close extension host: %v\n", err)
		}
	}()

	// Log registered tools so startup issues are visible in the log.
	{
		registered := h.RegisteredTools()
		names := make([]string, 0, len(registered))
		for _, t := range registered {
			names = append(names, t.Tool.Name)
		}
		slog.Info("wllr: extensions ready", "tools", names)
	}

	m.SetExtensionPaths(extPaths)

	// Wire the /model picker: list the active provider's models, and switch +
	// persist on selection. The switch rebuilds the main agent's language model,
	// updates the context window, and saves the choice for next launch.
	currentProvider := cfg.Provider
	oauthState := newOAuthLoginState(ctx, pool, cfg.Model)
	m.ProviderListFn = func() []harness.ProviderChoice {
		return []harness.ProviderChoice{
			{ID: providerOpenAI, Name: "ChatGPT", Sublabel: "sign in with a ChatGPT account"},
			{ID: providerAnthropic, Name: "Anthropic", Sublabel: "sign in with a Claude account"},
			{ID: providerOpenRouter, Name: "OpenRouter", Sublabel: "use an API key and choose models"},
			{ID: providerLocal, Name: "Local model", Sublabel: localProviderSublabel(cfg)},
		}
	}
	m.SelectProviderFn = func(provider string) (string, bool, error) {
		modelID := defaultModelForProvider(provider)
		if provider == providerLocal {
			models := localModels(ctx, cfg)
			if len(models) == 0 {
				return "", false, harness.ErrLocalModelSetupNeeded
			}
			modelID = firstAvailableModel(cfg.Model, models)
		}
		if modelID == "" {
			return "", false, fmt.Errorf("unknown provider %q", provider)
		}
		currentProvider = provider
		cfg.Provider = provider
		cfg.Model = modelID
		pool.SetProviderName(provider)
		pool.SetDefaultModelName(modelID)
		// Apply the resolved window unconditionally (0 included): a window-less
		// model must not keep the previous model's window in the pool, which
		// would drive the compaction threshold and the ctx display wrongly.
		// An explicit user override is always preferred and is never cleared.
		if cw := contextWindowForSelection(provider, modelID, cfg); cw > 0 {
			pool.SetModelContextWindow(modelID, cw)
		}
		if saveErr := saveProvider(provider); saveErr != nil {
			slog.Warn("wllr: could not persist provider selection", "provider", provider, "error", saveErr)
		}
		if saveErr := saveModel(modelID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", modelID, "error", saveErr)
		}
		oauthState.model = modelID
		// Re-apply the thinking selection for the new provider: the persisted
		// mode when valid, the local endpoint's declared default when local, or
		// a clear when the stored mode is invalid for the new provider (no stale
		// agent options or status). Runs before the switch because every case
		// below returns.
		m.SetThinkingForModel(startupThinkingMode(ctx, cfg, provider))
		m.SetSpeedDisplay(speedDisplayFor(provider))
		switch provider {
		case providerOpenAI, providerAnthropic:
			return modelID, true, nil
		case providerLocal:
			prov, lm, err := buildProvider(ctx, cfg)
			if err != nil {
				return "", false, err
			}
			pool.SetProvider(prov)
			if main := pool.Get(agent.MainAgentID); main != nil {
				main.SetModel(lm, modelID, contextWindowForSelection(provider, modelID, cfg))
			}
			return modelID, false, nil
		default:
			return "", false, fmt.Errorf("unknown provider %q", provider)
		}
	}
	// The /models list shows the models the user actually has: local endpoint
	// entries, pinned OpenRouter models, and saved catalog models for
	// anthropic/openai/gemini. It spans providers so any model can be tagged or
	// selected; the add-model flow (`a`) is how new ones are introduced.
	m.ModelListFn = func() []harness.ModelChoice {
		return savedModelChoices(ctx, cfg, currentProvider, cfg.Model)
	}
	m.SelectModelFn = func(modelID string) error {
		if currentProvider == providerOpenRouter {
			if _, ok := cfg.openRouterModel(modelID); !ok {
				return fmt.Errorf(
					"OpenRouter model %q is not in your saved models; use Browse OpenRouter models in /models",
					modelID,
				)
			}
		}
		contextWindow := contextWindowForSelection(currentProvider, modelID, cfg)
		if contextWindow <= 0 {
			return fmt.Errorf("%w: %s", harness.ErrContextWindowRequired, modelID)
		}
		var lm fantasy.LanguageModel
		if currentProvider == providerLocal {
			if !applyLocalModelChoice(ctx, cfg, modelID) {
				return fmt.Errorf("local model %q is not configured in wllr.local_models", modelID)
			}
			prov, newLM, err := buildProvider(ctx, cfg)
			if err != nil {
				return err
			}
			pool.SetProvider(prov)
			lm = newLM
		} else {
			var lmErr error
			lm, lmErr = pool.LanguageModelForModel(ctx, modelID)
			if lmErr != nil {
				return lmErr
			}
		}
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetModel(lm, modelID, contextWindow)
		}
		cfg.Model = modelID
		pool.SetDefaultModelName(modelID)
		pool.SetModelContextWindow(modelID, contextWindow)
		if saveErr := saveModel(modelID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", modelID, "error", saveErr)
		}
		// Re-apply the thinking selection for the new model: the persisted mode
		// when valid for it, the local endpoint's declared default when local,
		// or a clear when the stored mode is invalid for the new model.
		m.SetThinkingForModel(startupThinkingMode(ctx, cfg, currentProvider))
		return nil
	}
	// Add-model flow: `a` in /models lists providers, each routing to the wizard
	// that can configure it. Catalog providers list their built-in models and
	// save the choice; OpenRouter reuses the key + live-catalog browse; local
	// reuses the endpoint wizard.
	m.AddModelProviderListFn = func() []harness.ProviderChoice { return addModelProviders(cfg) }
	m.ProviderReadyFn = func(provider string) bool { return providerReady(cfg, provider) }
	m.CatalogModelsFn = func(provider string) []harness.ModelChoice { return catalogChoicesFor(cfg, provider) }
	m.SaveCatalogModelFn = func(provider string, choice harness.ModelChoice) (string, error) {
		return saveCatalogModel(ctx, cfg, pool, provider, choice)
	}
	m.RemoveSavedModelFn = func(provider, modelID string) error {
		if err := removeModel(provider, modelID); err != nil {
			return err
		}
		// Keep the in-memory config in step so the list refreshes immediately
		// without a reload.
		dropModelFromConfig(cfg, provider, modelID)
		return nil
	}
	// Cross-provider selection: the /models list spans providers, so choosing a
	// model owned by another provider rebuilds that provider and applies it.
	m.SelectProviderModelFn = func(provider, modelID string) error {
		if provider == "" {
			return fmt.Errorf("model provider is missing")
		}
		contextWindow := contextWindowForSelection(provider, modelID, cfg)
		if contextWindow <= 0 {
			return fmt.Errorf("%w: %s", harness.ErrContextWindowRequired, modelID)
		}
		if err := activateProviderModel(ctx, cfg, pool, provider, modelID); err != nil {
			return err
		}
		currentProvider = provider
		cfg.Provider = provider
		cfg.Model = modelID
		if saveErr := saveProvider(provider); saveErr != nil {
			slog.Warn("wllr: could not persist provider selection", "provider", provider, "error", saveErr)
		}
		if saveErr := saveModel(modelID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", modelID, "error", saveErr)
		}
		m.SetThinkingForModel(startupThinkingMode(ctx, cfg, provider))
		m.SetSpeedDisplay(speedDisplayFor(provider))
		return nil
	}

	// Model tiers: tag models as high/low in /models, then apply a tier by name
	// (/model high) or via a skill's frontmatter. A tier may name a different
	// provider, so applying one switches providers when needed.
	m.TierNamesFn = tierNames
	m.ModelTierLabelsFn = tierLabels
	m.TagModelTierFn = func(tier, provider, modelID string) error {
		if modelID == "" {
			return fmt.Errorf("no model selected")
		}
		if provider == "" {
			provider = currentProvider
		}
		return setModelTier(tier, provider, modelID)
	}
	m.ClearModelTierFn = clearModelTier
	m.ApplyModelTierFn = func(tier string) (string, string, error) {
		provider, modelID, thinking, err := resolveModelTier(tier, currentProvider, cfg)
		if err != nil {
			return "", "", err
		}
		if err := validateTierThinking(provider, thinking); err != nil {
			return "", "", fmt.Errorf("tier %s: %w", tier, err)
		}
		if provider == providerLocal {
			// The tier may name a model the endpoint advertised rather than one
			// declared in local_models (the picker lists both). Remember it for
			// the session, exactly as selecting it from /models would, so the
			// switch below can build a provider for it.
			if !applyLocalModelChoice(ctx, cfg, modelID) {
				return "", "", fmt.Errorf(
					"tier %s: local model %q is not available from any configured endpoint",
					tier, modelID,
				)
			}
		}
		// A model with no resolved window cannot stream (the pool treats a zero
		// window as incomplete metadata). Resolve the target model's own window
		// first — for local models that may require endpoint discovery — and
		// fail with an actionable message only when it stays unknown.
		if provider == providerLocal {
			if resolveLocalModelWindow(ctx, cfg, modelID) <= 0 {
				return "", "", fmt.Errorf(
					"tier %s: context window for %s is unknown; set it in wllr.local_models or select it once in /models",
					tier,
					modelID,
				)
			}
		} else if contextWindowForSelection(provider, modelID, cfg) <= 0 {
			return "", "", fmt.Errorf(
				"tier %s: context window for %s is unknown; select it once in /models to set a window, then tag it",
				tier, modelID,
			)
		}
		if provider != currentProvider {
			if err := activateProviderModel(ctx, cfg, pool, provider, modelID); err != nil {
				return "", "", err
			}
			currentProvider = provider
			cfg.Provider = provider
			if saveErr := saveProvider(provider); saveErr != nil {
				slog.Warn("wllr: could not persist provider selection", "provider", provider, "error", saveErr)
			}
		} else if provider == providerLocal {
			if err := activateProviderModel(ctx, cfg, pool, provider, modelID); err != nil {
				return "", "", err
			}
		} else {
			lm, lmErr := pool.LanguageModelForModel(ctx, modelID)
			if lmErr != nil {
				return "", "", lmErr
			}
			if main := pool.Get(agent.MainAgentID); main != nil {
				main.SetModel(lm, modelID, contextWindowForSelection(provider, modelID, cfg))
			}
		}
		cfg.Model = modelID
		pool.SetDefaultModelName(modelID)
		if saveErr := saveModel(modelID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", modelID, "error", saveErr)
		}
		// A tier may pin a thinking level; otherwise keep the provider's resolved
		// default. The mode ID for the target provider/model comes from the saved
		// thinking level, so a tier without one leaves the current selection.
		if thinking != "" {
			if modeID := thinkingModeIDForLevel(provider, modelID, thinking); modeID != "" {
				m.SetThinkingForModel(modeID)
			}
		} else {
			m.SetThinkingForModel(startupThinkingMode(ctx, cfg, provider))
			m.SetSpeedDisplay(speedDisplayFor(provider))
		}
		return provider, modelID, nil
	}
	// OpenRouter provider routing (/openrouter-speed): floor, nitro, or an
	// explicit sort. This is provider-level, separate from the model choice, so
	// it is only offered while OpenRouter is the active provider.
	m.SpeedListFn = func() []harness.SpeedChoice {
		if currentProvider != providerOpenRouter {
			return nil
		}
		out := make([]harness.SpeedChoice, 0, len(openRouterSpeedOptions))
		for _, o := range openRouterSpeedOptions {
			out = append(out, harness.SpeedChoice{ID: o.ID, Label: o.Label, Description: o.Description})
		}
		return out
	}
	m.SpeedUnavailableReasonFn = func() string {
		if currentProvider != providerOpenRouter {
			return "only OpenRouter models route across multiple providers"
		}
		return ""
	}
	m.SelectSpeedFn = func(id string) error {
		if currentProvider != providerOpenRouter {
			return fmt.Errorf("provider routing is only supported for OpenRouter")
		}
		if !isValidOpenRouterSpeed(id) {
			return fmt.Errorf("unknown routing option %q", id)
		}
		if err := saveOpenRouterSpeed(id); err != nil {
			return err
		}
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetProviderOptions(providerOptionsForRuntime(currentProvider, savedThinkingMode()))
		}
		return nil
	}
	m.SetContextWindowFn = func(provider, modelID string, tokens int64) error {
		if err := saveContextWindow(provider, modelID, tokens); err != nil {
			return err
		}
		pool.SetModelContextWindow(modelID, tokens)
		return nil
	}
	// A skill may declare a thinking level (e.g. thinking: high). Resolve it to
	// the active provider's mode ID and apply it, so the level vocabulary is
	// consistent with /thinking and tier thinking overrides.
	m.SetThinkingLevelFn = func(level string) error {
		modeID := thinkingModeIDForLevel(currentProvider, cfg.Model, level)
		if modeID == "" {
			return fmt.Errorf("provider %s does not support thinking level %q", currentProvider, level)
		}
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetProviderOptions(providerOptionsForRuntime(currentProvider, modeID))
		}
		if err := saveThinkingMode(modeID); err != nil {
			slog.Warn("wllr: could not persist thinking mode", "mode", modeID, "error", err)
		}
		if err := saveThinkingLevel(thinkingLevel(level)); err != nil {
			slog.Warn("wllr: could not persist thinking level", "level", level, "error", err)
		}
		return nil
	}
	m.HasOpenRouterKeyFn = func() bool { return cfg.OpenRouterAPIKey != "" }
	m.SaveOpenRouterKeyFn = func(key string) error {
		if err := saveAuthCredential(providerOpenRouter, authCredential{Type: authTypeAPIKey, Key: key}); err != nil {
			return err
		}
		cfg.OpenRouterAPIKey = key
		return nil
	}
	m.FetchOpenRouterModelsFn = func() ([]harness.OpenRouterModelChoice, error) {
		models, fetchErr := fetchOpenRouterModels(
			context.Background(),
			nil,
			openRouterModelsURL,
			cfg.OpenRouterAPIKey,
		)
		if fetchErr != nil {
			return nil, fetchErr
		}
		out := make([]harness.OpenRouterModelChoice, 0, len(models))
		for _, model := range models {
			out = append(
				out,
				harness.OpenRouterModelChoice{ID: model.ID, Name: model.Name, ContextWindow: model.ContextWindow},
			)
		}
		return out, nil
	}
	m.AddOpenRouterModelFn = func(choice harness.OpenRouterModelChoice) error {
		model := openRouterModelConfig{ID: choice.ID, Name: choice.Name, ContextWindow: choice.ContextWindow}
		candidate := *cfg
		candidate.Provider, candidate.Model = providerOpenRouter, model.ID
		prov, lm, buildErr := buildProvider(ctx, &candidate)
		if buildErr != nil {
			return buildErr
		}
		if err := cfg.pinOpenRouterModel(model); err != nil {
			return err
		}
		if err := saveProvider(providerOpenRouter); err != nil {
			return err
		}
		if err := saveModel(model.ID); err != nil {
			return err
		}
		cfg.Provider, cfg.Model = providerOpenRouter, model.ID
		currentProvider = providerOpenRouter
		pool.SetProvider(prov)
		pool.SetProviderName(providerOpenRouter)
		pool.SetDefaultModelName(model.ID)
		window := contextWindowForSelection(providerOpenRouter, model.ID, cfg)
		pool.SetModelContextWindow(model.ID, window)
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetModel(lm, model.ID, window)
		}
		m.SetThinkingForModel(startupThinkingMode(ctx, cfg, providerOpenRouter))
		return nil
	}
	if localModelReplaced {
		m.SetPendingModelPicker()
	}

	// Wire the interactive local-model setup flow: /login (or the startup
	// provider picker) redirects here when the local provider has no usable
	// model, letting the user probe an endpoint or enter details manually.
	m.HasLocalModelFn = func() bool { return len(localModels(ctx, cfg)) > 0 }
	m.ProbeLocalModelsFn = func(baseURL string) ([]harness.LocalModelChoice, string, harness.LocalModelProbeStatus) {
		models, resolvedBase, result := probeLocalModelsEndpoint(ctx, baseURL, "")
		if result == queryLocalModelsUnreachable {
			return nil, "", harness.LocalModelProbeUnreachable
		}
		if result != queryLocalModelsOK {
			return nil, "", harness.LocalModelProbeEmpty
		}
		out := make([]harness.LocalModelChoice, 0, len(models))
		for _, remote := range models {
			id := strings.TrimSpace(remote.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(remote.Name)
			if name == "" {
				name = id
			}
			out = append(out, harness.LocalModelChoice{
				ID:            id,
				Name:          name,
				ContextWindow: contextWindowFromOpenAIModel(remote),
			})
		}
		if len(out) == 0 {
			return nil, "", harness.LocalModelProbeEmpty
		}
		return out, resolvedBase, harness.LocalModelProbeOK
	}
	m.SaveLocalModelFn = func(entry harness.LocalModelEntry) (string, error) {
		lm := localModelConfig{
			ID:            entry.ID,
			Name:          entry.Name,
			BaseURL:       entry.BaseURL,
			APIKey:        entry.APIKey,
			ContextWindow: entry.ContextWindow,
		}
		if _, ok := cfg.localModelByID(lm.ID); !ok {
			cfg.LocalModels = append(cfg.LocalModels, lm)
		} else {
			for i := range cfg.LocalModels {
				if cfg.LocalModels[i].ID == lm.ID {
					cfg.LocalModels[i] = lm
					break
				}
			}
		}
		if saveErr := saveLocalModels(cfg.LocalModels); saveErr != nil {
			return "", fmt.Errorf("save local models: %w", saveErr)
		}
		rememberLocalModel(cfg, modelInfo{
			ID:            lm.ID,
			Name:          lm.Name,
			LocalBaseURL:  lm.BaseURL,
			LocalAPIKey:   lm.APIKey,
			ContextWindow: lm.ContextWindow,
		})
		currentProvider = providerLocal
		cfg.Provider = providerLocal
		if saveErr := saveProvider(providerLocal); saveErr != nil {
			slog.Warn("wllr: could not persist provider selection", "provider", providerLocal, "error", saveErr)
		}
		if saveErr := saveModel(lm.ID); saveErr != nil {
			slog.Warn("wllr: could not persist model selection", "model", lm.ID, "error", saveErr)
		}
		prov, newLM, buildErr := buildProvider(ctx, cfg)
		if buildErr != nil {
			return "", buildErr
		}
		pool.SetProvider(prov)
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetModel(newLM, lm.ID, contextWindowForSelection(providerLocal, lm.ID, cfg))
		}
		// Setup just finished: adopt the model's thinking default (detected) so
		// the new local model starts with the server's declared setting.
		m.SetThinkingForModel(startupThinkingMode(ctx, cfg, providerLocal))
		return lm.ID, nil
	}

	// Wire the /thinking picker: list the active model's reasoning modes and
	// apply + persist on selection. For local models the mode set is detected
	// (config > LM Studio app API > standard OpenAI fallback); an empty set is
	// expected for models the endpoint says cannot reason.
	m.ThinkingListFn = func() []harness.ThinkingChoice {
		var modes []thinkingMode
		if currentProvider == providerLocal {
			modes = localThinkingModesForModel(ctx, cfg)
		} else {
			modes = supportedThinkingModesForModel(currentProvider, cfg.Model)
		}
		out := make([]harness.ThinkingChoice, 0, len(modes))
		for _, t := range modes {
			out = append(out, harness.ThinkingChoice{ID: t.ID, Label: t.Name})
		}
		return out
	}
	m.ThinkingStatusFn = func() string {
		if lvl := m.ActiveThinking(); lvl != "" {
			return lvl
		}
		return "unavailable"
	}
	m.ThinkingUnsupportedReasonFn = func() string {
		if currentProvider != providerLocal {
			return "this provider/model does not support reasoning"
		}
		_, declared, _ := localThinkingInfo(ctx, cfg)
		if declared {
			return fmt.Sprintf("%s does not support reasoning (per the endpoint's model listing)", cfg.Model)
		}
		return "no reasoning modes could be detected for " + cfg.Model
	}
	m.SelectThinkingFn = func(levelID string) error {
		po := providerOptionsForRuntime(currentProvider, levelID)
		if main := pool.Get(agent.MainAgentID); main != nil {
			main.SetProviderOptions(po)
		}
		if saveErr := saveThinkingMode(levelID); saveErr != nil {
			slog.Warn("wllr: could not persist thinking mode", "mode", levelID, "error", saveErr)
		}
		return nil
	}

	// Apply the startup thinking mode to the main agent: the persisted mode for
	// the provider, or — for local models — the endpoint-declared default when
	// nothing valid is persisted (so the server's own default is visible and
	// adjustable, and the agent never starts on an effort the model rejects).
	startupLevel := startupThinkingMode(ctx, cfg, currentProvider)
	if lvl := startupLevel; lvl != "" {
		m.SetActiveThinking(lvl)
	}
	// Apply the merged provider options unconditionally: OpenRouter's routing
	// preference is valid without any reasoning level, so gating this on a
	// thinking mode would silently drop a configured routing choice.
	if main := pool.Get(agent.MainAgentID); main != nil {
		main.SetProviderOptions(providerOptionsForRuntime(currentProvider, startupLevel))
	}
	// Reflect the persisted routing preference in the status bar (OpenRouter
	// only; other providers have no routing preference).
	if currentProvider == providerOpenRouter {
		m.SetSpeedDisplay(savedOpenRouterSpeed())
	}
	// On startup with a local model that cannot reason (endpoint-declared):
	// clear the (possibly stale) persisted mode and reflect the state in the
	// status bar.
	if currentProvider == providerLocal {
		modes, declared, _ := localThinkingInfo(ctx, cfg)
		if declared && len(modes) == 0 {
			if err := saveThinkingMode(""); err != nil {
				slog.Warn("wllr: could not clear thinking mode", "error", err)
			}
			m.SetThinkingUnavailable()
		}
	}

	// Apply a stored, valid OAuth token at startup (refreshing if expired) so a
	// prior /login persists across restarts. Anthropic (browser+callback) and
	// Codex/openai (device-code) are supported.
	resolveStartupOAuth(ctx, pool, cfg)

	// First-run provider auth: record the chosen auth method once per provider so
	// the prompt is not shown again. RecordAuthFn persists to the 0600 auth file.
	m.RecordAuthFn = func(provider, method string) error {
		return saveAuthCredential(provider, authCredential{Type: authType(method)})
	}
	// OAuth login flow: begin returns the modal body + a URL to copy; await blocks
	// until login resolves (Anthropic local callback server, or Codex device-code
	// poll); complete exchanges the result and swaps the live provider.
	m.BeginOAuthFn = oauthState.begin
	m.CompleteOAuthFn = oauthState.complete
	m.AwaitOAuthFn = func() (string, bool) {
		input, err := oauthState.await()
		if err != nil {
			slog.Warn("wllr: oauth login failed", "error", err)
			return "", false
		}
		return input, input != ""
	}
	if cfg.Provider == providerOpenRouter && (missingAuth || needsOpenRouterModel) {
		m.SetPendingAuthProvider(providerOpenRouter)
	} else if missingAuth && !cfg.ModelConfigured && !cfg.ProviderConfigured {
		m.SetPendingSetupWizard()
	} else if missingAuth && !hasAuthRecord(currentProvider) {
		m.SetPendingAuthProvider(currentProvider)
	}

	m.SetLogFn(func(level int, msg string) {
		lvl := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
		l := slog.LevelError
		if level >= 0 && level < len(lvl) {
			l = lvl[level]
		}
		slog.Log(ctx, l, msg)
	})

	// Session recording (messages + tool calls) is handled by the bundled
	// `history` WASM extension, which writes JSONL under ~/.wllr/sessions/ and
	// provides browse/rollback UI. The former core session.Journal was redundant
	// with it and has been removed.

	// --exec mode runs the same program as the TUI, with the renderer off and a
	// single prompt submitted. Routing through the same path is the point: exec
	// must not be a lighter-weight variant that silently skips the extension
	// hooks, provider-request chain, pool, or session recording. It stops as
	// soon as the turn finishes, which is what makes it non-interactive.
	if *execPrompt != "" {
		// No renderer and no input: the renderer is what would draw to a TTY, and
		// nil input stops bubbletea from opening one to read from. Together they
		// let the same program run unattended.
		prog := tea.NewProgram(
			&m,
			tea.WithoutRenderer(),
			tea.WithInput(nil),
			tea.WithoutSignalHandler(),
		)
		m.SetProgram(prog)
		m.SetExecWriter(os.Stdout)
		m.SubmitExecPrompt(*execPrompt)
		if _, err := prog.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
			os.Exit(1)
		}
		return
	}

	prog := tea.NewProgram(&m)
	m.SetProgram(prog)

	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wllr: "+err.Error())
		os.Exit(1)
	}
}

// setupLogging configures the default slog handler. The rolling log FILE is
// written by the bundled `logging` WASM extension (fed via EventLog), not core.
// In exec/headless mode logs also go to stderr; in TUI mode stderr is omitted
// (it would corrupt the alt-screen). Returns a cleanup function (stops the log
// dispatch goroutine) that must be deferred by the caller.
func setupLogging(h *extension.Host, tuiMode bool) func() {
	// stderr handler: only in exec/headless mode (it would corrupt the TUI alt-screen).
	var stderrH slog.Handler
	if !tuiMode {
		stderrH = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	// File/sink handler: the WASM log dispatcher, forwarding records to the
	// bundled `logging` extension (which writes ~/.wllr/logs/<ts>.log).
	dispatchH, stopDispatch := newDispatchLogHandler(h, slog.LevelDebug)

	slog.SetDefault(slog.New(newTee(stderrH, dispatchH)))
	return stopDispatch
}

// registerAgentStatusTool registers the get_agent_status native tool on h.
// The tool returns turn count, recent conversation history, and liveness state
// for a running agent.
func registerAgentStatusTool(h *extension.Host, pool *agent.AgentPool) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "get_agent_status",
		Description: "No-side-effect status check for an agent. Returns is_running, pending_messages, recent activity age, active/last tool, shutdown request state, turn_count, and recent history.",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID to inspect"},"history_limit":{"type":"integer","description":"Number of recent messages to include (default 10)"}},"required":["agent_id"]}`,
		),
		OutputSchema: json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string"},"is_running":{"type":"boolean"},"pending_messages":{"type":"integer"},"last_activity_age_ms":{"type":"integer"},"turn_duration_ms":{"type":"integer"},"last_tool_age_ms":{"type":"integer"},"active_tool":{"type":"string"},"last_tool":{"type":"string"},"shutdown_requested":{"type":"boolean"},"turn_count":{"type":"integer"},"last_summary":{"type":"string"},"recent":{"type":"array","items":{"type":"object","properties":{"role":{"type":"string"},"preview":{"type":"string"}}}}}}`,
		),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			AgentID      string `json:"agent_id"`
			HistoryLimit int    `json:"history_limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.AgentID == "" {
			return "agent_id is required", true
		}
		if in.HistoryLimit <= 0 {
			in.HistoryLimit = 10
		}
		a := pool.Get(in.AgentID)
		if a == nil {
			return fmt.Sprintf("agent %q not found", in.AgentID), true
		}
		activity := a.Activity()
		now := time.Now()
		var lastActivityAgeMS int64
		var turnDurationMS int64
		var lastToolAgeMS int64
		if !activity.LastActivityAt.IsZero() {
			lastActivityAgeMS = now.Sub(activity.LastActivityAt).Milliseconds()
		}
		if a.IsRunning() && !activity.TurnStartedAt.IsZero() {
			turnDurationMS = now.Sub(activity.TurnStartedAt).Milliseconds()
		}
		if !activity.LastToolCallAt.IsZero() {
			lastToolAgeMS = now.Sub(activity.LastToolCallAt).Milliseconds()
		}
		history := a.History()
		start := len(history) - in.HistoryLimit
		if start < 0 {
			start = 0
		}
		recent := history[start:]
		type msgOut struct {
			Role    string `json:"role"`
			Preview string `json:"preview"`
		}
		msgs := make([]msgOut, 0, len(recent))
		for _, m := range recent {
			preview := string([]rune(m.Content))
			if r := []rune(preview); len(r) > 200 {
				preview = string(r[:200]) + "…"
			}
			msgs = append(msgs, msgOut{Role: string(m.Role), Preview: preview})
		}
		out, _ := json.Marshal(map[string]any{
			"agent_id":             in.AgentID,
			"is_running":           a.IsRunning(),
			"pending_messages":     a.InboxLen(),
			"last_activity_age_ms": lastActivityAgeMS,
			"turn_duration_ms":     turnDurationMS,
			"last_tool_age_ms":     lastToolAgeMS,
			"active_tool":          activity.ActiveToolName,
			"last_tool":            activity.LastToolName,
			"shutdown_requested":   activity.ShutdownRequested,
			"turn_count":           len(history) / 2,
			"last_summary":         a.LastSummary(),
			"recent":               msgs,
		})
		return string(out), false
	})
}

// loadBuiltinExtensions loads the trusted built-in WASM extensions with
// least-privilege permissions sourced from the checked-in permission manifests
// in cmd/builtins (<name>.manifest.json). The manifest is the source of truth,
// independent of the compiled WASM bytes: if the WASM is ever compromised to
// call exec, http_post, http_get, or mcp_spawn, the host still denies it because
// the declared manifest grants only what each built-in legitimately needs.
//   - agents, statusline drive the TUI scene graph (ui_patch) -> require ui
//   - logging appends to a log file (append_file) -> requires file_write
//   - history uses list_sessions (host-side picker data) -> requires file_read
//   - queue uses only unrestricted host calls (store,
//     modal, notify, register_command, agent_list/mailbox) -> require none
//   - plan drives a compact sidebar widget (ui_patch) -> requires ui
//
// Loading fails closed: a missing, unreadable, or malformed manifest yields
// zero permissions (and a warning), never an implicit all-permissions grant.
func loadBuiltinExtensions(ctx context.Context, h *extension.Host) {
	for _, name := range []string{"agents", "history", "logging", "plan", "prompt", "queue", "statusline"} {
		filename := name + ".wasm"
		data, err := builtinFS.ReadFile("builtins/" + filename)
		if err != nil {
			slog.Warn(
				"wllr: built-in extension missing; run `make extensions` before building",
				"extension",
				name,
				"error",
				err,
			)
			continue
		}
		perms := builtinManifestPermissions(name)
		if loadErr := h.LoadBytes(ctx, filename, data, true, perms...); loadErr != nil {
			fmt.Fprintf(os.Stderr, "wllr: load built-in extension %q: %v\n", name, loadErr)
		}
	}
}

// builtinManifestPermissions reads the checked-in permission manifest for a
// built-in from the embedded FS. It is the source of truth for built-in
// permissions and is independent of the compiled WASM bytes. Fails closed:
// a missing or malformed manifest returns nil (zero permissions).
func builtinManifestPermissions(name string) []sdk.Permission {
	data, err := builtinFS.ReadFile("builtins/" + name + ".manifest.json")
	if err != nil {
		slog.Warn(
			"wllr: built-in permission manifest missing; granting zero permissions",
			"extension",
			name,
			"error",
			err,
		)
		return nil
	}
	var manifest struct {
		Permissions []sdk.Permission `json:"permissions"`
	}
	if jerr := json.Unmarshal(data, &manifest); jerr != nil {
		slog.Warn(
			"wllr: built-in permission manifest malformed; granting zero permissions",
			"extension",
			name,
			"error",
			jerr,
		)
		return nil
	}
	// Drop unknown permission names so a tampered manifest can never grant a
	// capability that does not exist in the SDK (fail closed to zero for those).
	valid := map[sdk.Permission]bool{
		sdk.PermExec:         true,
		sdk.PermFileOpen:     true,
		sdk.PermFileRead:     true,
		sdk.PermFileWrite:    true,
		sdk.PermNetworkRead:  true,
		sdk.PermNetworkWrite: true,
		sdk.PermUI:           true,
	}
	out := make([]sdk.Permission, 0, len(manifest.Permissions))
	for _, p := range manifest.Permissions {
		if valid[p] {
			out = append(out, p)
		}
	}
	return out
}

// startMCPBridge initializes the MCP server bridge and returns a cleanup function.
// Startup errors are non-fatal: the bridge logs a warning and the app continues.
func startMCPBridge(ctx context.Context, h *extension.Host) func() {
	mcpExt := mcp.NewExtension(h)
	if mcpErr := mcpExt.Start(ctx); mcpErr != nil {
		slog.Warn("wllr: mcp bridge init failed", "error", mcpErr)
	}
	return func() {
		if closeErr := mcpExt.Close(); closeErr != nil {
			slog.Warn("wllr: close mcp bridge", "error", closeErr)
		}
	}
}

// configPath returns the path to the shared wllr config file.
func configPath() string {
	if p := os.Getenv("WLLR_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wllr/config.yaml"
	}
	return filepath.Join(home, ".config", "wllr", "config.yaml")
}

// loadConfigGroup reads the config file and returns the JSON blob for the given group.
// Returns {} if the group does not exist. The config file is a flat YAML object
// keyed by group name (extension name or "wllr" for the main app).
func loadConfigGroup(group string) (json.RawMessage, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return json.RawMessage("{}"), nil
		}
		return nil, err
	}

	// Parse YAML into map
	var all map[string]yaml.Node
	if err := yaml.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("config: parse error: %w", err)
	}
	if v, ok := all[group]; ok {
		// Decode the YAML node and re-encode as JSON. Marshaling the node back
		// to YAML would emit YAML for a YAML source file, but every caller
		// json.Unmarshals this result, so the group must always be JSON here.
		var value any
		if err := v.Decode(&value); err != nil {
			return nil, fmt.Errorf("config: decode error: %w", err)
		}
		out, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config: marshal error: %w", err)
		}
		return json.RawMessage(out), nil
	}

	return json.RawMessage("{}"), nil
}

// httpPost performs an HTTP POST request and returns the status code and body.
// Extracted from main() to keep cyclomatic complexity within the project limit.
func httpPost(url string, headers map[string]string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(
		req,
	) //nolint:gosec // URL is from user config; SSRF is intentional
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err = buf.ReadFrom(resp.Body); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, buf.Bytes(), nil
}

func httpGet(url string, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(
		req,
	) //nolint:gosec // URL is from user config; SSRF is intentional
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err = buf.ReadFrom(resp.Body); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, buf.Bytes(), nil
}
