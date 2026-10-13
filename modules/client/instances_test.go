package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildInstancesFromFlags pins the flag form: `id=url`, id and URL
// whitespace-trimmed, every flag instance carrying the default token.
func TestBuildInstancesFromFlags(t *testing.T) {
	cfgs, err := BuildInstances(InstanceFlags{
		"local=http://127.0.0.1:8765",
		"ci = http://127.0.0.1:8766",
	}, "", "t", "instance", "config")
	if err != nil {
		t.Fatalf("BuildInstances: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("got %d configs, want 2", len(cfgs))
	}
	if cfgs[0].ID != "local" || cfgs[0].BaseURL != "http://127.0.0.1:8765" || cfgs[0].Token != "t" {
		t.Errorf("cfgs[0] = %+v, want {local http://127.0.0.1:8765 t}", cfgs[0])
	}
	if cfgs[1].ID != "ci" || cfgs[1].BaseURL != "http://127.0.0.1:8766" || cfgs[1].Token != "t" {
		t.Errorf("cfgs[1] = %+v, want {ci http://127.0.0.1:8766 t} (whitespace trimmed)", cfgs[1])
	}
}

// TestBuildInstancesFromFile pins the JSON file shape and token precedence: an
// entry's own token wins over the default, an entry without one takes the
// default.
func TestBuildInstancesFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	data := `[{"id":"ci","url":"http://x"},{"id":"dev","url":"http://y","token":"own"}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgs, err := BuildInstances(nil, path, "t", "instance", "config")
	if err != nil {
		t.Fatalf("BuildInstances: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("got %d configs, want 2", len(cfgs))
	}
	if cfgs[0].ID != "ci" || cfgs[0].Token != "t" {
		t.Errorf("cfgs[0] = %+v, want ci with default token t", cfgs[0])
	}
	if cfgs[1].ID != "dev" || cfgs[1].Token != "own" {
		t.Errorf("cfgs[1] = %+v, want dev with its own token own", cfgs[1])
	}
}

// TestBuildInstancesTrimsFileEntry pins that a file entry is whitespace-trimmed
// like a flag entry, so `[{"id":" ci ","url":" http://x "}]` behaves like
// `-peer " ci = http://x "` instead of being dropped later by the server's
// peer-id charset check with only a warning.
func TestBuildInstancesTrimsFileEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	if err := os.WriteFile(path, []byte(`[{"id":" ci ","url":" http://x "}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgs, err := BuildInstances(nil, path, "t", "peer", "peers")
	if err != nil {
		t.Fatalf("BuildInstances: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("got %d configs, want 1", len(cfgs))
	}
	if cfgs[0].ID != "ci" || cfgs[0].BaseURL != "http://x" {
		t.Errorf("cfgs[0] = %+v, want {ci http://x} (whitespace trimmed)", cfgs[0])
	}
}

// TestBuildInstancesFlagsAndFile pins the merge order: flag instances first,
// then file entries, preserving today's buildConfigs order.
func TestBuildInstancesFlagsAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	if err := os.WriteFile(path, []byte(`[{"id":"dev","url":"http://y"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgs, err := BuildInstances(InstanceFlags{"ci=http://x"}, path, "t", "instance", "config")
	if err != nil {
		t.Fatalf("BuildInstances: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("got %d configs, want 2", len(cfgs))
	}
	if cfgs[0].ID != "ci" || cfgs[1].ID != "dev" {
		t.Errorf("order = [%s %s], want [ci dev] (flags first)", cfgs[0].ID, cfgs[1].ID)
	}
}

// TestBuildInstancesRejectsBadFlag pins the exact error string so the
// `wllr-multiplex` behavior is byte-for-byte unchanged by the refactor.
func TestBuildInstancesRejectsBadFlag(t *testing.T) {
	_, err := BuildInstances(InstanceFlags{"nope"}, "", "t", "instance", "config")
	if err == nil {
		t.Fatal("expected an error for a flag without id=url")
	}
	if want := `bad -instance "nope": want id=url`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestBuildInstancesRejectsBadFileEntry pins that a file entry is validated like
// a flag entry: a missing id or url is an error, not a silent drop, so a typo in
// the operator's peers file is surfaced rather than becoming an invisible
// omission.
func TestBuildInstancesRejectsBadFileEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	if err := os.WriteFile(path, []byte(`[{"id":"ci","url":""}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildInstances(nil, path, "t", "peer", "peers")
	if err == nil {
		t.Fatal("expected an error for a file entry with an empty url")
	}
	if !strings.Contains(err.Error(), "want id and url") {
		t.Errorf("error = %q, want it to name the missing id/url", err.Error())
	}
}

// TestBuildInstancesLabelsNameCallerFlags pins that the error wording follows the
// caller's own flag names, so `wllr serve` never tells the operator to pass flags
// it does not have.
func TestBuildInstancesLabelsNameCallerFlags(t *testing.T) {
	_, err := BuildInstances(InstanceFlags{"nope"}, "", "t", "peer", "peers")
	if err == nil {
		t.Fatal("expected an error")
	}
	if want := `bad -peer "nope": want id=url`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestBuildInstancesRequiresOne pins that a caller must declare at least one
// instance; the file/flag-less case is an error (the `wllr serve` caller
// short-circuits before reaching this). The error wraps ErrNoInstances so a
// caller that may legitimately have none can recognize it.
func TestBuildInstancesRequiresOne(t *testing.T) {
	_, err := BuildInstances(nil, "", "t", "instance", "config")
	if err == nil {
		t.Fatal("expected an error when no instances are declared")
	}
	if !strings.Contains(err.Error(), "no instances") {
		t.Errorf("error = %q, want it to mention no instances", err.Error())
	}
	if !errors.Is(err, ErrNoInstances) {
		t.Errorf("error = %T, want it to wrap ErrNoInstances so callers can recognize it", err)
	}
}
