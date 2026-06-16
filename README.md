# llama-tui

A beautiful, k9s-inspired terminal UI for managing and running local LLM models via [llama-server](https://github.com/ggml-org/llama.cpp).

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ llama-tui v0.1.0                              ? help  q quit  s search       │
├───────────────────────┬──────────────────────────────────────────────────────┤
│ LOCAL MODELS          │ Llama-3.2-3B-Instruct-Q4_K_M.gguf                   │
│ ──────────────────    │ ────────────────────────────────────────────────      │
│ ▸ ● Llama-3.2-3B Q4  │  Quantization   Q4_K_M                               │
│   ○ Mistral-7B Q5    │  Size           2.01 GB                              │
│   ○ Gemma-3-12B Q3   │  Downloaded     2025-06-12                           │
│                       │  Status         ● LOADED                             │
│                       │                                                      │
│                       │  GPU            Apple M3 Pro · Metal                 │
│                       │                                                      │
│                       │  [L] Load    [U] Unload    [C] Chat    [Del] Delete  │
├───────────────────────┴──────────────────────────────────────────────────────┤
│ ● RUNNING  http://localhost:8080  │  GPU: M3 Pro  │  llama.cpp b9667         │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Features

- **Model library** — Browse and manage downloaded GGUF models
- **HuggingFace integration** — Search or pick from a curated list of popular models, download with real-time progress
- **llama-server management** — Load/unload models, one at a time, with automatic health-check
- **GPU selection** — Auto-detects all GPUs; lets you pick which one to use (supports Apple Metal, NVIDIA CUDA, AMD ROCm)
- **Inline chat** — Test loaded models directly inside the TUI without leaving your terminal
- **OpenAI-compatible endpoint** — Every loaded model exposes `http://localhost:8080/v1/` for Opencode, curl, or any other client
- **Self-update** — App and llama-server update themselves from GitHub Releases
- **Cross-platform** — macOS (arm64/x64) and Linux (x64/arm64)

## Installation

### Download a release binary

```bash
# macOS (Apple Silicon)
curl -L https://github.com/patriciodanos/llama-tui/releases/latest/download/llama-tui_latest_darwin_arm64.tar.gz | tar xz
./llama-tui

# macOS (Intel)
curl -L https://github.com/patriciodanos/llama-tui/releases/latest/download/llama-tui_latest_darwin_amd64.tar.gz | tar xz
./llama-tui

# Linux (x64)
curl -L https://github.com/patriciodanos/llama-tui/releases/latest/download/llama-tui_latest_linux_amd64.tar.gz | tar xz
./llama-tui
```

### Build from source

```bash
git clone https://github.com/patriciodanos/llama-tui
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
gpu_layers = -1          # -1 = auto (offload all layers to GPU)
selected_gpu_index = 0   # index into detected GPU list

[huggingface]
token = ""               # optional — required for gated models (Llama 3, etc.)

[update]
llama_cpp_build_tag = "b9667"
```

Press `s` inside the TUI to open the settings screen.

## Key Bindings

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Navigate model list |
| `Enter` / `l` | Load selected model |
| `u` | Unload running model |
| `c` | Open inline chat |
| `d` | Search / download models |
| `Del` | Delete model file |
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
