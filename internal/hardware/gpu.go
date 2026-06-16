package hardware

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
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

// DetectGPUs returns all detected GPUs on the current system.
// It returns an empty slice (not error) if no GPUs are found or detection fails.
// Detection runs fast platform-specific commands in a best-effort manner.
func DetectGPUs() []GPU {
	gpus := detectGPUs()
	for i := range gpus {
		gpus[i].Index = i
		gpus[i].IsDefault = i == 0
	}
	return gpus
}

// LlamaServerFlags returns the llama-server CLI flags for the given GPU and layers.
// gpu may be nil (use CPU).
// layers: -1 means "auto" (offload all), 0 means CPU only, >0 means N layers.
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
	case "nvidia", "amd":
		if layers == 0 {
			return []string{"--n-gpu-layers", "0"}
		}
		return []string{"--n-gpu-layers", layerVal, "--main-gpu", strconv.Itoa(gpu.Index)}
	default:
		return []string{"--n-gpu-layers", "0"}
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

// parseLspci parses lspci output for VGA/3D controller lines.
func parseLspci(output string) []GPU {
	var gpus []GPU
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(line)
		if !strings.Contains(upper, "VGA COMPATIBLE CONTROLLER") &&
			!strings.Contains(upper, "3D CONTROLLER") &&
			!strings.Contains(upper, "DISPLAY CONTROLLER") {
			continue
		}
		// Format: "xx:xx.x VGA compatible controller: Vendor Name [Model] (rev xx)"
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+1:])
		// Strip the controller type prefix
		for _, prefix := range []string{
			"VGA compatible controller: ",
			"3D controller: ",
			"Display controller: ",
		} {
			if strings.HasPrefix(rest, prefix) {
				rest = strings.TrimPrefix(rest, prefix)
				break
			}
		}
		vendor := parseVendorString(rest)
		gpus = append(gpus, GPU{
			Name:   rest,
			VRAM:   "Unknown",
			Vendor: vendor,
		})
	}
	return gpus
}
