// Package ui — LogPanelModel shows server log lines in the bottom-left panel.
package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// logRingCap is the maximum number of log lines retained.
const logRingCap = 100

// LogPanelModel displays server log output in the bottom-left panel.
type LogPanelModel struct {
	logs      []string
	lastError string // shown as a header when non-empty
	width     int
	height    int
}

// NewLogPanelModel creates a LogPanelModel with an empty log buffer.
func NewLogPanelModel() LogPanelModel {
	return LogPanelModel{}
}

// SetSize updates the panel's rendering dimensions.
func (m *LogPanelModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetLogs replaces the log buffer (capped at logRingCap entries).
func (m *LogPanelModel) SetLogs(logs []string) {
	if len(logs) > logRingCap {
		logs = logs[len(logs)-logRingCap:]
	}
	m.logs = logs
}

// SetLastError sets an error banner shown at the top of the panel.
func (m *LogPanelModel) SetLastError(err string) {
	m.lastError = err
}

// Init implements tea.Model.
func (m LogPanelModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m LogPanelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// View renders the log panel content (without the border — RenderFrame adds that).
func (m LogPanelModel) View() string {
	if len(m.logs) == 0 && m.lastError == "" {
		return StyleDim.Render("No logs yet.")
	}

	var lines []string

	if m.lastError != "" {
		lines = append(lines, StyleError.Render("✕ "+m.lastError))
	}

	// Calculate how many log lines we can show given the panel's inner height.
	// m.height is the inner content height (border already handled by RenderFrame).
	// Reserve 1 line for the error banner if set.
	maxLines := m.height
	if m.lastError != "" {
		maxLines--
	}
	if maxLines < 1 {
		maxLines = 1
	}

	// Take the most recent lines.
	visible := m.logs
	if len(visible) > maxLines {
		visible = visible[len(visible)-maxLines:]
	}

	// Truncate each line to fit panel inner width.
	maxLineWidth := m.width
	if maxLineWidth < 1 {
		maxLineWidth = 40
	}
	for _, l := range visible {
		// Truncate long lines safely using rune slicing (avoids splitting multi-byte characters).
		runes := []rune(l)
		if len(runes) > maxLineWidth {
			l = string(runes[:maxLineWidth-1]) + "…"
		}
		lines = append(lines, StyleDim.Render(l))
	}

	return strings.Join(lines, "\n")
}
