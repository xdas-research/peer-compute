// Package identity - configuration constants
package identity

import (
	"os"
	"path/filepath"
)

const (
	// ConfigDirName is the name of the configuration directory
	ConfigDirName = ".peercompute"
)

// DefaultConfigDir returns the default configuration directory path.
// On Linux/macOS: ~/.peercompute
// On Windows: %USERPROFILE%\.peercompute
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home dir is not available
		return ConfigDirName
	}
	return filepath.Join(home, ConfigDirName)
}

// DefaultKeyPath returns the default path for the identity key file.
func DefaultKeyPath() string {
	return filepath.Join(DefaultConfigDir(), KeyFileName)
}

// DefaultTrustedPeersPath returns the default path for the trusted peers file.
func DefaultTrustedPeersPath() string {
	return filepath.Join(DefaultConfigDir(), "trusted_peers.json")
}
