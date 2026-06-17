package hardware

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPU represents a detected GPU device.
type GPU struct {
	Index     int    // 0-based index within detected list
	Name      string // e.g. "Apple M3 Pro", "NVIDIA GeForce RTX 4090"
	VRAM      string // e.g. "16 GB", "Shared" (Apple Silicon), "Unknown"
	Vendor    string // "apple", "nvidia", "amd", "intel", "unknown"
	IsDefault bool   // true for index 0
}

var (
	gpuCache     []GPU
	gpuCacheOnce sync.Once
)

// DetectGPUs returns all detected GPUs on the current system.
// Results are cached after the first call so that external commands
// (nvidia-smi, lspci, system_profiler) are only executed once per process.
// It returns an empty slice (not error) if no GPUs are found or detection fails.
func DetectGPUs() []GPU {
	gpuCacheOnce.Do(func() {
		raw := detectGPUs()
		for i := range raw {
			raw[i].Index = i
			raw[i].IsDefault = i == 0
		}
		gpuCache = raw
	})
	// Return a copy so callers cannot mutate the cache.
	out := make([]GPU, len(gpuCache))
	copy(out, gpuCache)
	return out
}

// LlamaServerFlags returns the llama-server CLI flags for the given GPU and layers.
// gpu may be nil (use CPU).
// layers: -1 means "auto" (offload all), 0 means CPU only, >0 means N layers.
//
// For Vulkan backends (nvidia, amd, intel on Linux) the device is selected via
// the GGML_VK_VISIBLE_DEVICES environment variable returned by LlamaServerEnv,
// which hides all other Vulkan devices so the chosen GPU is always Vulkan0.
// --main-gpu is therefore never emitted; it would be wrong when multiple Vulkan
// devices are visible and redundant (== 0) when only one is.
func LlamaServerFlags(gpu *GPU, layers int) []string {
	if gpu == nil || gpu.Vendor == "unknown" {
		return []string{"--n-gpu-layers", "0"}
	}

	layerVal := strconv.Itoa(layers)
	if layers == -1 {
		layerVal = "999"
	}

	switch gpu.Vendor {
	case "apple":
		return []string{"--n-gpu-layers", layerVal}
	case "nvidia", "amd", "intel":
		if layers == 0 {
			return []string{"--n-gpu-layers", "0"}
		}
		return []string{"--n-gpu-layers", layerVal}
	default:
		return []string{"--n-gpu-layers", "0"}
	}
}

// LlamaServerEnv returns additional environment variables that must be set when
// launching llama-server for the given GPU. Returns nil for CPU/Apple (no extra
// env needed).
//
// For Vulkan-capable GPUs (nvidia, amd, intel) it emits:
//
//	GGML_VK_VISIBLE_DEVICES=<gpu.Index>
//
// This restricts Vulkan enumeration to only the selected device, preventing
// llama.cpp from spilling tensors onto other GPUs (which can cause OOM crashes
// when those GPUs have limited free VRAM).
func LlamaServerEnv(gpu *GPU) []string {
	if gpu == nil || gpu.Vendor == "unknown" || gpu.Vendor == "apple" {
		return nil
	}
	switch gpu.Vendor {
	case "nvidia", "amd", "intel":
		return []string{fmt.Sprintf("GGML_VK_VISIBLE_DEVICES=%d", gpu.Index)}
	default:
		return nil
	}
}

// detectGPUs is the platform-specific implementation (build tags select the real one).
// This file provides the shared helpers; platform files provide detectGPUs().

// runCommand executes a command with a 5-second timeout and returns its combined
// stdout output. Returns empty string on any error or timeout.
func runCommand(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

// parseVendorString normalises a vendor string from system_profiler / lspci output.
func parseVendorString(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "apple"):
		return "apple"
	case strings.Contains(lower, "nvidia"):
		return "nvidia"
	case strings.Contains(lower, "amd") || strings.Contains(lower, "advanced micro"):
		return "amd"
	case strings.Contains(lower, "intel"):
		return "intel"
	default:
		return "unknown"
	}
}

// parseMacOSGPUs parses `system_profiler SPDisplaysDataType` text output.
func parseMacOSGPUs(output string) []GPU {
	var gpus []GPU

	type entry struct {
		name   string
		vram   string
		vendor string
	}

	var current *entry
	scanner := bufio.NewScanner(strings.NewReader(output))

	flush := func() {
		if current == nil {
			return
		}
		if current.name == "" {
			current = nil
			return
		}
		v := current.vendor
		if v == "" {
			v = parseVendorString(current.name)
		}
		if current.vram == "" {
			if v == "apple" {
				current.vram = "Shared"
			} else {
				current.vram = "Unknown"
			}
		}
		gpus = append(gpus, GPU{
			Name:   current.name,
			VRAM:   current.vram,
			Vendor: v,
		})
		current = nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// A new top-level GPU section starts with a non-empty line that ends
		// with a colon and has no leading whitespace (indentation level 1).
		// system_profiler uses 4-space indentation for the GPU name line.
		if trimmed == "" {
			continue
		}

		// Detect "Chipset Model:" line — this starts a new GPU block.
		if strings.HasPrefix(trimmed, "Chipset Model:") {
			flush()
			current = &entry{}
			current.name = strings.TrimSpace(strings.TrimPrefix(trimmed, "Chipset Model:"))
			continue
		}

		if current == nil {
			continue
		}

		// VRAM line variants: "VRAM (Total):", "VRAM (Dynamic, Max):", plain "VRAM:"
		if strings.Contains(strings.ToUpper(trimmed), "VRAM") && strings.Contains(trimmed, ":") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				if val != "" {
					current.vram = val
				}
			}
			continue
		}

		if strings.HasPrefix(trimmed, "Vendor:") {
			raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "Vendor:"))
			current.vendor = parseVendorString(raw)
			continue
		}
	}
	flush()

	return gpus
}

// parseNvidiaSMI parses nvidia-smi CSV output (index,name,memory_MiB).
func parseNvidiaSMI(output string) []GPU {
	var gpus []GPU
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 3)
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[1])
		memMiB := strings.TrimSpace(parts[2])
		vram := "Unknown"
		if mb, err := strconv.Atoi(memMiB); err == nil {
			gb := float64(mb) / 1024.0
			vram = strconv.FormatFloat(gb, 'f', 0, 64) + " GB"
		}
		gpus = append(gpus, GPU{
			Name:   name,
			VRAM:   vram,
			Vendor: "nvidia",
		})
	}
	return gpus
}

// trimLspciName shortens a raw lspci device description to a more readable form.
// It removes known long vendor prefixes and strips the trailing "(rev XX)" suffix.
// Example: "Advanced Micro Devices, Inc. [AMD/ATI] HawkPoint1 (rev c5)" → "AMD HawkPoint1"
func trimLspciName(raw, vendor string) string {
	name := raw
	// Strip long AMD/ATI vendor prefix variations.
	for _, pfx := range []string{
		"Advanced Micro Devices, Inc. [AMD/ATI] ",
		"Advanced Micro Devices, Inc. [AMD] ",
		"Advanced Micro Devices, Inc. ",
	} {
		if strings.HasPrefix(name, pfx) {
			name = strings.TrimPrefix(name, pfx)
			break
		}
	}
	// Strip long Intel vendor prefix.
	for _, pfx := range []string{
		"Intel Corporation ",
		"Intel Corp. ",
	} {
		if strings.HasPrefix(name, pfx) {
			name = strings.TrimPrefix(name, pfx)
			break
		}
	}
	// Strip long NVIDIA vendor prefix.
	if strings.HasPrefix(name, "NVIDIA Corporation ") {
		name = strings.TrimPrefix(name, "NVIDIA Corporation ")
	}
	// Strip trailing "(rev XX)" suffix.
	if idx := strings.LastIndex(name, " (rev "); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	// Prepend a short vendor tag if the name doesn't already start with it.
	tag := ""
	switch vendor {
	case "amd":
		tag = "AMD "
	case "intel":
		tag = "Intel "
	case "nvidia":
		tag = "NVIDIA "
	}
	if tag != "" && !strings.HasPrefix(strings.ToUpper(name), strings.ToUpper(tag)) {
		name = tag + name
	}
	if name == "" {
		return raw
	}
	return name
}

// parseLspci parses lspci output for VGA/3D controller lines.
func parseLspci(output string) []GPU {
	var gpus []GPU
	// controller type labels that appear before the vendor/model in lspci output.
	typeLabels := []string{
		"VGA compatible controller: ",
		"3D controller: ",
		"Display controller: ",
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(line)
		if !strings.Contains(upper, "VGA COMPATIBLE CONTROLLER") &&
			!strings.Contains(upper, "3D CONTROLLER") &&
			!strings.Contains(upper, "DISPLAY CONTROLLER") {
			continue
		}
		// Find the controller type label (case-insensitive) and extract everything after it.
		// This is robust to any number of colons in the PCI address (including PCI domains).
		var rest string
		found := false
		for _, lbl := range typeLabels {
			if idx := strings.Index(upper, strings.ToUpper(lbl)); idx >= 0 {
				rest = strings.TrimSpace(line[idx+len(lbl):])
				found = true
				break
			}
		}
		if !found || rest == "" {
			continue
		}
		vendor := parseVendorString(rest)
		name := trimLspciName(rest, vendor)
		gpus = append(gpus, GPU{
			Name:   name,
			VRAM:   "Unknown",
			Vendor: vendor,
		})
	}
	return gpus
}
