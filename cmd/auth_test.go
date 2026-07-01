package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// withAuthPath points WLLR_AUTH at a temp file for the duration of a test.
func withAuthPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	t.Setenv("WLLR_AUTH", path)
	return path
}

func TestHasAuthRecord_MissingFile(t *testing.T) {
	withAuthPath(t)
	if hasAuthRecord("anthropic") {
		t.Error("hasAuthRecord on missing file should be false")
	}
}

func TestSaveAuthCredential_RoundTrip(t *testing.T) {
	withAuthPath(t)

	if err := saveAuthCredential("anthropic", authCredential{Type: authTypeOAuth}); err != nil {
		t.Fatalf("saveAuthCredential: %v", err)
	}
	if !hasAuthRecord("anthropic") {
		t.Error("hasAuthRecord should be true after save")
	}
	cred, ok := loadAuthCredential("anthropic")
	if !ok {
		t.Fatal("loadAuthCredential: not found after save")
	}
	if cred.Type != authTypeOAuth {
		t.Errorf("cred.Type = %q, want oauth", cred.Type)
	}
}

func TestSaveAuthCredential_PreservesOtherProviders(t *testing.T) {
	path := withAuthPath(t)

	if err := saveAuthCredential("anthropic", authCredential{Type: authTypeOAuth}); err != nil {
		t.Fatalf("save anthropic: %v", err)
	}
	if err := saveAuthCredential("openai", authCredential{Type: authTypeAPIKey}); err != nil {
		t.Fatalf("save openai: %v", err)
	}

	// Both entries present.
	if c, ok := loadAuthCredential("anthropic"); !ok || c.Type != authTypeOAuth {
		t.Errorf("anthropic entry lost or wrong: %+v ok=%v", c, ok)
	}
	if c, ok := loadAuthCredential("openai"); !ok || c.Type != authTypeAPIKey {
		t.Errorf("openai entry lost or wrong: %+v ok=%v", c, ok)
	}

	// File is 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("auth file perm = %o, want 600", perm)
	}
}

func TestSaveAuthCredential_Overwrite(t *testing.T) {
	withAuthPath(t)

	if err := saveAuthCredential("anthropic", authCredential{Type: authTypeAPIKey}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := saveAuthCredential("anthropic", authCredential{Type: authTypeOAuth}); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	cred, _ := loadAuthCredential("anthropic")
	if cred.Type != authTypeOAuth {
		t.Errorf("cred.Type after overwrite = %q, want oauth", cred.Type)
	}
}

func TestLoadAuthFile_MalformedTolerated(t *testing.T) {
	path := withAuthPath(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A malformed file is overwritten fresh by save rather than blocking it.
	if err := saveAuthCredential("gemini", authCredential{Type: authTypeAPIKey}); err != nil {
		t.Fatalf("saveAuthCredential over malformed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var all map[string]authCredential
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatalf("file not valid JSON after save: %v", err)
	}
	if all["gemini"].Type != authTypeAPIKey {
		t.Errorf("gemini entry missing after recovering from malformed file")
	}
}
