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
func savedModel() string {
	raw, err := loadConfigGroup(wllrConfigGroup)
	if err != nil {
		return ""
	}
	var g struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(raw, &g) != nil {
		return ""
	}
	return g.Model
}

// saveModel persists the model selection to the "wllr" group of the config
// file, preserving all other groups and keys. Best-effort: returns an error the
// caller may surface, but never partially writes (temp-file + rename).
func saveModel(modelID string) error {
	path := configPath()

	// Read the whole config object (or start empty).
	all := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all) // tolerate a malformed file by overwriting
	}

	// Merge model into the "wllr" group, preserving its other keys.
	group := map[string]json.RawMessage{}
	if existing, ok := all[wllrConfigGroup]; ok {
		_ = json.Unmarshal(existing, &group)
	}
	mv, err := json.Marshal(modelID)
	if err != nil {
		return err
	}
	group["model"] = mv
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
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
