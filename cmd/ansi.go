package main

import "github.com/charmbracelet/x/ansi"

func stripAnsi(s string) string {
	return ansi.Strip(s)
}
