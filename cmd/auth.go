package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Provider authentication records live in a dedicated, 0600 auth file
// (~/.config/wllr/auth.json), separate from the plaintext config. It is keyed
// by provider name; the presence of an entry is the record that the user has
// already chosen an auth method for that provider — so the first-run prompt is
// only shown once per provider.
//
// This mirrors pi's auth.json shape:
//
//	{ "anthropic": { "type": "oauth" }, "openai": { "type": "api_key" } }
//
// The actual OAuth token-exchange flow is a separate, later step; today the
// record captures the chosen method and the API key still resolves from the
// provider's environment variable.

// authType is the auth method recorded for a provider.
type authType string

const (
	authTypeAPIKey authType = "api_key"
	authTypeOAuth  authType = "oauth"
)

// authCredential is one provider's recorded auth entry.
type authCredential struct {
	Type authType `json:"type"`
	// Key, when set, is a stored API key. Optional: for api_key providers the
	// key normally resolves from the environment.
	Key string `json:"key,omitempty"`
	// Access/Refresh/Expires hold OAuth tokens (type == oauth). Access is the
	// bearer token used as the API key; Refresh renews it; Expires is the access
	// token's absolute expiry in unix ms (with a safety margin already applied).
	Access  string `json:"access,omitempty"`
	Refresh string `json:"refresh,omitempty"`
	// AccountID is the ChatGPT account id extracted from a Codex access token;
	// required to route Codex API calls. Empty for providers that don't use it.
	AccountID string `json:"account_id,omitempty"`
	Expires   int64  `json:"expires,omitempty"`
}

// isExpired reports whether an OAuth access token is past its (margin-adjusted)
// expiry. A zero Expires is treated as not-expired (unknown lifetime).
func (c authCredential) isExpired() bool {
	return c.Expires != 0 && time.Now().UnixMilli() >= c.Expires
}

// authPath returns the path to the auth file. It honors WLLR_AUTH for tests and
// otherwise sits next to config.json.
func authPath() string {
	if p := os.Getenv("WLLR_AUTH"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(configPath()), "auth.json")
}

// loadAuthCredential returns the recorded credential for a provider and whether
// one exists. A missing/unreadable file yields (zero, false).
func loadAuthCredential(provider string) (authCredential, bool) {
	all, err := loadAuthFile()
	if err != nil {
		return authCredential{}, false
	}
	c, ok := all[provider]
	return c, ok
}

// hasAuthRecord reports whether the user has already chosen an auth method for
// the provider. This is the "don't ask again" gate.
func hasAuthRecord(provider string) bool {
	_, ok := loadAuthCredential(provider)
	return ok
}

// loadAuthFile reads and parses the whole auth file. A missing file is not an
// error (returns an empty map).
func loadAuthFile() (map[string]authCredential, error) {
	data, err := os.ReadFile(authPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]authCredential{}, nil
		}
		return nil, err
	}
	all := map[string]authCredential{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("auth: parse error: %w", err)
	}
	return all, nil
}

// saveAuthCredential records (or overwrites) the credential for a provider,
// preserving all other providers' entries. The file is written 0600 via a
// temp-file + rename so it is never partially written.
func saveAuthCredential(provider string, cred authCredential) error {
	all, err := loadAuthFile()
	if err != nil {
		// Tolerate a malformed file by starting fresh rather than refusing to save.
		all = map[string]authCredential{}
	}
	all[provider] = cred

	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}

	path := authPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the auth directory.
		return err
	}
	if _, err := f.Write(out); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the auth directory.
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp) //nolint:gosec // tmp is returned by os.CreateTemp in the auth directory.
		return err
	}
	return os.Rename(tmp, path) //nolint:gosec // tmp is returned by os.CreateTemp in the auth directory.
}
