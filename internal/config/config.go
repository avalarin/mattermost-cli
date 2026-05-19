package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

var ErrMissingRequiredField = errors.New("missing required field")

type Config struct {
	Debug    bool `toml:"debug"`
	Server   ServerConfig
	AI       AIConfig
	UI       UIConfig
	Colors   ColorsConfig
	Channels ChannelsConfig
}

// ChannelsConfig holds channel list display preferences.
type ChannelsConfig struct {
	Sort       string `toml:"sort"`        // "alphabetical" | "last_message"; default "alphabetical"
	UnreadOnly bool   `toml:"unread_only"` // default false
}

// ColorsConfig holds terminal color overrides (256-color ANSI codes or hex RGB).
type ColorsConfig struct {
	// ActiveHeaderBg is the background color of the active panel header. Default "237" (dark gray).
	ActiveHeaderBg string `toml:"active_header_bg"`
	// ActiveHeaderFg is the foreground color of the active panel header. Default "15" (bright white).
	ActiveHeaderFg string `toml:"active_header_fg"`
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
	DateFormat           string `toml:"date_format"`
	FullDateFormat       string `toml:"full_date_format"`
	MessageLimit         int    `toml:"message_limit"`
	Theme                string `toml:"theme"`
	ChannelsWidth        int    `toml:"channels_width"`
	ShowModeIndicator    bool   `toml:"show_mode_indicator"`
	ChannelMessages      string `toml:"channel_messages"`        // "root_only" | "all"
	ThreadPopupWidthPct  int    `toml:"thread_popup_width_pct"`  // percent of terminal width, default 70
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
	cfg.UI.FullDateFormat = "02.01.2006"
	cfg.UI.MessageLimit = 100
	cfg.UI.Theme = "auto"
	cfg.UI.ChannelsWidth = 22
	cfg.UI.ShowModeIndicator = true
	cfg.UI.ChannelMessages = "root_only"
	cfg.UI.ThreadPopupWidthPct = 70
	cfg.Colors.ActiveHeaderBg = "237"
	cfg.Colors.ActiveHeaderFg = "15"
	cfg.AI.Model = "claude-sonnet-4-6"
	cfg.AI.Enabled = false
	cfg.Channels.Sort = "alphabetical"
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
