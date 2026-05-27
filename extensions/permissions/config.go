package main

// Config holds the permission rules loaded from the extension config.
type Config struct {
	Read  PathRules `json:"read"`
	Write PathRules `json:"write"`
}
