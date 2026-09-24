package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// readConfigGroups parses the config file into a map of group name to a JSON
// object. The file is documented as YAML, so a YAML parse is authoritative;
// JSON is a YAML subset and is handled by the same path. A missing or malformed
// file yields an empty map so callers can proceed (and overwrite) rather than
// losing the write entirely.
func readConfigGroups() map[string]json.RawMessage {
	all := map[string]json.RawMessage{}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return all
	}
	var nodes map[string]any
	if err := yaml.Unmarshal(data, &nodes); err != nil {
		return all
	}
	for group, value := range nodes {
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		all[group] = encoded
	}
	return all
}

// The persisted model selection lives in the app config file
// (~/.config/wllr/config.yaml) under the "wllr" group as {"model": "<id>"}.
// loadConfigGroup routes the "wllr" group to this file; every other group is an
// extension's config, which lives in its own folder instead.

// wllrConfigGroup is the config group holding core wllr settings.
const wllrConfigGroup = "wllr"

// savedModel returns the persisted model selection, or "" if none is stored or
// the config file is missing/unreadable.
func savedModel() string { return savedWllrField("model") }

// saveModel persists the model selection to the "wllr" group of the config file.
func saveModel(modelID string) error { return saveWllrField("model", modelID) }

// savedProvider returns the persisted provider selection, or "" if none is
// stored or the config file is missing/unreadable.
func savedProvider() string { return savedWllrField("provider") }

// saveProvider persists the provider selection to the "wllr" group.
func saveProvider(provider string) error { return saveWllrField("provider", provider) }

// savedWllrField reads a single string field from the "wllr" config group, or
// "" if absent/unreadable.
func savedWllrField(field string) string {
	raw, err := loadConfigGroup(wllrConfigGroup)
	if err != nil {
		return ""
	}
	var g map[string]json.RawMessage
	if json.Unmarshal(raw, &g) != nil {
		return ""
	}
	var value string
	if json.Unmarshal(g[field], &value) != nil {
		return ""
	}
	return value
}

// saveWllrField persists a single string field to the "wllr" group of the
// config file, preserving all other groups and keys. Best-effort: returns an
// error the caller may surface, but never partially writes (temp-file + rename).
func saveWllrField(field, value string) error { return saveWllrRawField(field, value) }

// localModelConfigWire is the on-disk shape of localModelConfig: ContextWindow
// is written as a plain JSON number instead of localModelConfig's RawMessage
// field (which exists only to tolerate string/number values on read).
type localModelConfigWire struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	BaseURL       string   `json:"base_url"`
	APIKey        string   `json:"api_key"`
	ThinkingModes []string `json:"thinking_modes,omitempty"`
	ContextWindow int64    `json:"context_window,omitempty"`
}

// saveLocalModels persists the full local_models list to the "wllr" group.
func saveLocalModels(models []localModelConfig) error {
	wire := make([]localModelConfigWire, 0, len(models))
	for _, m := range models {
		wire = append(wire, localModelConfigWire{
			ID:            m.ID,
			Name:          m.Name,
			BaseURL:       m.BaseURL,
			APIKey:        m.APIKey,
			ContextWindow: m.ContextWindow,
			ThinkingModes: m.ThinkingModes,
		})
	}
	return saveWllrRawField("local_models", wire)
}

// saveWllrRawField persists any JSON-marshalable value to a field in the
// "wllr" group of the config file, preserving all other groups and keys.
// Best-effort: returns an error the caller may surface, but never partially
// writes (temp-file + rename).
func saveWllrRawField(field string, value any) error {
	// Read the whole config object (or start empty).
	all := readConfigGroups()

	// Merge the field into the "wllr" group, preserving its other keys.
	group := map[string]json.RawMessage{}
	if existing, ok := all[wllrConfigGroup]; ok {
		_ = json.Unmarshal(existing, &group)
	}
	mv, err := json.Marshal(value)
	if err != nil {
		return err
	}
	group[field] = mv
	gv, err := json.Marshal(group)
	if err != nil {
		return err
	}
	all[wllrConfigGroup] = gv

	return writeWllrConfig(all)
}

// removeWllrField deletes a field from the "wllr" group of the config file,
// preserving all other groups and keys. Removing an absent field is a no-op
// that leaves the file untouched. Best-effort: returns an error the caller may
// surface, but never partially writes (temp-file + rename).
func removeWllrField(field string) error {
	all := readConfigGroups()

	group := map[string]json.RawMessage{}
	if existing, ok := all[wllrConfigGroup]; ok {
		_ = json.Unmarshal(existing, &group)
	}
	if _, ok := group[field]; !ok {
		return nil
	}
	delete(group, field)
	gv, err := json.Marshal(group)
	if err != nil {
		return err
	}
	all[wllrConfigGroup] = gv

	return writeWllrConfig(all)
}

// writeWllrConfig serializes the config object as YAML and atomically replaces
// the app config file. YAML is the documented config format and the format the
// loaders expect, so the file is written as YAML rather than as JSON (a YAML
// subset) that only happened to parse.
func writeWllrConfig(all map[string]json.RawMessage) error {
	nodes := make(map[string]any, len(all))
	for group, raw := range all {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("config: encode group %q: %w", group, err)
		}
		nodes[group] = value
	}
	out, err := yaml.Marshal(nodes)
	if err != nil {
		return err
	}
	return replaceConfigFile(configPath(), out)
}

// replaceConfigFile atomically replaces path with data: the write goes to a
// temp file in the same directory and is renamed over the target, so readers
// never observe a partial file.
func replaceConfigFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the target directory.
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the target directory.
		return err
	}
	return os.Rename(tmp, path) //nolint:gosec // tmp is returned by os.CreateTemp in the target directory.
}
