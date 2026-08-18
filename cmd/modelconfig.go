package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// The persisted model selection lives in the shared config file
// (~/.config/wllr/config.json) under the "wllr" group as {"model": "<id>"}.
// This is the same flat, group-keyed JSON that loadConfigGroup reads for
// extensions; here we read/write the "wllr" group's model field directly.

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
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	ContextWindow int64  `json:"context_window,omitempty"`
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
		})
	}
	return saveWllrRawField("local_models", wire)
}

// saveWllrRawField persists any JSON-marshalable value to a field in the
// "wllr" group of the config file, preserving all other groups and keys.
// Best-effort: returns an error the caller may surface, but never partially
// writes (temp-file + rename).
func saveWllrRawField(field string, value any) error {
	path := configPath()

	// Read the whole config object (or start empty).
	all := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all) // tolerate a malformed file by overwriting
	}

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

	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: temp file in the same dir + rename.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(out); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the config directory.
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the config directory.
		return err
	}
	return os.Rename(tmp, path) //nolint:gosec // tmp is returned by os.CreateTemp in the config directory.
}
