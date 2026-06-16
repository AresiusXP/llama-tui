//go:build linux

package hardware

import (
	"os"
	"path/filepath"
	"strings"
)

func detectGPUs() []GPU {
	// 1. Try nvidia-smi first.
	out := runCommand("nvidia-smi", "--query-gpu=index,name,memory.total", "--format=csv,noheader,nounits")
	if out != "" {
		gpus := parseNvidiaSMI(out)
		if len(gpus) > 0 {
			return gpus
		}
	}

	// 2. Try lspci.
	lspciOut := runCommand("lspci")
	if lspciOut != "" {
		gpus := parseLspci(lspciOut)
		if len(gpus) > 0 {
			return gpus
		}
	}

	// 3. Fall back to /sys/class/drm PCI vendor IDs.
	return detectDRMGPUs()
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
