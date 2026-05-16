package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	result := resolveConfigPath("")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "mattermost-cli", "config.toml")
	if result != expected {
		t.Errorf("resolveConfigPath(\"\") = %q, want %q", result, expected)
	}
}

func TestCustomConfigPath(t *testing.T) {
	result := resolveConfigPath("/tmp/test.toml")
	if result != "/tmp/test.toml" {
		t.Errorf("resolveConfigPath(\"/tmp/test.toml\") = %q, want %q", result, "/tmp/test.toml")
	}
}
