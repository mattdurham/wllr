package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// InstanceFlags collects repeated `id=url` instance declarations from the command
// line. It implements flag.Value, so a binary can bind it with flag.Var without
// this package importing flag.
type InstanceFlags []string

// String renders the collected declarations for flag's usage output.
func (f *InstanceFlags) String() string { return strings.Join(*f, ",") }

// Set appends one `id=url` declaration. Validation happens in BuildInstances so
// the error can name the shared flag.
func (f *InstanceFlags) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// instanceFileEntry is one entry in an instances JSON file.
type instanceFileEntry struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// ErrNoInstances is returned (wrapped) by BuildInstances when the flag and file
// sources declare no usable instance. Its message names the *caller's* flags,
// because `wllr serve` and `wllr-multiplex` spell them differently
// ("peer"/"peers" vs "instance"/"config"). A caller that may legitimately have
// no instances (a plain `wllr serve`) recognizes it with errors.Is and continues
// instead of treating it as a fault.
var ErrNoInstances = errors.New("no instances")

// BuildInstances merges flag and file declarations into Configs. Flag instances
// take the default token; a file entry's own token wins over it. instanceFlag and
// configFlag are the flag names the *caller* uses, so every error names a flag
// the caller actually has. It returns an error wrapping ErrNoInstances when
// neither source declares an instance, because every caller needs at least one
// (the `wllr serve` caller short-circuits the no-source case and treats an empty
// source as no peers).
func BuildInstances(
	flags InstanceFlags,
	configPath, defaultToken, instanceFlag, configFlag string,
) ([]Config, error) {
	var cfgs []Config
	for _, raw := range flags {
		id, url, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("bad -%s %q: want id=url", instanceFlag, raw)
		}
		cfgs = append(
			cfgs,
			Config{
				ID:      strings.TrimSpace(id),
				BaseURL: strings.TrimSpace(url),
				Token:   defaultToken,
			},
		)
	}
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}
		var fileCfgs []instanceFileEntry
		if err := json.Unmarshal(data, &fileCfgs); err != nil {
			return nil, fmt.Errorf("parse %s: %w", configPath, err)
		}
		for i, fc := range fileCfgs {
			// A file entry is validated like a flag entry, rather than being
			// silently dropped later by the server's buildPeers: a typo in the
			// operator's file should be an error, not an invisible omission. The
			// stored id/url are trimmed too, so `[{"id":" ci ","url":" http://x "}]`
			// behaves like `-peer " ci = http://x "`.
			if strings.TrimSpace(fc.ID) == "" || strings.TrimSpace(fc.URL) == "" {
				return nil, fmt.Errorf("bad -%s entry %d in %s: want id and url", instanceFlag, i, configPath)
			}
			tok := fc.Token
			if tok == "" {
				tok = defaultToken
			}
			cfgs = append(cfgs, Config{ID: strings.TrimSpace(fc.ID), BaseURL: strings.TrimSpace(fc.URL), Token: tok})
		}
	}
	if len(cfgs) == 0 {
		return nil, fmt.Errorf("%w: pass -%s id=url or -%s file.json", ErrNoInstances, instanceFlag, configFlag)
	}
	return cfgs, nil
}
