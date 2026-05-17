---
name: golangci-lint-v2-config
description: golangci-lint v2 requires version field and different linters section structure — v1 config causes exit code 3
metadata:
  type: feedback
---

golangci-lint v2.x (installed on this machine as v2.8.0) requires a `version: "2"` field and uses `linters.default: none` instead of v1's `disable-all: false`.

Minimal working v2 config:

```yaml
version: "2"

linters:
  default: none
  enable:
    - errcheck
    - govet
    - staticcheck

linters-settings:
  errcheck:
    check-type-assertions: true
```

**Why:** The v1 config format (without `version: "2"`) causes exit code 3 with: "unsupported version of the configuration".

**How to apply:** Whenever creating or updating `.golangci.yml` in this project, always use the v2 format with `version: "2"` at the top.
