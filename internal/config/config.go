package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultHubURL = "https://morphso.toji.homes"

type Config struct {
	HubURL string `yaml:"hub_url"`
	Token  string `yaml:"token"`
	dir    string
}

// Load reads dir/config.yaml. Returns a Config with defaults if the file doesn't exist.
func Load(dir string) (*Config, error) {
	cfg := &Config{
		HubURL: defaultHubURL,
		dir:    dir,
	}
	path := cfg.Path()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.dir = dir
	if cfg.HubURL == "" {
		cfg.HubURL = defaultHubURL
	}
	return cfg, nil
}

// DefaultLoad loads from ~/.morphso/config.yaml.
func DefaultLoad() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return Load(filepath.Join(home, ".morphso"))
}

func (c *Config) Path() string {
	return filepath.Join(c.dir, "config.yaml")
}

func (c *Config) Save() error {
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(c.Path(), data, 0600)
}
