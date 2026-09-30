// Package config resolves CLI settings and on-disk locations.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DefaultClientID is the public "Microsoft Graph Command Line Tools"
	// application, which supports delegated Graph permissions and a
	// http://localhost redirect URI for interactive browser sign-in.
	DefaultClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"
	// DefaultTenant accepts any work or school account.
	DefaultTenant = "organizations"
	// DefaultGraphURL is the Microsoft Graph v1.0 endpoint.
	DefaultGraphURL = "https://graph.microsoft.com/v1.0"

	EnvConfigDir   = "TEAMS_CLI_CONFIG_DIR"
	EnvClientID    = "TEAMS_CLI_CLIENT_ID"
	EnvTenant      = "TEAMS_CLI_TENANT_ID"
	EnvAccessToken = "TEAMS_CLI_ACCESS_TOKEN"
	EnvGraphURL    = "TEAMS_CLI_GRAPH_URL"

	settingsFile = "config.json"
	cacheFile    = "msal_cache.json"
)

// Settings are persisted at login so later commands reuse the same app
// registration and tenant.
type Settings struct {
	ClientID string `json:"clientId,omitempty"`
	Tenant   string `json:"tenant,omitempty"`
}

// Dir returns the directory used to store CLI state.
func Dir() (string, error) {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config directory: %w", err)
	}
	return filepath.Join(base, "teams-cli"), nil
}

// CachePath returns the path of the token cache file.
func CachePath(dir string) string { return filepath.Join(dir, cacheFile) }

// Load reads saved settings from dir. A missing file yields empty settings.
func Load(dir string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("invalid %s: %w", settingsFile, err)
	}
	return s, nil
}

// Save writes settings to dir.
func Save(dir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(dir, settingsFile), b)
}

// Remove deletes all persisted state (settings and token cache).
func Remove(dir string) error {
	for _, name := range []string{settingsFile, cacheFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Resolve picks the effective value using precedence flag > env > saved > default.
func Resolve(flag, envKey, saved, def string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if saved != "" {
		return saved
	}
	return def
}

// WriteFileAtomic writes data to path with owner-only permissions, creating
// the parent directory if needed.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
