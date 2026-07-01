package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

func defaultLoginProvider() string {
	if provider := os.Getenv("WLLR_PROVIDER"); provider != "" {
		return provider
	}
	return providerAnthropic
}

func defaultLoginModel() string {
	if model := os.Getenv("WLLR_MODEL"); model != "" {
		return model
	}
	if model := savedModel(); model != "" {
		return model
	}
	return "claude-sonnet-4-6"
}

func runLoginCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	provider := fs.String("provider", defaultLoginProvider(), "provider to authenticate (anthropic or openai)")
	model := fs.String("model", defaultLoginModel(), "model to use after login")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	state := newOAuthLoginState(ctx, nil, *model)
	body, _, err := state.begin(*provider)
	if err != nil {
		fmt.Fprintf(stderr, "wllr login: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, body)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Waiting for login to complete. Press Ctrl+C to cancel.")

	input, err := state.await()
	if err != nil {
		fmt.Fprintf(stderr, "wllr login: %v\n", err)
		return 1
	}
	if input == "" {
		fmt.Fprintln(stderr, "wllr login: login cancelled")
		return 1
	}
	if err := state.complete(*provider, input); err != nil {
		fmt.Fprintf(stderr, "wllr login: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Logged in to %s. Credentials saved to %s.\n", *provider, authPath())
	return 0
}
