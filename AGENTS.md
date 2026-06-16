# AGENTS.md — llama-tui Contributor Guide

## Project Overview

**llama-tui** is a terminal UI (TUI) application for managing and running local LLM models via [llama-server](https://github.com/ggml-org/llama.cpp). Written in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

---

## Architecture

```
llama-tui/
├── main.go                    # Entrypoint — calls cmd.Execute()
├── cmd/root.go                # Cobra CLI setup; creates app.Model and tea.Program
├── internal/
│   ├── app/app.go             # Root Bubble Tea model — wires all sub-models together
│   ├── config/config.go       # Load/save ~/.config/llama-tui/config.toml
│   ├── hardware/              # GPU detection (macOS: system_profiler; Linux: nvidia-smi/lspci)
│   ├── llamaserver/           # llama-server subprocess manager + OpenAI HTTP client
│   │   ├── manager.go         # Process lifecycle; maxLogLines=200 ring buffer
│   │   ├── client.go          # OpenAI-compatible HTTP client
│   │   └── metrics.go         # FetchMetrics(): polls /slots always, /metrics when enabled
│   ├── huggingface/           # HuggingFace API client + resumable downloader
│   ├── updater/               # GitHub Releases-based self-update for app + llama-server
│   └── ui/                    # All Bubble Tea sub-models and Lip Gloss styles
│       ├── theme.go           # Colors (Catppuccin Mocha) + exported Style* vars
│       ├── layout.go          # Four-panel frame layout — NewLayout() + RenderFrame()
│       ├── models.go          # Shared data types (LocalModel, ModelStatus, etc.)
│       ├── library.go         # Top-left panel: local model list
│       ├── log_panel.go       # Bottom-left panel: live server log (logRingCap=100)
│       ├── status_panel.go    # Top-right panel: server/GPU/version/metrics (persistent)
│       ├── detail.go          # Bottom-right panel: model metadata + action keys
│       ├── confirm.go         # Delete confirmation modal (ConfirmModel)
│       ├── chat.go            # Inline chat panel (replaces detail in bottom-right)
│       ├── search.go          # HuggingFace search overlay
│       ├── download.go        # Download progress overlay
│       ├── settings.go        # Settings screen + GPU selector
│       └── help.go            # Key-bindings overlay
└── assets/
    ├── popular_models.json    # Embedded curated GGUF model list
    └── assets.go              # //go:embed shim
```

### 4-panel layout

```
┌─── left (30%) ───────┬─── right (70%) ──────────────────────────────┐
│  LibraryModel        │  StatusPanelModel  (1–2 inner lines)          │  top row
├──────────────────────┼──────────────────────────────────────────────┤
│  LogPanelModel       │  DetailModel  or  ChatModel                   │  bottom row
└──────────────────────┴──────────────────────────────────────────────┘
  action bar (1 row)
  status bar (1 row)
```

Height split per column: left top = 78% of inner height, left bottom = 22%. Right top = 1 inner line (metrics off) or 2 inner lines (metrics on); right bottom = remainder.

### Key signatures to know

```go
// layout.go
func NewLayout(width, height int, metricsEnabled bool) Layout

// RenderFrame — 8 content args + leftFocused flag
func RenderFrame(
    layout Layout,
    libraryContent, statusContent, logContent, detailContent,
    actionContent, statusBarContent string,
    leftFocused bool,
) string
```

`NewLayout` is called in `WindowSizeMsg`, `ServerStartedMsg`, and `ServerStoppedMsg` handlers in `app.go` — always pass the current `cfg.Server.MetricsEnabled`.

### Message flow

```
User input (key/mouse)
    ↓
app.Model.Update()
    ↓ dispatches to sub-models
    ↓ or handles server/hardware messages
    ↓
tea.Cmd (async work — network, disk, subprocess)
    ↓
tea.Msg returned to Update()
    ↓
app.Model.View() → renders panels via ui.RenderFrame()
```

Async events from the llama-server manager are delivered via `manager.SetProgram(p)` which calls `p.Send(msg)` from goroutines.

---

## Development

### Build & run

```bash
make build          # compiles to ./dist/llama-tui
make run            # build + run
make dev            # go run (faster iteration)
make test           # run all tests
make vet            # go vet
make lint           # golangci-lint — requires: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
make snapshot       # local GoReleaser build without publishing (good pre-release check)
```

**Verification order before submitting:** `make test && make vet && make build`

### Release

Tag push triggers GoReleaser via GitHub Actions → produces binaries for `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`.

---

## Critical Runtime Constraints

These are the most dangerous traps — violations freeze the UI or corrupt state:

### 1. Never block in `Update()` or `View()`

`Manager.IsHealthy()` spawns `llama-server --version` (subprocess). `Manager.LoadModel()` calls `UnloadModel()` which can block up to 5 s. **Both must run inside a `tea.Cmd`**, never directly in `Update()` or `View()`.

- Cache the health result in `m.llamaHealthy` / `m.llamaChecked`; read from the cache in `View()`.
- Use `m.llamaInstallTried` (bool guard) to prevent auto-install → unhealthy → auto-install loops.
- `View()` must be pure — read cached fields only, never spawn work.

### 2. Exactly one `cmd.Wait()` per process

In `manager.go`, the pipe-drain goroutine is the only caller of `cmd.Wait()` (it closes `waitDone` when done). All other callers — including `UnloadModel()` — block on `<-waitDone`. Adding a second `cmd.Wait()` call anywhere causes a panic; the `exec.Cmd` contract forbids it.

### 3. Detail panel must be synced after every library refresh

Call `detail.SetModel(library.SelectedModel())` after **every** library refresh **and** after `ServerStartedMsg`. If skipped, the `*LocalModel` snapshot inside the detail panel is stale (may show `StatusAvailable` even though the model is running), causing the status badge to display incorrectly regardless of `serverState`.

### 4. Metrics epoch validation

`ServerMetricsMsg` carries the `Epoch int` it was launched with. `app.Model.metricsEpoch` is incremented on every `ServerStartedMsg` and `ServerStoppedMsg`. In the handler, discard messages whose epoch doesn't match — they come from a prior polling loop. When adding any polling loop, follow this pattern to prevent stale-data overwrites and goroutine leaks.

---

## Package Rules

- `internal/llamaserver` — backend only; must not import `internal/ui`.
- `internal/ui` — must not import `internal/llamaserver` directly; communicate via message types.
- `internal/hardware` — pure detection; no TUI code.
- `internal/config` — no dependencies on other internal packages.
- Sub-models must not import `internal/app` (import cycle).
- Sub-models communicate upward **only** via typed `tea.Msg` values — never function calls.

---

## Coding Guidelines

### Bubble Tea patterns

- Sub-models **must** implement `tea.Model` (`Init`, `Update`, `View`).
- Use `tea.Batch()` when multiple `tea.Cmd`s need to run concurrently.
- Use `p.Send(msg)` from goroutines for external async events (server logs, download progress, etc.).
- For destructive actions, use `ConfirmModel` (`confirm.go`) — it sends `ConfirmDeleteYesMsg` / `ConfirmDeleteNoMsg` upward; handle them in `app.go`.

### Style

- Follow standard Go formatting (`gofmt`).
- Export only what other packages need.
- All exported types and functions must have doc comments.
- Error messages: lowercase, no trailing punctuation (`"load config"` not `"Load config."`).

### Config

Never hardcode paths — always use `config.DataDir()`, `config.BinDir()`, or `cfg.ModelsDir`. `config.Load()` creates the file with defaults on first run.

---

## UI Conventions

- Colors: use constants from `ui/theme.go` only (Catppuccin Mocha palette) — never inline hex strings.
- Styles: use exported `ui.Style*` vars. Key ones:
  - `StylePanelTitle` — green + bold; use for **all panel section headers** (not `StyleTitle`, which is accent-blue and reserved for overlay headings like search/settings)
  - `StyleDim` — secondary/metadata text
  - `StyleMuted` — timestamps and very secondary metadata (dimmer than `StyleDim`)
  - `StyleKey` — key hints like `[x]`
  - `StyleLogInfo` — important log events (full-brightness, not dim)
  - `StyleWarning` — caution states and log warning lines
  - `StyleError` — errors and stop states
- Panel `View()` methods must **not** render their own border — `RenderFrame` in `layout.go` adds borders for all four panels.
- Panel titles use 2 lines of overhead: `StylePanelTitle.Render("TITLE")` + `StyleDim.Render(strings.Repeat("─", width))` as the first two content lines.
- `ChatModel` does not render its own `StylePanel` border. It is constructed with `RightWidth-2` (inner content width) and `RightBottomHeight` (inner height).

---

## Shared Types (ui/models.go)

`ModelStatus` values:

| Constant | Meaning |
|---|---|
| `StatusAvailable` | On disk, not loaded |
| `StatusLoaded` | Currently running in llama-server |
| `StatusDownloading` | Active download in progress |
| `StatusPaused` | Download cancelled; partial file on disk, resumable |

`LocalModel.RepoID` and `RemoteFilename` are required for download resumption — they are set during download and must be preserved.

---

## Testing

- Unit tests live next to the code they test (`foo_test.go`).
- For network-dependent tests (HuggingFace API), use build tags or skip in CI.
- `go test ./...` before submitting.

---

## Adding a New Feature

1. Decide which package owns the new behavior.
2. If it touches the UI: create or modify a `*Model` in `internal/ui/`.
3. Define new `tea.Msg` types for async communication upward.
4. Handle those messages in `internal/app/app.go`.
5. Update `ui/help.go` if new key bindings are added.
6. `make test && make vet && make build`
