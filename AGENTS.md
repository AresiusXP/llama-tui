# AGENTS.md — llama-tui Contributor Guide

## Project Overview

**llama-tui** is a terminal UI (TUI) application for managing and running local LLM models via [llama-server](https://github.com/ggml-org/llama.cpp). It is written in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

Users can:
- Browse and download GGUF models from HuggingFace
- Load/unload models into llama-server
- Select which GPU to use (with multi-GPU support)
- Chat with loaded models directly inside the TUI
- Self-update both the app and the bundled llama-server binary

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
│   ├── huggingface/           # HuggingFace API client + resumable downloader
│   ├── updater/               # GitHub Releases-based self-update for app + llama-server
│   └── ui/                    # All Bubble Tea sub-models and Lip Gloss styles
│       ├── theme.go           # Colors + exported styles
│       ├── layout.go          # Two-panel frame layout
│       ├── models.go          # Shared data types (LocalModel, ModelStatus, etc.)
│       ├── library.go         # Left panel: local model list
│       ├── detail.go          # Right panel: model metadata + action keys
│       ├── chat.go            # Inline chat panel
│       ├── search.go          # HuggingFace search overlay
│       ├── download.go        # Download progress overlay
│       ├── settings.go        # Settings screen + GPU selector
│       └── help.go            # Key-bindings overlay
└── assets/
    ├── popular_models.json    # Embedded curated GGUF model list
    └── assets.go              # //go:embed shim
```

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

Async events from the llama-server manager are delivered via `manager.SetProgram(p)` which calls `p.Send(msg)` from goroutines. This is the approved Bubble Tea pattern for external events.

---

## Development

### Prerequisites

- Go 1.22+
- macOS or Linux (Windows not supported)

### Build & run

```bash
make build          # compiles to ./dist/llama-tui
make run            # build + run
make dev            # go run (faster iteration)
make test           # run all tests
make vet            # go vet
```

### Release

Releases are built by GoReleaser via GitHub Actions on tag push:

```bash
git tag v0.1.0
git push origin v0.1.0
```

This produces binaries for `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`.

---

## Coding Guidelines

### Bubble Tea patterns

- Sub-models **must** implement `tea.Model` (`Init`, `Update`, `View`).
- Sub-models **must not** import `internal/app` (would create import cycles).
- Sub-models communicate **upward** only via typed `tea.Msg` values — never function calls.
- Parent (`app.go`) handles all messages from sub-models by type-switching.
- Use `tea.Batch()` when multiple `tea.Cmd`s need to run concurrently.
- Use `p.Send(msg)` from goroutines for external async events (server logs, download progress, etc.).

### Package rules

- `internal/llamaserver` is **backend only** — must not import `internal/ui`.
- `internal/ui` must not import `internal/llamaserver` directly — use message types.
- `internal/hardware` is **pure detection** — no TUI code.
- `internal/config` has no dependencies on other internal packages.

### Style

- Follow standard Go formatting (`gofmt`).
- Export only what other packages need.
- Prefer small, composable functions over long methods.
- All exported types and functions must have doc comments.
- Error messages: lowercase, no trailing punctuation (`"load config"` not `"Load config."`).

### Config

Config lives at `~/.config/llama-tui/config.toml`. The `config.Load()` function creates it with defaults on first run. Never hardcode paths — always use `config.DataDir()`, `config.BinDir()`, or `cfg.ModelsDir`.

### UI conventions

- Use colors only from `ui/theme.go` constants (never inline hex strings in panel code).
- Use styles only from the exported `ui.Style*` vars.
- Panel content strings should **not** include border rendering — the frame in `layout.go` adds borders.
- Use `ui.StyleDim.Render(...)` for secondary/metadata text.
- Use `ui.StyleKey.Render("[X]")` for key hints.

---

## Testing

- Unit tests live next to the code they test (`foo_test.go`).
- Hardware detection tests are in `internal/hardware/gpu_test.go`.
- For network-dependent tests (HuggingFace API), use build tags or skip in CI.
- Run `go test ./...` before submitting changes.

---

## Adding a New Feature

1. Decide which package owns the new behavior.
2. If it touches the UI: create or modify a `*Model` in `internal/ui/`.
3. Define new `tea.Msg` types for communication.
4. Handle those messages in `internal/app/app.go`.
5. Update `ui/help.go` if new key bindings are added.
6. Run `make test && make vet && make build` to verify.
