---
name: go-websocket-coder
description: Use github.com/coder/websocket instead of nhooyr.io/websocket — the latter is deprecated and flagged by staticcheck SA1019
metadata:
  type: feedback
---

Use `github.com/coder/websocket` (not `nhooyr.io/websocket`). The nhooyr package was forked; coder/websocket is the maintained successor with an identical API.

**Why:** staticcheck SA1019 flags nhooyr.io/websocket as deprecated in every file that imports it, causing lint failures.

**How to apply:** Any time nhooyr.io/websocket appears in go.mod or source imports, replace with github.com/coder/websocket. The import paths for sub-packages (e.g. wsjson) are identical — just change the module prefix.

Note: `conn.CloseNow()` returns `error` — errcheck will fail on bare `defer conn.CloseNow()`. Use `defer func() { _ = conn.CloseNow() }()` or `_ = conn.CloseNow()`.
