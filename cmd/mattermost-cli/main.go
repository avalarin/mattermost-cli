package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avalarin/mattermost-cli/internal/config"
	"github.com/avalarin/mattermost-cli/internal/mattermost"
	"github.com/avalarin/mattermost-cli/internal/store"
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

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mattermost-cli", "db.sqlite")
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPath := defaultDBPath()
	var st *store.Store
	if dbPath != "" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
			slog.Warn("cannot create config directory", "err", err)
		} else {
			db, err := store.Open(dbPath)
			if err != nil {
				slog.Warn("failed to open database, running without persistence", "err", err)
			} else {
				defer func() {
					if err := db.Close(); err != nil {
						slog.Warn("failed to close database", "err", err)
					}
				}()
				st = store.NewStore(db)
			}
		}
	}

	header, status, wsClient, channels, restClient, teamID, channelsWidth, showModeIndicator, activeHeaderFg, activeHeaderBg, fullDateFormat, channelMessages, threadPopupWidthPct := loadStartupState(resolvedConfig)
	if wsClient != nil {
		wsClient.Start(ctx)
	}

	var eventsCh <-chan mattermost.Event
	var statusCh <-chan mattermost.ConnStatus
	if wsClient != nil {
		eventsCh = wsClient.Events()
		statusCh = wsClient.Status()
	}

	m := tui.NewModelWithHeader(header, status, eventsCh, statusCh, channels, st, restClient, teamID, channelsWidth, showModeIndicator, activeHeaderFg, activeHeaderBg, fullDateFormat, channelMessages, threadPopupWidthPct)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// loadStartupState loads config and authenticates with the Mattermost server.
// On hard failures (invalid config fields, auth error) it prints to stderr and exits.
// A missing config file is not a hard failure — the TUI can show a message instead.
// Returns the header info, status message, WS client, channels, REST client, team ID,
// channels sidebar width, whether the mode indicator should be shown, active header fg/bg colors,
// the full date format for messages not from today, the channel_messages setting, and thread popup width pct.
func loadStartupState(path string) (tui.HeaderInfo, string, *mattermost.WSClient, []mattermost.Channel, *mattermost.Client, string, int, bool, string, string, string, string, int) {
	header := tui.HeaderInfo{Status: mattermost.ConnStatusConnecting}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return header, "Config file not found. Run with --config path/to/config.toml", nil, nil, nil, "", 22, true, "15", "237", "02.01.2006", "root_only", 70
	}

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config error:", err)
		os.Exit(1)
	}

	client := mattermost.NewClientWithTimeout(cfg.Server.URL, cfg.Server.Token, 15*time.Second)

	user, err := client.GetCurrentUser()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Authentication failed:", err)
		os.Exit(1)
	}
	header.Username = user.Username

	var channels []mattermost.Channel
	teamID := ""
	if cfg.Server.Team != "" {
		team, err := client.GetTeamByName(cfg.Server.Team)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to get team:", err)
			os.Exit(1)
		}
		header.TeamName = team.Name
		teamID = team.ID

		channels, err = client.GetChannelsForTeam(team.ID)
		if err != nil {
			slog.Debug("failed to load channels", "err", err)
		}
	}

	wsClient := mattermost.NewWSClient(cfg.Server.URL, cfg.Server.Token)
	return header, "", wsClient, channels, client, teamID, cfg.UI.ChannelsWidth, cfg.UI.ShowModeIndicator, cfg.Colors.ActiveHeaderFg, cfg.Colors.ActiveHeaderBg, cfg.UI.FullDateFormat, cfg.UI.ChannelMessages, cfg.UI.ThreadPopupWidthPct
}
