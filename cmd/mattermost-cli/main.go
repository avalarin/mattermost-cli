package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avalarin/mattermost-cli/internal/config"
	"github.com/avalarin/mattermost-cli/internal/mattermost"
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

	header, status := loadStartupState(resolvedConfig)

	m := tui.NewModelWithHeader(header, status)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// loadStartupState loads config and authenticates with the Mattermost server.
// On hard failures (invalid config fields, auth error) it prints to stderr and exits.
// A missing config file is not a hard failure — the TUI can show a message instead.
func loadStartupState(path string) (tui.HeaderInfo, string) {
	header := tui.HeaderInfo{Status: tui.ConnStatusConnecting}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return header, "Config file not found. Run with --config path/to/config.toml"
	}

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config error:", err)
		os.Exit(1)
	}

	client := mattermost.NewClient(cfg.Server.URL, cfg.Server.Token)

	user, err := client.GetCurrentUser()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Authentication failed:", err)
		os.Exit(1)
	}
	header.Username = user.Username

	if cfg.Server.Team != "" {
		team, err := client.GetTeamByName(cfg.Server.Team)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to get team:", err)
			os.Exit(1)
		}
		header.TeamName = team.Name
	}

	return header, "Config loaded: server=" + cfg.Server.URL
}
