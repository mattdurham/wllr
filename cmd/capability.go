package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk/md"
)

// osCapabilityProvider implements extension.CapabilityProvider using host OS calls.
// It is constructed once at startup and passed to extension.Host.SetCapabilities.
type osCapabilityProvider struct {
	pool *agent.AgentPool
}

// newOSCapabilityProvider creates a CapabilityProvider backed by the host OS.
// pool is used to apply system prompt changes to all agents.
func newOSCapabilityProvider(pool *agent.AgentPool) extension.CapabilityProvider {
	return &osCapabilityProvider{pool: pool}
}

func (p *osCapabilityProvider) Exec(ctx context.Context, command, dir string, onLine func(string)) (string, error) {
	return runExec(ctx, command, dir, onLine)
}

func (p *osCapabilityProvider) GetEnv(name string) (string, error) {
	if name != "" {
		return os.Getenv(name), nil
	}
	vars := os.Environ()
	data, _ := json.Marshal(vars)
	return string(data), nil
}

func (p *osCapabilityProvider) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (p *osCapabilityProvider) WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (p *osCapabilityProvider) AppendFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, werr := f.WriteString(content); werr != nil {
		_ = f.Close()
		return werr
	}
	return f.Close()
}

func (p *osCapabilityProvider) HTTPPost(url string, headers map[string]string, body []byte) (int, []byte, error) {
	return httpPost(url, headers, body)
}

func (p *osCapabilityProvider) HTTPGet(url string, headers map[string]string) (int, []byte, error) {
	return httpGet(url, headers)
}

func (p *osCapabilityProvider) ConfigRead(group string) (json.RawMessage, error) {
	raw, err := loadConfigGroup(group)
	if err != nil {
		return nil, err
	}
	if group == wllrConfigGroup {
		raw = redactWllrAPIKeys(raw)
	}
	return raw, nil
}

// redactWllrAPIKeys removes local model API keys from the app-level "wllr"
// group before an extension sees it. The group is readable by extensions (the
// bundled context extension reads it for prompt_override and prompt_files), and
// it carries local_models[].api_key values no extension needs, so the keys are
// stripped rather than exposed through config_read. Unmarshalable data — which
// json.Marshal should never produce here — fails closed to an empty object
// rather than leaking the raw group.
func redactWllrAPIKeys(raw json.RawMessage) json.RawMessage {
	var group map[string]json.RawMessage
	if json.Unmarshal(raw, &group) != nil {
		return raw
	}
	modelsRaw, ok := group["local_models"]
	if !ok {
		return raw
	}
	var models []map[string]json.RawMessage
	if json.Unmarshal(modelsRaw, &models) != nil {
		return raw
	}
	stripped := false
	for _, m := range models {
		if _, ok := m["api_key"]; ok {
			delete(m, "api_key")
			stripped = true
		}
	}
	if !stripped {
		return raw
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		return json.RawMessage("{}")
	}
	group["local_models"] = encoded
	out, err := json.Marshal(group)
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}

func (p *osCapabilityProvider) FormatMarkdown(markdown string) string {
	return md.Render(markdown)
}

// compile-time check
var _ extension.CapabilityProvider = (*osCapabilityProvider)(nil)
