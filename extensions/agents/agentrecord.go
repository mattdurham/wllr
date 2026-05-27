package main

// agentRecord tracks a running sub-agent's status for the /agents modal.
type agentRecord struct {
	id         string
	name       string
	task       string
	lastUpdate string
}
