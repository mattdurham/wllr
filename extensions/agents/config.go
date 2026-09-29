// Loop-guard configuration loading. The agents extension reads its own
// per-extension config file (~/.wllr/extensions/agents/config.yaml) through
// the host's config_read call — the same pattern as the permissions
// extension. Missing or empty config is a fine default state (fresh-install
// rule): the guard runs with built-in defaults. A malformed config is an
// error, never silent.
//
// This file is untagged so it can be tested natively.

package main

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// agentsConfig is the extension's config file shape.
type agentsConfig struct {
	LoopGuard loopGuardConfig `yaml:"loop_guard"`
}

// configReadOverride, when set (native tests only), replaces the host
// ConfigRead call.
var configReadOverride func() ([]byte, error)

// loadAgentsConfig reads and parses the extension config, applying defaults
// to every zero field. A missing file yields the pure defaults.
func loadAgentsConfig() (loopGuardConfig, error) {
	var data []byte
	var err error
	switch {
	case configReadOverride != nil:
		data, err = configReadOverride()
	case configReadHost != nil:
		raw, rerr := configReadHost()
		data, err = []byte(raw), rerr
	}
	if err != nil {
		return loopGuardConfig{}, fmt.Errorf("config_read: %w", err)
	}
	var cfg agentsConfig
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return loopGuardConfig{}, fmt.Errorf("parse agents config: %w", err)
		}
	}
	return cfg.LoopGuard.applyDefaults(), nil
}
