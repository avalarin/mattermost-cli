# Code Reviewer Memory

Recurring patterns and project-specific pitfalls to check on every review.

## Patterns

- **[config] go.mod vs CI Go version mismatch** — The `go` directive in `go.mod` and the `go-version` in `.github/workflows/ci.yml` must stay in sync. They diverged in the initial commit (go.mod says 1.24.2, CI says 1.23). Check both whenever either file changes.

- **[ci] golangci-lint pinned to "latest"** — `golangci/golangci-lint-action` is used with `version: latest`, which can cause non-deterministic CI failures when a new linter version ships. Pin to a specific semver.

- **[dead-code] Defined but unused abstractions** — `KeyMap` in `keys.go` and `FeedView` in `views/feed.go` were created but not wired into `model.go`. Watch for types/structs created speculatively that are never referenced by any caller.

- **[code-smell] Duplicate if-else branches returning the same value** — In `loadConfigStatus`, both branches of an `errors.Is` check return identical strings. Collapsed dead branches mask intent.

- **[code-smell] Styles struct bypassed by inline style construction** — `DefaultStyles()` defines canonical styles, but render methods in `model.go` construct fresh `lipgloss.NewStyle()` calls inline, defeating the purpose of the styles abstraction.

- **[dead-code] Superseded constructors left in place** — When a new constructor is added (e.g., `NewModelWithHeader` replacing `NewModelWithStatus`), the old one is not removed. Check that every exported constructor has at least one caller after a refactor.

- **[networking] Synchronous startup network calls without timeout** — `loadStartupState` (and any future init code) calls HTTP endpoints before the TUI starts, with no timeout on `http.Client`. A hung server freezes the process silently. Always set a startup-specific timeout or move auth into a `tea.Cmd`.

- **[tui] Viewport mutation before `ready` guard** — In Bubble Tea models, `viewport.New` is called lazily on the first `WindowSizeMsg`. Any code path (e.g., incoming WS events) that calls `viewport.SetContent` or `viewport.GotoBottom` before that first message operates on a zero-value viewport; worse, when `handleWindowSize` fires it overwrites the content. Always guard viewport writes with `m.ready`, and after `viewport.New` re-render any buffered content instead of using a hard-coded placeholder.

- **[channels] Non-blocking send with `default:` for priority status signals** — Using `select { case ch <- v: default: }` on a status channel that also receives lower-priority ticks can silently drop a high-priority signal (e.g., `ConnStatusConnected`) if the buffer is saturated with ticks. Reserve `default:` drops for genuinely disposable messages; for signals that must be visible, drain the buffer first or use a blocking send (which is safe when the consumer is a bubbletea goroutine that always drains promptly).
