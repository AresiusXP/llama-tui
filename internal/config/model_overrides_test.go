package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelOverridesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	gpuLayers := 20
	overrides := ModelOverrides{
		"mistral-7b-q4.gguf": {
			ContextSize:   8192,
			GPULayers:     &gpuLayers,
			ParallelSlots: 2,
			KVCacheTypeK:  "q8_0",
			KVCacheTypeV:  "q8_0",
			Threads:       8,
			BatchSize:     1024,
		},
	}

	if err := SaveModelOverrides(overrides); err != nil {
		t.Fatalf("SaveModelOverrides: %v", err)
	}

	got, err := LoadModelOverrides()
	if err != nil {
		t.Fatalf("LoadModelOverrides: %v", err)
	}

	mc, ok := got["mistral-7b-q4.gguf"]
	if !ok {
		t.Fatal("expected entry for mistral-7b-q4.gguf")
	}
	if mc.ContextSize != 8192 {
		t.Errorf("ContextSize: got %d, want 8192", mc.ContextSize)
	}
	if mc.GPULayers == nil || *mc.GPULayers != 20 {
		t.Errorf("GPULayers: got %v, want pointer to 20", mc.GPULayers)
	}
	if mc.ParallelSlots != 2 {
		t.Errorf("ParallelSlots: got %d, want 2", mc.ParallelSlots)
	}
	if mc.KVCacheTypeK != "q8_0" {
		t.Errorf("KVCacheTypeK: got %q, want q8_0", mc.KVCacheTypeK)
	}
	if mc.KVCacheTypeV != "q8_0" {
		t.Errorf("KVCacheTypeV: got %q, want q8_0", mc.KVCacheTypeV)
	}
	if mc.Threads != 8 {
		t.Errorf("Threads: got %d, want 8", mc.Threads)
	}
	if mc.BatchSize != 1024 {
		t.Errorf("BatchSize: got %d, want 1024", mc.BatchSize)
	}
}

func TestLoadModelOverridesMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// File does not exist — should return empty map, not error.
	got, err := LoadModelOverrides()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestGPULayersNilRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// GPULayers nil (not set) should stay nil after round-trip.
	overrides := ModelOverrides{
		"model.gguf": {
			ContextSize: 4096,
			// GPULayers intentionally nil
		},
	}

	if err := SaveModelOverrides(overrides); err != nil {
		t.Fatalf("SaveModelOverrides: %v", err)
	}
	got, err := LoadModelOverrides()
	if err != nil {
		t.Fatalf("LoadModelOverrides: %v", err)
	}
	mc := got["model.gguf"]
	if mc.GPULayers != nil {
		t.Errorf("expected GPULayers nil, got %v", mc.GPULayers)
	}
}

func TestGPULayersMinusOneRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// GPULayers -1 (auto override) must survive round-trip as -1, not disappear.
	minus1 := -1
	overrides := ModelOverrides{
		"model.gguf": {
			GPULayers: &minus1,
		},
	}

	if err := SaveModelOverrides(overrides); err != nil {
		t.Fatalf("SaveModelOverrides: %v", err)
	}
	got, err := LoadModelOverrides()
	if err != nil {
		t.Fatalf("LoadModelOverrides: %v", err)
	}
	mc := got["model.gguf"]
	if mc.GPULayers == nil || *mc.GPULayers != -1 {
		t.Errorf("expected GPULayers -1, got %v", mc.GPULayers)
	}
}

func TestModelOverridesFilePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	want := filepath.Join(dir, "llama-tui", "models.toml")
	got := ModelOverridesFilePath()
	if got != want {
		t.Errorf("ModelOverridesFilePath: got %q, want %q", got, want)
	}
}

func TestSaveCreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	// Point XDG to a subdirectory that doesn't exist yet.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "nested", "dir"))

	overrides := ModelOverrides{"m.gguf": {Threads: 4}}
	if err := SaveModelOverrides(overrides); err != nil {
		t.Fatalf("SaveModelOverrides should create missing dir: %v", err)
	}
	if _, err := os.Stat(ModelOverridesFilePath()); err != nil {
		t.Errorf("models.toml not created: %v", err)
	}
}
