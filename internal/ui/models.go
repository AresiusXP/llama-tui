package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ModelStatus represents the state of a local model.
type ModelStatus int

const (
	StatusAvailable   ModelStatus = iota
	StatusLoaded                  // currently running in llama-server
	StatusDownloading             // being downloaded
	StatusPaused                  // download was cancelled; partial file on disk, resumable
)

// String returns a human-readable status label.
func (s ModelStatus) String() string {
	switch s {
	case StatusLoaded:
		return "LOADED"
	case StatusDownloading:
		return "DOWNLOADING"
	case StatusPaused:
		return "PAUSED"
	default:
		return "AVAILABLE"
	}
}

// BadgeStyle returns the appropriate badge lipgloss.Style for the status.
func (s ModelStatus) BadgeStyle() lipgloss.Style {
	switch s {
	case StatusLoaded:
		return StyleBadgeLoaded
	case StatusDownloading:
		return StyleBadgeDownload
	case StatusPaused:
		return StyleBadgeDownload // amber — same colour, different icon
	default:
		return StyleBadgeAvail
	}
}

// LocalModel represents a GGUF model file on disk.
type LocalModel struct {
	Name           string      // filename without path
	Path           string      // full path
	SizeBytes      int64
	SizeDisplay    string      // e.g. "4.1 GB"
	Quant          string      // quantization level parsed from filename, e.g. "Q4_K_M"
	Status         ModelStatus
	DownloadedAt   time.Time
	Progress       float64 // 0.0-1.0 during download / pause
	TotalBytes     int64   // expected total size during download (0 = unknown)
	RepoID         string  // HuggingFace repo ID — set during download; needed to resume
	RemoteFilename string  // File path within the repo — needed to resume
}

// quantPattern matches common GGUF quantization strings:
// Q-prefix (Q4_K_M, Q8_0), I-prefix (IQ3_XS), T-prefix (TQ1_0, TQ2_0),
// and F-prefix (F32, F16) floating-point formats.
var quantPattern = regexp.MustCompile(`(?i)[IQTF][QP]?\d+(?:_[A-Z0-9]+)*`)

// ParseQuant extracts quantization from a GGUF filename.
// e.g. "Mistral-7B-Q4_K_M.gguf" → "Q4_K_M"
// Returns "" if not found.
func ParseQuant(filename string) string {
	// Strip extension
	name := strings.TrimSuffix(filename, ".gguf")
	name = strings.TrimSuffix(name, ".GGUF")

	match := quantPattern.FindString(name)
	if match != "" {
		return strings.ToUpper(match)
	}
	return ""
}

// FormatSize converts bytes to human-readable string.
func FormatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
