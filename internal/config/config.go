package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

var ErrMissingRequiredField = errors.New("missing required field")

type Config struct {
	Server ServerConfig
	AI     AIConfig
	UI     UIConfig
}

type ServerConfig struct {
	URL   string `toml:"url"`
	Token string `toml:"token"`
	Team  string `toml:"team"`
}

type AIConfig struct {
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
	Enabled bool   `toml:"enabled"`
}

type UIConfig struct {
	DateFormat    string `toml:"date_format"`
	MessageLimit  int    `toml:"message_limit"`
	Theme         string `toml:"theme"`
	ChannelsWidth int    `toml:"channels_width"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{}
	applyDefaults(cfg)

	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnv(cfg)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	cfg.UI.DateFormat = "15:04"
	cfg.UI.MessageLimit = 100
	cfg.UI.Theme = "auto"
	cfg.UI.ChannelsWidth = 22
	cfg.AI.Model = "claude-sonnet-4-6"
	cfg.AI.Enabled = false
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("MATTERMOST_URL"); v != "" {
		cfg.Server.URL = v
	}
	if v := os.Getenv("MATTERMOST_TOKEN"); v != "" {
		cfg.Server.Token = v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.AI.APIKey = v
	}
}

func validate(cfg *Config) error {
	if cfg.Server.URL == "" {
		return fmt.Errorf("%w: server.url", ErrMissingRequiredField)
	}
	if cfg.Server.Token == "" {
		return fmt.Errorf("%w: server.token", ErrMissingRequiredField)
	}
	return nil
}
