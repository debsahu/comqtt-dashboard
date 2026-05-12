package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// addonConfig is the dashboard slice of configuration. The same YAML config
// file the broker reads is also parsed for a top-level `dashboard:` section.
// Defaults match the embedded dashboard package's expectations.
type addonConfig struct {
	Enabled            bool   `yaml:"enabled"`
	SessionSecret      string `yaml:"session-secret"`
	PasswordExpiryDays int    `yaml:"password-expiry-days"`
}

func defaultAddonConfig() addonConfig {
	return addonConfig{Enabled: true}
}

// loadAddonConfig parses the `dashboard:` section out of a config file. Returns
// defaults if path is empty or the file lacks the section.
func loadAddonConfig(path string) (addonConfig, error) {
	cfg := defaultAddonConfig()
	if path == "" {
		return cfg, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	wrap := struct {
		Dashboard *addonConfig `yaml:"dashboard"`
	}{}
	if err := yaml.Unmarshal(raw, &wrap); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if wrap.Dashboard != nil {
		cfg = *wrap.Dashboard
	}
	return cfg, nil
}

// decodeSecret returns the SessionSecret as raw bytes. Accepts either a
// base64-encoded value or a raw string. Returns nil when empty (broker then
// auto-generates and persists one).
func (c addonConfig) decodeSecret() []byte {
	if c.SessionSecret == "" {
		return nil
	}
	if b, err := base64.StdEncoding.DecodeString(c.SessionSecret); err == nil && len(b) >= 16 {
		return b
	}
	return []byte(c.SessionSecret)
}
