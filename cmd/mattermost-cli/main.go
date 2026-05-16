package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avalarin/mattermost-cli/internal/config"
	"github.com/avalarin/mattermost-cli/internal/tui"
)

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: cannot determine home directory:", err)
		home = ""
	}
	return filepath.Join(home, ".config", "mattermost-cli", "config.toml")
}

func resolveConfigPath(configFlag string) string {
	if configFlag != "" {
		return configFlag
	}
	return defaultConfigPath()
}

func loadConfigStatus(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "Config file not found. Run with --config path/to/config.toml"
	}
	cfg, err := config.Load(path)
	if err != nil {
		return "Config error: " + err.Error()
	}
	return "Config loaded: server=" + cfg.Server.URL
}

func main() {
	configPath := flag.String("config", "", "path to config file")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	resolvedConfig := resolveConfigPath(*configPath)

	if *debug {
		f, err := os.OpenFile("debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning: cannot open debug.log:", err)
		} else {
			handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
			slog.SetDefault(slog.New(handler))
		}
	}

	status := loadConfigStatus(resolvedConfig)

	m := tui.NewModelWithStatus(status)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
