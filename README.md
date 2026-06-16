# llama-tui

A beautiful, k9s-inspired terminal UI for managing and running local LLM models via [llama-server](https://github.com/ggml-org/llama.cpp).

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ [?] help  [q] quit  [s] settings  [d] download                               │
├───────────────────────┬──────────────────────────────────────────────────────┤
│ LOCAL MODELS          │ ● Loaded  │  Active  │  Slots 1/4  │  M3 Pro  │ v0.4 │
│ ──────────────────    ├──────────────────────────────────────────────────────┤
│ ▸ ● Llama-3.2-3B Q4   │ Llama-3.2-3B-Instruct-Q4_K_M.gguf                    │
│   ○ Mistral-7B Q5     │ ──────────────────────────────────────────────────── │
│   ○ Gemma-3-12B Q3    │  Quantization   Q4_K_M                               │
│                       │  Size           2.01 GB                              │
│                       │  Downloaded     2025-06-12                           │
│                       │  Status         ● LOADED                             │
├───────────────────────┤                                                      │
│ SERVER LOG            │  GPU            Apple M3 Pro · Metal                 │
│ ──────────────────    │                                                      │
│ llama server listen.. │  [L] Load  [U] Unload  [C] Chat  [Ctrl+D] Delete     │
│ model loaded ok       │                                                      │
├───────────────────────┴──────────────────────────────────────────────────────┤
│ ● RUNNING  http://localhost:8080                                             │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Features

- **Model library** — Browse and manage downloaded GGUF models
- **HuggingFace integration** — Search or pick from a curated list of popular models, download with real-time progress and cancel/resume support
- **llama-server management** — Load/unload models, one at a time, with automatic health-check and auto-install on first run
- **GPU selection** — Auto-detects all GPUs; lets you pick which one to use (supports Apple Metal, NVIDIA CUDA, AMD ROCm)
- **Inline chat** — Test loaded models directly inside the TUI without leaving your terminal
- **Persistent status panel** — Always-visible top-right panel showing server state, active slots, GPU name, llama.cpp build, and app version
- **Server log panel** — Live, color-coded llama-server output in a ring-buffered panel (last 100 lines)
- **Live metrics** — Optional Prometheus endpoint shows generation speed (t/s), prompt speed (t/s), and request count in real time
- **OpenAI-compatible endpoint** — Every loaded model exposes `http://localhost:8080/v1/` for Opencode, curl, or any other client
- **Self-update** — App and llama-server update themselves from GitHub Releases (`Ctrl+U`)
- **Cross-platform** — macOS (arm64/x64) and Linux (x64/arm64)

## Installation

### Homebrew (macOS and Linux)

```bash
brew install AresiusXP/tap/llama-tui
```

Works on macOS (Apple Silicon and Intel) and Linux via [Homebrew on Linux](https://docs.brew.sh/Homebrew-on-Linux).

### Download a release binary

```bash
# macOS (Apple Silicon)
curl -L https://github.com/AresiusXP/llama-tui/releases/latest/download/llama-tui_latest_darwin_arm64.tar.gz | tar xz
./llama-tui

# macOS (Intel)
curl -L https://github.com/AresiusXP/llama-tui/releases/latest/download/llama-tui_latest_darwin_amd64.tar.gz | tar xz
./llama-tui

# Linux (x64)
curl -L https://github.com/AresiusXP/llama-tui/releases/latest/download/llama-tui_latest_linux_amd64.tar.gz | tar xz
./llama-tui
```

### Build from source

```bash
git clone https://github.com/AresiusXP/llama-tui
cd llama-tui
make build
./dist/llama-tui
```

Requires Go 1.22+.

## First run

On first launch, llama-tui will detect your platform and download the appropriate `llama-server` binary from the [llama.cpp releases](https://github.com/ggml-org/llama.cpp/releases). The binary is stored at `~/.local/share/llama-tui/bin/llama-server`.

## Configuration

Config file: `~/.config/llama-tui/config.toml`

```toml
models_dir = "/Users/you/.local/share/llama-tui/models"

[server]
port = 8080
context_size = 4096
gpu_layers = -1           # -1 = auto (offload all layers to GPU)
selected_gpu_index = 0    # index into detected GPU list
parallel_slots = -1       # -1 = auto; set to N to allow N simultaneous requests
kv_cache_type_k = ""      # key cache type: f16 (default), f32, bf16, q8_0, q4_0, q5_0, …
kv_cache_type_v = ""      # value cache type: same options as kv_cache_type_k
metrics_enabled = false   # true = enable Prometheus /metrics endpoint + live t/s display

[huggingface]
token = ""                # optional — required for gated models (Llama 3, etc.)

[update]
llama_cpp_build_tag = "b9667"
```

Press `s` inside the TUI to open the settings screen.

## Settings Screen

The settings screen (press `s`) exposes all configuration options without editing the TOML file manually:

| Field | Description |
|-------|-------------|
| Models directory | Where GGUF files are stored |
| Server port | Port llama-server listens on (default: 8080) |
| Context size | Token context window size (default: 4096) |
| GPU layers | Layers to offload to GPU; -1 = all (default: -1) |
| Parallel slots | Max simultaneous requests; -1 = auto (default: -1) |
| KV cache type K | Key-cache quantization type (default: f16) |
| KV cache type V | Value-cache quantization type (default: f16) |
| Metrics endpoint | Toggle the Prometheus `/metrics` endpoint and live t/s display |
| HuggingFace token | API token for downloading gated models |
| GPU selection | Pick which detected GPU llama-server uses |

Navigate fields with `Tab` / `Shift+Tab` or `↑` / `↓`. Press `Ctrl+S` to save and close, `Esc` to discard.

## Key Bindings

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Navigate model list |
| `Enter` / `l` | Load selected model |
| `u` | Unload running model |
| `c` | Open inline chat |
| `d` | Download / search models |
| `x` | Cancel / resume download |
| `Ctrl+D` | Delete model file |
| `Ctrl+U` | Install / update llama-server |
| `s` | Settings |
| `Tab` | Switch panel focus |
| `?` | Toggle help overlay |
| `q` / `Ctrl+C` | Quit |

## Using with Opencode

Once a model is loaded, the server exposes an OpenAI-compatible API at `http://localhost:8080`. Configure Opencode:

```json
{
  "providers": {
    "llama-local": {
      "base_url": "http://localhost:8080",
      "model": "local"
    }
  }
}
```

## License

MIT
