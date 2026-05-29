package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
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

func (p *osCapabilityProvider) HTTPPost(url string, headers map[string]string, body []byte) (int, []byte, error) {
	return httpPost(url, headers, body)
}

func (p *osCapabilityProvider) ConfigRead(group string) (json.RawMessage, error) {
	return loadConfigGroup(group)
}

// compile-time check
var _ extension.CapabilityProvider = (*osCapabilityProvider)(nil)
