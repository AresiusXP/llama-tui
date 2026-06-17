//go:build linux

package hardware

import (
	"os"
	"path/filepath"
	"strings"
)

func detectGPUs() []GPU {
	var gpus []GPU

	// 1. Try nvidia-smi for NVIDIA GPUs — it provides accurate VRAM.
	out := runCommand("nvidia-smi", "--query-gpu=index,name,memory.total", "--format=csv,noheader,nounits")
	if out != "" {
		for _, g := range parseNvidiaSMI(out) {
			gpus = append(gpus, g)
		}
	}

	// Track whether nvidia-smi already provided NVIDIA entries so we can
	// skip the less-detailed lspci entries for the same cards.
	hasNvidia := false
	for _, g := range gpus {
		if g.Vendor == "nvidia" {
			hasNvidia = true
			break
		}
	}

	// 2. Try lspci for all remaining GPUs (AMD, Intel, etc.).
	//    If nvidia-smi already found NVIDIA cards, skip lspci NVIDIA entries
	//    to avoid duplicates (nvidia-smi provides better VRAM info anyway).
	lspciOut := runCommand("lspci")
	if lspciOut != "" {
		for _, g := range parseLspci(lspciOut) {
			if g.Vendor == "nvidia" && hasNvidia {
				// Already covered by nvidia-smi with accurate VRAM; skip.
				continue
			}
			gpus = append(gpus, g)
		}
	}

	// 3. Fall back to /sys/class/drm if still nothing found (e.g. lspci not installed).
	if len(gpus) == 0 {
		gpus = detectDRMGPUs()
	}

	return gpus
}

func detectDRMGPUs() []GPU {
	matches, err := filepath.Glob("/sys/class/drm/card[0-9]")
	if err != nil || len(matches) == 0 {
		return nil
	}

	var gpus []GPU
	for _, cardPath := range matches {
		vendorFile := filepath.Join(cardPath, "device", "vendor")
		data, err := os.ReadFile(vendorFile)
		if err != nil {
			continue
		}
		vendorID := strings.TrimSpace(string(data))
		var vendor string
		switch vendorID {
		case "0x10de":
			vendor = "nvidia"
		case "0x1002":
			vendor = "amd"
		case "0x8086":
			vendor = "intel"
		default:
			vendor = "unknown"
		}

		name := vendor + " GPU"
		labelFile := filepath.Join(cardPath, "device", "label")
		if labelData, err := os.ReadFile(labelFile); err == nil {
			name = strings.TrimSpace(string(labelData))
		}

		gpus = append(gpus, GPU{
			Name:   name,
			VRAM:   "Unknown",
			Vendor: vendor,
		})
	}
	return gpus
}

// DetectLinuxGPUBuild returns the recommended llama-server build variant for
// the current Linux system based on detected GPU hardware.
//
// Returns:
//   - "vulkan" — when at least one NVIDIA or AMD GPU is detected; the Vulkan
//     build works for both vendors without proprietary driver dependencies.
//   - ""       — no discrete/integrated GPU detected; use the CPU-only build.
func DetectLinuxGPUBuild() string {
	gpus := DetectGPUs()
	for _, g := range gpus {
		switch g.Vendor {
		case "nvidia", "amd", "intel":
			return "vulkan"
		}
	}
	return ""
}
