package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
	headless := flag.Bool("headless", false, "headless mode: log WS events to stdout without TUI")
	flag.Parse()

	resolvedConfig := resolveConfigPath(*configPath)

	// Allow debug = true in the config file (useful for dev configs).
	if !*debug {
		if cfg, err := config.Load(resolvedConfig); err == nil && cfg.Debug {
			*debug = true
		}
	}

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
				if err := st.PruneMessages(2000); err != nil {
					slog.Warn("failed to prune old messages", "err", err)
				}
			}
		}
	}

	if *headless {
		runHeadless(resolvedConfig, st)
		return
	}

	header, status, wsClient, channels, restClient, teamID, channelsWidth, showModeIndicator, activeHeaderFg, activeHeaderBg, fullDateFormat, channelMessages, threadPopupWidthPct, cfg := loadStartupState(resolvedConfig)
	if wsClient != nil {
		wsClient.Start(ctx)
	}

	var eventsCh <-chan mattermost.Event
	var statusCh <-chan mattermost.ConnStatus
	if wsClient != nil {
		eventsCh = wsClient.Events()
		statusCh = wsClient.Status()
	}

	m := tui.NewModelWithHeader(header, status, eventsCh, statusCh, channels, st, restClient, teamID, channelsWidth, showModeIndicator, activeHeaderFg, activeHeaderBg, fullDateFormat, channelMessages, threadPopupWidthPct, cfg.Channels.Sort, cfg.Channels.UnreadOnly)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// runHeadless connects to Mattermost and logs WebSocket events to stdout.
// It also prints the DB state at startup to help diagnose message caching issues.
func runHeadless(configPath string, st *store.Store) {
	if st != nil {
		count, oldest, newest, err := st.MessageStats()
		if err != nil {
			hlLog("DB", "error getting stats: %v", err)
		} else if count > 0 {
			hlLog("DB", "%d messages stored, oldest=%s, newest=%s",
				count, oldest.Format("2006-01-02"), newest.Format("2006-01-02"))
		} else {
			hlLog("DB", "no messages stored")
		}

		msgs, err := st.LoadRecent(100)
		if err != nil {
			hlLog("DB", "LoadRecent(100) error: %v", err)
		} else {
			hlLog("DB", "LoadRecent(100) returned %d messages (this is what All Activity shows on startup)", len(msgs))
			if len(msgs) > 0 {
				first, last := msgs[0], msgs[len(msgs)-1]
				hlLog("DB", "  first: %s #%s @%s: %q",
					time.UnixMilli(first.CreateAt).Format("2006-01-02"),
					first.ChannelName, first.SenderName, hlShort(first.Text))
				hlLog("DB", "  last:  %s #%s @%s: %q",
					time.UnixMilli(last.CreateAt).Format("2006-01-02"),
					last.ChannelName, last.SenderName, hlShort(last.Text))
			}
		}
	} else {
		hlLog("DB", "no database configured")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	client := mattermost.NewClientWithTimeout(cfg.Server.URL, cfg.Server.Token, 15*time.Second)
	user, err := client.GetCurrentUser()
	if err != nil {
		fmt.Fprintln(os.Stderr, "auth failed:", err)
		os.Exit(1)
	}
	hlLog("AUTH", "connected as @%s", user.Username)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	wsClient := mattermost.NewWSClient(cfg.Server.URL, cfg.Server.Token)
	wsClient.Start(ctx)
	hlLog("WS", "connecting to %s...", cfg.Server.URL)

	for {
		select {
		case <-ctx.Done():
			hlLog("WS", "shutting down")
			return
		case s, ok := <-wsClient.Status():
			if !ok {
				return
			}
			hlLog("WS", "%s", s)
		case evt, ok := <-wsClient.Events():
			if !ok {
				return
			}
			if evt.Type == "" {
				continue
			}
			if evt.Type == mattermost.EventTypePosted {
				postJSON, _ := evt.Data["post"].(string)
				var post mattermost.Message
				if jsonErr := json.Unmarshal([]byte(postJSON), &post); jsonErr != nil {
					hlLog("EVENT", "posted (parse error: %v) raw=%q", jsonErr, postJSON)
					continue
				}
				sender, _ := evt.Data["sender_name"].(string)
				channel, _ := evt.Data["channel_display_name"].(string)
				if channel == "" {
					channel = post.ChannelID
				}
				hlLog("EVENT", "posted #%s %s: %q", channel, sender, hlShort(post.Text))
				continue
			}
			hlLog("EVENT", "%s", evt.Type)
		}
	}
}

func hlLog(component, format string, args ...any) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %-8s %s\n", ts, component, fmt.Sprintf(format, args...))
}

func hlShort(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "..."
	}
	return s
}

// loadStartupState loads config and authenticates with the Mattermost server.
// On hard failures (invalid config fields, auth error) it prints to stderr and exits.
// A missing config file is not a hard failure — the TUI can show a message instead.
func loadStartupState(path string) (tui.HeaderInfo, string, *mattermost.WSClient, []mattermost.Channel, *mattermost.Client, string, int, bool, string, string, string, string, int, *config.Config) {
	header := tui.HeaderInfo{Status: mattermost.ConnStatusConnecting}
	defaultCfg := &config.Config{}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return header, "Config file not found. Run with --config path/to/config.toml", nil, nil, nil, "", 22, true, "15", "237", "02.01.2006", "root_only", 70, defaultCfg
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
	return header, "", wsClient, channels, client, teamID, cfg.UI.ChannelsWidth, cfg.UI.ShowModeIndicator, cfg.Colors.ActiveHeaderFg, cfg.Colors.ActiveHeaderBg, cfg.UI.FullDateFormat, cfg.UI.ChannelMessages, cfg.UI.ThreadPopupWidthPct, cfg
}
