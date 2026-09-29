package main

import "encoding/json"

// configReadHost is the host-backed config read. Under wasip1, main.go wires
// this to the SDK's ConfigRead in an init hook; natively (tests) it stays nil
// and configReadOverride drives loadAgentsConfig instead.
var configReadHost func() (json.RawMessage, error)
