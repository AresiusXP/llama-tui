// Package config handles loading and saving llama-tui configuration.
// Config file lives at ~/.config/llama-tui/config.toml (XDG-compliant).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

const appName = "llama-tui"

// Server holds llama-server runtime settings.
type Server struct {
	Port             int    `toml:"port"`
	ContextSize      int    `toml:"context_size"`
	GPULayers        int    `toml:"gpu_layers"`
	SelectedGPUIndex int    `toml:"selected_gpu_index"`
	LlamaServerPath  string `toml:"llama_server_path"` // override; empty = managed
}

// HuggingFace holds HF integration settings.
type HuggingFace struct {
	Token string `toml:"token"`
}

// Update holds update preferences.
type Update struct {
	LlamaCPPBuildTag string `toml:"llama_cpp_build_tag"` // e.g. "b9667"
	AppVersion       string `toml:"app_version"`
}

// Config is the root configuration structure.
type Config struct {
	ModelsDir   string      `toml:"models_dir"`
	Server      Server      `toml:"server"`
	HuggingFace HuggingFace `toml:"huggingface"`
	Update      Update      `toml:"update"`
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		ModelsDir: defaultModelsDir(),
		Server: Server{
			Port:             8080,
			ContextSize:      4096,
			GPULayers:        -1, // auto
			SelectedGPUIndex: 0,
			LlamaServerPath:  "",
		},
		HuggingFace: HuggingFace{
			Token: "",
		},
		Update: Update{
			LlamaCPPBuildTag: "",
			AppVersion:       "",
		},
	}
}

// Load reads the config file, creating it with defaults if it doesn't exist.
func Load() (*Config, error) {
	path := ConfigFilePath()

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	cfg := Default()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// First run: write defaults.
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	// Ensure models dir exists.
	if err := os.MkdirAll(cfg.ModelsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create models dir: %w", err)
	}

	return cfg, nil
}

// Save writes the current config to disk.
func (c *Config) Save() error {
	path := ConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	return enc.Encode(c)
}

// homeDir returns the user home directory, falling back to /tmp/llama-tui
// in restricted environments where $HOME is undefined.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	// Fallback: use a temp directory so the app remains usable.
	return filepath.Join(os.TempDir(), appName)
}

// ConfigFilePath returns the platform-appropriate config file path.
func ConfigFilePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName, "config.toml")
	}
	return filepath.Join(homeDir(), ".config", appName, "config.toml")
}

// DataDir returns the platform-appropriate data directory.
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}
	return filepath.Join(homeDir(), ".local", "share", appName)
}

// BinDir returns the directory where managed binaries (llama-server) are stored.
func BinDir() string {
	return filepath.Join(DataDir(), "bin")
}

// DefaultLlamaServerPath returns the default managed llama-server binary path.
func DefaultLlamaServerPath() string {
	return filepath.Join(BinDir(), llamaServerBinaryName())
}

func llamaServerBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

// LlamaServerPath returns the effective llama-server binary path,
// honouring any user override in config.
func (c *Config) LlamaServerPath() string {
	if c.Server.LlamaServerPath != "" {
		return c.Server.LlamaServerPath
	}
	return DefaultLlamaServerPath()
}

func defaultModelsDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName, "models")
	}
	return filepath.Join(homeDir(), ".local", "share", appName, "models")
}
