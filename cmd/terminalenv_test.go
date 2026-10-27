package main

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestTerminalEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		environ     []string
		want        []string
		wantProfile colorprofile.Profile
	}{
		{
			name:        "restores color detection in Zellij without TERM",
			environ:     []string{"ZELLIJ=0"},
			want:        []string{"ZELLIJ=0", "TERM=xterm-256color"},
			wantProfile: colorprofile.ANSI256,
		},
		{
			name:        "replaces an empty TERM in Zellij",
			environ:     []string{"TERM=", "ZELLIJ=0"},
			want:        []string{"TERM=xterm-256color", "ZELLIJ=0"},
			wantProfile: colorprofile.ANSI256,
		},
		{
			name:        "preserves an explicit terminal and truecolor",
			environ:     []string{"ZELLIJ=0", "TERM=xterm-256color", "COLORTERM=truecolor"},
			want:        []string{"ZELLIJ=0", "TERM=xterm-256color", "COLORTERM=truecolor"},
			wantProfile: colorprofile.TrueColor,
		},
		{
			name:        "preserves an explicit dumb terminal",
			environ:     []string{"ZELLIJ=0", "TERM=dumb"},
			want:        []string{"ZELLIJ=0", "TERM=dumb"},
			wantProfile: colorprofile.NoTTY,
		},
		{
			name:        "continues to honor NO_COLOR",
			environ:     []string{"ZELLIJ=0", "NO_COLOR=1"},
			want:        []string{"ZELLIJ=0", "NO_COLOR=1", "TERM=xterm-256color"},
			wantProfile: colorprofile.ASCII,
		},
		{
			name:        "does not infer a terminal outside Zellij",
			environ:     []string{"PATH=/bin"},
			want:        []string{"PATH=/bin"},
			wantProfile: colorprofile.NoTTY,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.environ...)
			got := terminalEnvironment(tt.environ)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("terminalEnvironment() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.environ, original) {
				t.Fatalf("terminalEnvironment() mutated its input: got %v, want %v", tt.environ, original)
			}
			if profile := colorprofile.Env(got); profile != tt.wantProfile {
				t.Fatalf("color profile = %v, want %v", profile, tt.wantProfile)
			}
		})
	}
}
