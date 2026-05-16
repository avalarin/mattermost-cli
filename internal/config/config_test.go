package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

const validTOML = `
[server]
url   = "https://mm.example.com"
token = "tok123"
team  = "myteam"

[ai]
api_key = "sk-abc"
model   = "claude-opus-4-7"
enabled = true

[ui]
date_format   = "2006-01-02 15:04"
message_limit = 200
theme         = "dark"
`

func TestLoadValidConfig(t *testing.T) {
	path := writeTemp(t, validTOML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.URL != "https://mm.example.com" {
		t.Errorf("URL: got %q", cfg.Server.URL)
	}
	if cfg.Server.Token != "tok123" {
		t.Errorf("Token: got %q", cfg.Server.Token)
	}
	if cfg.Server.Team != "myteam" {
		t.Errorf("Team: got %q", cfg.Server.Team)
	}
	if cfg.AI.APIKey != "sk-abc" {
		t.Errorf("AI.APIKey: got %q", cfg.AI.APIKey)
	}
	if cfg.AI.Model != "claude-opus-4-7" {
		t.Errorf("AI.Model: got %q", cfg.AI.Model)
	}
	if !cfg.AI.Enabled {
		t.Error("AI.Enabled: expected true")
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("UI.Theme: got %q", cfg.UI.Theme)
	}
	if cfg.UI.MessageLimit != 200 {
		t.Errorf("UI.MessageLimit: got %d", cfg.UI.MessageLimit)
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	path := writeTemp(t, validTOML)
	t.Setenv("MATTERMOST_URL", "https://override.example.com")
	t.Setenv("MATTERMOST_TOKEN", "overridetoken")
	t.Setenv("ANTHROPIC_API_KEY", "sk-override")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.URL != "https://override.example.com" {
		t.Errorf("URL not overridden: got %q", cfg.Server.URL)
	}
	if cfg.Server.Token != "overridetoken" {
		t.Errorf("Token not overridden: got %q", cfg.Server.Token)
	}
	if cfg.AI.APIKey != "sk-override" {
		t.Errorf("APIKey not overridden: got %q", cfg.AI.APIKey)
	}
}

func TestMissingURLReturnsError(t *testing.T) {
	path := writeTemp(t, "[server]\ntoken = \"tok\"\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Errorf("expected ErrMissingRequiredField, got %v", err)
	}
}

func TestMissingTokenReturnsError(t *testing.T) {
	path := writeTemp(t, "[server]\nurl = \"https://mm.example.com\"\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Errorf("expected ErrMissingRequiredField, got %v", err)
	}
}

func TestDefaultValues(t *testing.T) {
	path := writeTemp(t, "[server]\nurl=\"https://mm.example.com\"\ntoken=\"tok\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UI.DateFormat != "15:04" {
		t.Errorf("DateFormat default: got %q", cfg.UI.DateFormat)
	}
	if cfg.UI.MessageLimit != 100 {
		t.Errorf("MessageLimit default: got %d", cfg.UI.MessageLimit)
	}
	if cfg.UI.Theme != "auto" {
		t.Errorf("Theme default: got %q", cfg.UI.Theme)
	}
	if cfg.AI.Model != "claude-sonnet-4-6" {
		t.Errorf("AI.Model default: got %q", cfg.AI.Model)
	}
	if cfg.AI.Enabled {
		t.Error("AI.Enabled default: expected false")
	}
}

func TestMissingConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.toml")
	_, err := Load(path)
	// Missing file is not fatal — validation will catch missing URL/Token
	if err == nil {
		t.Fatal("expected error (missing required fields), got nil")
	}
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Errorf("expected ErrMissingRequiredField, got %v", err)
	}
}
