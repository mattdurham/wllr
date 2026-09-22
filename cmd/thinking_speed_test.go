package main

import (
	"testing"

	fantasyopenrouterprovider "charm.land/fantasy/providers/openrouter"
)

func TestOpenRouterSpeedSortMapping(t *testing.T) {
	withConfigPath(t)
	for _, tc := range []struct {
		id   string
		want string
	}{
		{"nitro", "throughput"},
		{"floor", "price"},
		{"latency", "latency"},
		{"throughput", "throughput"},
		{"price", "price"},
		{"", ""},
		{"default", ""},
	} {
		if err := saveOpenRouterSpeed(tc.id); err != nil {
			t.Fatalf("saveOpenRouterSpeed(%q): %v", tc.id, err)
		}
		po := openRouterProviderOptions()
		if tc.want == "" {
			if po != nil {
				t.Errorf("%q: want no routing sent, got %v", tc.id, po)
			}
			continue
		}
		opts, ok := po[fantasyopenrouterprovider.Name].(*fantasyopenrouterprovider.ProviderOptions)
		if !ok || opts.Provider == nil || opts.Provider.Sort == nil {
			t.Fatalf("%q: routing not set: %#v", tc.id, po)
		}
		if *opts.Provider.Sort != tc.want {
			t.Errorf("%q: sort = %q, want %q", tc.id, *opts.Provider.Sort, tc.want)
		}
	}
}

func TestOpenRouterSpeedAppliesAndPersistsIndependently(t *testing.T) {
	withConfigPath(t)
	if err := saveOpenRouterSpeed("nitro"); err != nil {
		t.Fatal(err)
	}
	// Routing is applied for OpenRouter and reflected in the status source.
	if got := speedDisplayFor(providerOpenRouter); got != "nitro" {
		t.Errorf("speedDisplayFor(openrouter) = %q, want nitro", got)
	}
	// A reasoning level set afterwards must not drop the routing preference:
	// both are persisted in separate config fields.
	po := providerOptionsForRuntime(providerOpenRouter, "high")
	if _, ok := po[fantasyopenrouterprovider.Name]; !ok {
		t.Fatalf("routing lost: %#v", po)
	}
	if got := savedOpenRouterSpeed(); got != "nitro" {
		t.Errorf("routing preference = %q, want nitro", got)
	}
}

func TestOpenRouterSpeedIgnoredForOtherProviders(t *testing.T) {
	withConfigPath(t)
	if err := saveOpenRouterSpeed("nitro"); err != nil {
		t.Fatal(err)
	}
	if got := speedDisplayFor(providerAnthropic); got != "" {
		t.Errorf("anthropic display = %q, want empty", got)
	}
	if got := speedDisplayFor(providerLocal); got != "" {
		t.Errorf("local display = %q, want empty", got)
	}
	// Anthropic options carry no OpenRouter routing entry.
	po := providerOptionsForRuntime(providerAnthropic, "high")
	if _, ok := po[fantasyopenrouterprovider.Name]; ok {
		t.Error("routing applied to anthropic; must be OpenRouter-only")
	}
}

func TestOpenRouterSpeedDefaultClearsConfig(t *testing.T) {
	withConfigPath(t)
	if err := saveOpenRouterSpeed("nitro"); err != nil {
		t.Fatal(err)
	}
	if got := savedWllrField("openrouter_speed"); got != "nitro" {
		t.Fatalf("stored = %q, want nitro", got)
	}
	if err := saveOpenRouterSpeed(""); err != nil {
		t.Fatal(err)
	}
	if got := savedWllrField("openrouter_speed"); got != "" {
		t.Errorf("after default, stored = %q, want removed", got)
	}
	if po := openRouterProviderOptions(); po != nil {
		t.Errorf("after default, options = %v, want nil", po)
	}
}

func TestOpenRouterSpeedRejectsUnknownOption(t *testing.T) {
	withConfigPath(t)
	if isValidOpenRouterSpeed("turbo") {
		t.Error("unknown option should be invalid")
	}
	if err := saveOpenRouterSpeed("turbo"); err != nil {
		t.Fatal(err)
	}
	// A persisted unknown value must not produce a sort (fail closed).
	if po := openRouterProviderOptions(); po != nil {
		t.Errorf("unknown option produced options: %v", po)
	}
}
