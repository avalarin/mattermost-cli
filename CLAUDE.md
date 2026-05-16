# mattermost-cli

Terminal TUI client for Mattermost with built-in Claude AI agent. Go 1.23, module `github.com/avalarin/mattermost-cli`.

## Build, test, lint

```bash
just check   # build + vet + test + lint (run this after every change)
just build   # go build ./...
just test    # go test ./...
just lint    # golangci-lint run
just vet     # go vet ./...
just run --config config.dev.toml   # run the app
```

## After every change — all of these must be green

Run `just check` — it runs build, vet, test, and lint in order. Never leave a change in a state where `just check` fails.

## Running the app

```bash
./mattermost-cli --config config.dev.toml
./mattermost-cli --config config.dev.toml --debug
```

For local development use `config.dev.toml` in the project root (gitignored). Copy `config.example.toml` to get started:

```bash
cp config.example.toml config.dev.toml
# fill in server.url, server.token, server.team
```

Config path defaults to `~/.config/mattermost-cli/config.toml` in production. `--debug` writes structured logs to `debug.log`.

## Project structure

```
cmd/mattermost-cli/main.go   # entry point, CLI flags
internal/config/             # TOML + env loading
internal/mattermost/         # REST client, WS client, types
internal/store/              # SQLite (db.go) + in-memory state (store.go)
internal/tui/                # Bubble Tea model, views, keys, styles
```

## Key conventions

- Each task milestone leaves the app runnable and manually testable — no dead-end stubs that prevent `go build`
- All network calls are non-blocking — use `tea.Cmd` for async operations in TUI
- `ModeNormal` vs `ModeCommand` — TUI has two input modes; navigation keys are disabled in `ModeCommand`
- Exit via `/quit` command. `Ctrl+C` clears the input field; if field is empty, shows "To exit, use /quit" in the status bar
- Commands start with `/`: `/send #channel text`, `/send @user text`, `/quit`
- `/send #channel` looks up by channel slug (`channel.Name` exact match)
- `/send @user` finds or creates a DM channel via `FindOrCreateDM`
- SQLite DSN in production: `file:path?_journal=WAL`; in tests: `file::memory:?cache=shared`
- WS reconnect backoff: `min(base * 2^attempt, 60s)` × jitter [0.8, 1.2]

## Test conventions

- HTTP API tests use `net/http/httptest` mock servers — no real MM server required
- SQLite tests use `:memory:` DSN
- WS tests use a local WebSocket test server
- No mocking of internal packages — only mock at external boundaries (HTTP, WS, SQLite)

## Workflow

Implement **one task at a time**. After completing each task:
1. Run `just check` — all checks must be green
2. Mark the task done in the spec (`### ✅ Tx: ...`)
3. Write a short summary to the user: what was done and what to verify manually

Do not start the next task until the user confirms.

## Specs and task tracking

`docs/` contains specs for all milestones:

- `docs/spec.md` — общая спецификация проекта
- `docs/m1/spec.md` — M1: базовая лента, авторизация, отправка

### Marking tasks done

When a task (T1, T2, …) is fully implemented and all its checks pass, mark it in the corresponding `spec.md` by adding ✅ to the task heading:

```markdown
### ✅ T1: Project scaffolding + app skeleton
```

### Task decision log

While implementing a task, create `docs/mX/tX.md` with a record of the technical decisions made. The file should cover:

- **What was done** — scope implemented, files created/modified
- **Key decisions** — non-obvious choices, trade-offs, alternatives considered
- **Deviations from spec** — anything that differs from `spec.md` and why
- **Known limitations** — shortcuts taken, what is intentionally out of scope

Example: `docs/m1/t1.md` for task T1 of milestone M1.
