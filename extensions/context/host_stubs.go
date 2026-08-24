//go:build !wasip1

package main

import "encoding/json"

func ConfigRead() json.RawMessage            { return nil }
func ConfigReadGroup(string) json.RawMessage { return nil }
func Log(int, string)                        {}
func Logf(int, string, ...any)               {}
