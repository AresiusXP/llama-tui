// Package ui — StatusPanelModel shows the server/GPU/version status in the top-right panel.
package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// StatusPanelModel displays current app/server status.
// When MetricsEnabled is false it renders a single summary line.
// When MetricsEnabled is true it renders two lines: the summary line plus
// a live performance row (generation t/s, prompt t/s, active requests).
type StatusPanelModel struct {
	// ModelLoaded indicates whether a model is currently loaded.
	ModelLoaded bool

	// UsageStats is a short description of the server activity,
	// e.g. "Active", "Idle", "Starting", "Error".
	UsageStats string

	// GPU is the name of the active GPU (or "CPU" if none).
	GPU string

	// LlamaVersion is the llama.cpp build tag, e.g. "b9667".
	LlamaVersion string

	// AppVersion is the application version string, e.g. "v0.3.0".
	AppVersion string

	// ── Live metrics (populated by periodic /metrics + /slots polling) ──

	// MetricsEnabled is true when llama-server was started with --metrics.
	// Controls whether the second row is rendered.
	MetricsEnabled bool

	// GenerationTPS is the average token generation throughput (tokens/sec).
	GenerationTPS float64

	// PromptTPS is the average prompt ingestion throughput (tokens/sec).
	PromptTPS float64

	// RequestsProcessing is the number of in-flight inference requests.
	RequestsProcessing int

	// ActiveSlots is the number of parallel slots currently generating.
	ActiveSlots int

	// TotalSlots is the total number of parallel slots configured.
	TotalSlots int

	// MetricsReady is true once at least one metrics poll has returned data.
	// Prevents showing "0.00 t/s" before the first poll.
	MetricsReady bool
}

// NewStatusPanelModel creates a StatusPanelModel with sensible defaults.
func NewStatusPanelModel() StatusPanelModel {
	return StatusPanelModel{
		UsageStats:   "Idle",
		GPU:          "None",
		LlamaVersion: "N/A",
		AppVersion:   "v0.1.0",
	}
}

// Init implements tea.Model.
func (m StatusPanelModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m StatusPanelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// View renders the status panel content (without the border — RenderFrame adds that).
// Returns 1 line when MetricsEnabled is false, 2 lines when true.
// The status panel is too compact (1–2 inner lines) to fit a multi-line title header,
// so the green ● bullet acts as a visual anchor / panel identity marker inline.
func (m StatusPanelModel) View() string {
	sep := StyleDim.Render("  │  ")

	// ── Line 1: static info ──────────────────────────────────────────────
	var modelStr string
	if m.ModelLoaded {
		// Green bullet used as the inline "panel title" anchor.
		modelStr = StyleBadgeLoaded.Render("● Loaded")
	} else {
		modelStr = StyleBadgeStopped.Render("○ Not Loaded")
	}

	usageStr := StyleDim.Render(m.UsageStats)

	gpu := m.GPU
	if gpu == "" {
		gpu = "CPU"
	}
	gpuStr := StyleDim.Render(gpu)

	// Show slot occupancy when model is loaded.
	var slotsStr string
	if m.ModelLoaded {
		if m.TotalSlots > 0 {
			slotsStr = StyleDim.Render(fmt.Sprintf("Slots %d/%d", m.ActiveSlots, m.TotalSlots))
		} else {
			slotsStr = StyleDim.Render("Slots …")
		}
	}

	llamaStr := StyleDim.Render(m.LlamaVersion)
	appStr := StyleMuted.Render(m.AppVersion)

	line1Parts := []string{modelStr, sep, usageStr}
	if slotsStr != "" {
		line1Parts = append(line1Parts, sep, slotsStr)
	}
	line1Parts = append(line1Parts, sep, gpuStr, sep, llamaStr, sep, appStr)
	line1 := strings.Join(line1Parts, "")

	if !m.MetricsEnabled {
		return line1
	}

	// ── Line 2: live performance metrics (only when --metrics is on) ─────
	var genStr, promptStr, reqStr string
	if !m.MetricsReady {
		genStr = StyleDim.Render("Gen: …")
		promptStr = StyleDim.Render("Prompt: …")
		reqStr = StyleDim.Render("Requests: …")
	} else {
		genStr = StyleDim.Render(fmt.Sprintf("Gen: %.1f t/s", m.GenerationTPS))
		promptStr = StyleDim.Render(fmt.Sprintf("Prompt: %.1f t/s", m.PromptTPS))
		reqStr = StyleDim.Render(fmt.Sprintf("Requests: %d", m.RequestsProcessing))
	}

	line2 := strings.Join([]string{genStr, sep, promptStr, sep, reqStr}, "")

	return line1 + "\n" + line2
}
