package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PermissionsConfig defines file system access control rules for extensions.
// It supports allow and deny lists for both read and write operations.
type PermissionsConfig struct {
	// Enabled controls whether permission checks are enforced.
	// When false, all operations are allowed (backward compatible).
	Enabled bool `json:"enabled"`

	// ReadAllow is a list of directory paths extensions are allowed to read from.
	// Paths are matched as prefixes after normalization.
	// Empty list means allow all (when no ReadDeny is set).
	ReadAllow []string `json:"read_allow,omitempty"`

	// ReadDeny is a list of directory paths extensions are denied from reading.
	// Deny rules take precedence over allow rules.
	ReadDeny []string `json:"read_deny,omitempty"`

	// WriteAllow is a list of directory paths extensions are allowed to write to.
	// Paths are matched as prefixes after normalization.
	// Empty list means allow all (when no WriteDeny is set).
	WriteAllow []string `json:"write_allow,omitempty"`

	// WriteDeny is a list of directory paths extensions are denied from writing to.
	// Deny rules take precedence over allow rules.
	WriteDeny []string `json:"write_deny,omitempty"`
}

// PermissionChecker validates file system access for extensions.
type PermissionChecker struct {
	config PermissionsConfig
	// Normalized and cleaned paths for fast matching
	readAllow  []string
	readDeny   []string
	writeAllow []string
	writeDeny  []string
}

// NewPermissionChecker creates a new checker from the given config.
func NewPermissionChecker(config PermissionsConfig) *PermissionChecker {
	pc := &PermissionChecker{
		config: config,
	}

	// Normalize all paths for consistent matching
	pc.readAllow = normalizePaths(config.ReadAllow)
	pc.readDeny = normalizePaths(config.ReadDeny)
	pc.writeAllow = normalizePaths(config.WriteAllow)
	pc.writeDeny = normalizePaths(config.WriteDeny)

	return pc
}

// CanRead checks whether the given path can be read according to the config.
func (pc *PermissionChecker) CanRead(path string) error {
	if !pc.config.Enabled {
		return nil
	}

	normalized, err := normalizePath(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check deny list first
	if pc.matchesAny(normalized, pc.readDeny) {
		return fmt.Errorf("read denied: %s", path)
	}

	// If allow list is empty, allow all (unless denied above)
	if len(pc.readAllow) == 0 {
		return nil
	}

	// Check allow list
	if pc.matchesAny(normalized, pc.readAllow) {
		return nil
	}

	return fmt.Errorf("read not allowed: %s", path)
}

// CanWrite checks whether the given path can be written according to the config.
func (pc *PermissionChecker) CanWrite(path string) error {
	if !pc.config.Enabled {
		return nil
	}

	normalized, err := normalizePath(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check deny list first
	if pc.matchesAny(normalized, pc.writeDeny) {
		return fmt.Errorf("write denied: %s", path)
	}

	// If allow list is empty, allow all (unless denied above)
	if len(pc.writeAllow) == 0 {
		return nil
	}

	// Check allow list
	if pc.matchesAny(normalized, pc.writeAllow) {
		return nil
	}

	return fmt.Errorf("write not allowed: %s", path)
}

// matchesAny returns true if path matches any of the prefix patterns.
func (pc *PermissionChecker) matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.HasPrefix(path, pattern) {
			return true
		}
	}
	return false
}

// normalizePath converts a path to an absolute, cleaned path for consistent matching.
func normalizePath(path string) (string, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}

	// Make absolute if relative
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}

	// Clean the path (remove .., ., etc.)
	path = filepath.Clean(path)

	// Ensure trailing separator for directory patterns
	// This prevents /home/user matching /home/username
	if !strings.HasSuffix(path, string(filepath.Separator)) {
		// Check if it's a directory
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path += string(filepath.Separator)
		}
	}

	return path, nil
}

// normalizePaths normalizes a slice of paths.
func normalizePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if normalized, err := normalizePath(p); err == nil {
			result = append(result, normalized)
		}
		// Silently skip invalid paths during config loading
	}
	return result
}

// LoadPermissionsConfig loads permissions config from the config file.
// Returns a default (disabled) config if not found or on error.
func LoadPermissionsConfig(configReader func(string) (json.RawMessage, error)) PermissionsConfig {
	if configReader == nil {
		return PermissionsConfig{Enabled: false}
	}

	data, err := configReader("permissions")
	if err != nil || len(data) == 0 {
		return PermissionsConfig{Enabled: false}
	}

	var cfg PermissionsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return PermissionsConfig{Enabled: false}
	}

	return cfg
}
