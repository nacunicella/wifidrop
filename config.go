package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
)

// Config holds the persistent identity of this device. It is stored in the
// user's config directory so that the device ID and PIN survive restarts and
// phones paired once stay paired.
type Config struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	PIN  string `json:"pin"`
	// Installed records that the launcher/shortcuts were created once, so
	// later runs do not repeat it.
	Installed bool `json:"installed"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wifidrop", "config.json"), nil
}

func loadOrCreateConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("config %s is corrupt: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// First run: generate identity below.
	default:
		return nil, err
	}

	changed := false
	if cfg.ID == "" {
		cfg.ID = randomHex(6)
		changed = true
	}
	if cfg.Name == "" {
		cfg.Name = defaultDeviceName()
		changed = true
	}
	if len(cfg.PIN) != 6 {
		cfg.PIN = randomPIN()
		changed = true
	}
	if changed {
		if err := cfg.save(path); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func (c *Config) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func defaultDeviceName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "Equipo " + runtime.GOOS
	}
	return host
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func randomPIN() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}
