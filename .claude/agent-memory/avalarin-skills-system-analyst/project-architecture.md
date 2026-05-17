---
name: project-architecture
description: Package layout, TUI model structure, task completion status, and key types for mattermost-cli
metadata:
  type: project
---

## Module
`github.com/avalarin/mattermost-cli`, Go 1.24.2

## Key source files
- `cmd/mattermost-cli/main.go` — entry point; loads config, authenticates, creates TUI model
- `internal/config/config.go` — TOML + env loading, Config/ServerConfig/AIConfig/UIConfig structs
- `internal/mattermost/types.go` — Team, Channel, User, Message, Event types
- `internal/mattermost/client.go` — REST client with GetCurrentUser, GetTeamByName, GetChannelsForTeam, GetChannelByName, FindOrCreateDM, SendMessage
- `internal/mattermost/websocket.go` — WSClient stub (url/token only, no real connection yet)
- `internal/store/db.go` — DB struct stub (path only, no real SQLite)
- `internal/store/store.go` — Store stub (holds *DB only)
- `internal/tui/model.go` — root Bubble Tea Model (real, functional)
- `internal/tui/keys.go` — KeyMap with DefaultKeyMap()
- `internal/tui/styles.go` — Styles with DefaultStyles()
- `internal/tui/views/feed.go` — FeedView wrapping bubbles/viewport

## Task completion (M1 spec at docs/m1/spec.md)
- T1 scaffold: DONE
- T2 GitHub CI: DONE
- T3 config: DONE
- T4 REST client + types: DONE
- T5 TUI feed + header + /quit: DONE
- T6 WebSocket + live feed: DONE
- T7 SQLite + store: NOT DONE (next task)
- T8 /send command: NOT DONE
- T9 keyboard navigation: NOT DONE

## TUI Model state (post-T6, pre-T7)
- `Model` struct fields: width, height, mode, header (HeaderInfo), input, viewport, statusMsg, keys, styles, ready, events (<-chan mattermost.Event), connStatus (<-chan mattermost.ConnStatus), channels (map[string]string channelID->name), msgCache (map[string]string messageID->text), feedLines ([]string rendered lines), atBottom bool
- Init() returns tea.Batch(waitForEvent, waitForStatus) — two long-poll goroutine-as-cmd loops
- Update() handles: WindowSizeMsg, KeyMsg, mattermost.Event (→ handlePostedEvent), MsgConnStatus
- handlePostedEvent: JSON-decodes post from evt.Data["post"], resolves senderName+channelName, caches in msgCache, appends to feedLines, calls viewport.SetContent(join(feedLines))
- renderMessageLine: formats `[HH:MM] #channel  sender: text` or thread reply with ↩ prefix and 40-char parent snippet from msgCache
- executeCommand() handles /quit only (T8 will add /send)

## Stubs vs Real (post-T6)
- REAL: config, REST client, TUI model (full with WS event loop and feed rendering), WSClient (reconnect + backoff)
- STUB: store/db.go (DB struct with path field only, no actual SQLite), store/store.go (Store struct with *DB only, no message state)
- NOT YET: SQLite schema, store integration into TUI, /send command, T9 KeyMap refactor

## WSClient (internal/mattermost/websocket.go)
- Fields: url, token, events chan Event, status chan ConnStatus
- Start(ctx): launches goroutine with reconnect loop; exponential backoff min(base*2^attempt, 60s) × jitter[0.8,1.2]
- Events() <-chan Event; Status() <-chan ConnStatus
- WS auth challenge frame sent on connect; reads JSON frames → emits to events chan

## Config fields
- Server: URL, Token, Team (toml: url/token/team; env: MATTERMOST_URL, MATTERMOST_TOKEN)
- AI: APIKey, Model (default claude-sonnet-4-6), Enabled (default false); env: ANTHROPIC_API_KEY
- UI: DateFormat (default 15:04), MessageLimit (default 100), Theme (default auto)

## Dependencies (go.mod)
- github.com/BurntSushi/toml v1.6.0
- github.com/charmbracelet/bubbles v1.0.0
- github.com/charmbracelet/bubbletea v1.3.10
- github.com/charmbracelet/lipgloss v1.1.0
- github.com/coder/websocket v1.8.14
- No SQLite driver yet in go.mod (store/db.go is still a stub with path field only)

## Test coverage
- config: full unit tests
- mattermost/client: httptest mock server tests for all REST methods
- mattermost/websocket: WS test server tests (connect, posted event, reconnect, backoff)
- tui/model: behavioural tests + renderMessageLine tests (reply, no-parent, normal)
- cmd/main: 2 path resolution tests
- store: no tests yet
