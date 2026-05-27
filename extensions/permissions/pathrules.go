package main

// PathRules holds allow and deny lists for a permission type.
type PathRules struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}
