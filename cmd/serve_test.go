package main

// serve_test.go — tests for the `wllr serve` multi-instance configuration seam:
// flags → shared client parser → server-owned peer table, plus the serveArgs
// rewrite that must pass peer flags through untouched.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/client"
	"github.com/mattdurham/wllr/modules/server"
)

// TestParseServeInstances pins the flag form, the self-id default, and the peer
// token precedence (-peer-token vs a file entry's own token).
func TestParseServeInstances(t *testing.T) {
	sc, err := parseServeInstances("local", []string{"ci=http://x"}, "", "t")
	if err != nil {
		t.Fatalf("parseServeInstances: %v", err)
	}
	if sc.SelfID != "local" {
		t.Errorf("SelfID = %q, want local", sc.SelfID)
	}
	want := []server.Peer{{ID: "ci", BaseURL: "http://x", Token: "t"}}
	if !reflect.DeepEqual(sc.Peers, want) {
		t.Errorf("Peers = %+v, want %+v", sc.Peers, want)
	}

	// An empty self id defaults to the server's DefaultSelfID.
	sc, err = parseServeInstances("", []string{"ci=http://x"}, "", "t")
	if err != nil {
		t.Fatalf("parseServeInstances: %v", err)
	}
	if sc.SelfID != server.DefaultSelfID {
		t.Errorf("SelfID = %q, want %q", sc.SelfID, server.DefaultSelfID)
	}

	// A -peers file entry's own token wins over -peer-token.
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(path, []byte(`[{"id":"dev","url":"http://y","token":"own"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err = parseServeInstances("local", nil, path, "t")
	if err != nil {
		t.Fatalf("parseServeInstances: %v", err)
	}
	if len(sc.Peers) != 1 || sc.Peers[0].Token != "own" {
		t.Errorf("Peers = %+v, want dev with its own token own", sc.Peers)
	}

	// A malformed -peer flag is an error naming wllr serve's own flag, not a
	// -instance flag it does not have.
	if _, err := parseServeInstances("local", []string{"nope"}, "", "t"); err == nil {
		t.Error("expected an error for a malformed -peer flag")
	} else if !strings.Contains(err.Error(), "bad -peer") {
		t.Errorf("error = %q, want it to name -peer", err.Error())
	}
}

// TestParseServeInstancesNoPeers pins that a plain `wllr serve` (no -peer, no
// -peers) yields an empty peer list and no error, so behavior is unchanged.
func TestParseServeInstancesNoPeers(t *testing.T) {
	sc, err := parseServeInstances("local", nil, "", "t")
	if err != nil {
		t.Fatalf("parseServeInstances: %v", err)
	}
	if sc.SelfID != "local" {
		t.Errorf("SelfID = %q, want local", sc.SelfID)
	}
	if len(sc.Peers) != 0 {
		t.Errorf("Peers = %+v, want none", sc.Peers)
	}
}

// TestParseServeInstancesEmptyFile pins that a -peers file that parses but yields
// zero peers (an empty `[]`, or a generated file with no entries) is treated as
// "no peers": the server still starts, exactly like the no-flag case. Otherwise
// an operator with an empty peers file cannot start `wllr serve` at all.
func TestParseServeInstancesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := parseServeInstances("local", nil, path, "t")
	if err != nil {
		t.Fatalf("parseServeInstances with an empty peers file: %v", err)
	}
	if sc.SelfID != "local" {
		t.Errorf("SelfID = %q, want local", sc.SelfID)
	}
	if len(sc.Peers) != 0 {
		t.Errorf("Peers = %+v, want none", sc.Peers)
	}
}

// TestPeersFromConfigs pins that the token survives the client→server mapping; a
// dropped token would make the proxy 401 against a token-authed peer.
func TestPeersFromConfigs(t *testing.T) {
	got := peersFromConfigs([]client.Config{{ID: "ci", BaseURL: "http://x", Token: "secret"}})
	want := []server.Peer{{ID: "ci", BaseURL: "http://x", Token: "secret"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("peersFromConfigs = %+v, want %+v", got, want)
	}
}

// TestServeArgsKeepsPeerFlags pins that serveArgs rewrites only the subcommand
// and address and passes every other flag (notably -peer) through untouched.
func TestServeArgsKeepsPeerFlags(t *testing.T) {
	got := serveArgs([]string{"wllr", "serve", "127.0.0.1:9000", "-peer", "ci=http://x"})
	want := []string{"wllr", "--serve", "127.0.0.1:9000", "-peer", "ci=http://x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("serveArgs = %v, want %v", got, want)
	}

	// Without an explicit address the default is inserted and -peer still carries.
	got = serveArgs([]string{"wllr", "serve", "-peer", "ci=http://x"})
	want = []string{"wllr", "--serve", defaultServeAddr, "-peer", "ci=http://x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("serveArgs = %v, want %v", got, want)
	}
}
