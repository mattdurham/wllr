package main

// Config is loaded from the host via config_read.
type Config struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Token    string `json:"token"`
}
