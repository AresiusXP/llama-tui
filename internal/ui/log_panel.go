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

// colorLogLine applies semantic colour to a single log line based on content.
// The classification uses simple substring matching on a lowercased copy —
// fast and sufficient for llama-server's output format.
func colorLogLine(line string) string {
	lower := strings.ToLower(line)

	switch {
	case strings.Contains(lower, "error") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "err "):
		return StyleError.Render(line)

	case strings.Contains(lower, "warn"):
		return StyleWarning.Render(line)

	case strings.Contains(lower, "---") ||
		strings.Contains(lower, "─── live ───") ||
		strings.Contains(lower, "─── dead ───"):
		return StyleSuccess.Bold(true).Render(line)

	case strings.Contains(lower, "model loaded") ||
		strings.Contains(lower, "server is listening") ||
		strings.Contains(lower, "listening on") ||
		strings.Contains(lower, "main: model") ||
		strings.Contains(lower, "llama server"):
		return StyleLogInfo.Render(line)

	default:
		return StyleDim.Render(line)
	}
}

// View renders the log panel content (without the border — RenderFrame adds that).
func (m LogPanelModel) View() string {
	// Build the panel title header (2 lines: title + separator rule).
	titleLine := StylePanelTitle.Render("LOGS")
	w := m.width
	if w < 1 {
		w = 40
	}
	sep := StyleDim.Render(strings.Repeat("─", w))

	// Lines available for actual log content:
	// total height − 2 (title + separator) − 1 (error banner if set).
	maxLines := m.height - 2
	if m.lastError != "" {
		maxLines--
	}
	if maxLines < 1 {
		maxLines = 1
	}

	if len(m.logs) == 0 && m.lastError == "" {
		// Compact empty state when the panel is too small for two lines.
		if m.height < 4 {
			return strings.Join([]string{titleLine, sep, StyleDim.Render("No logs yet.")}, "\n")
		}
		empty1 := StyleDim.Render("No server logs yet.")
		empty2 := StyleDim.Render("Logs appear when a model is loaded.")
		return strings.Join([]string{titleLine, sep, empty1, empty2}, "\n")
	}

	var lines []string
	lines = append(lines, titleLine, sep)

	if m.lastError != "" {
		lines = append(lines, StyleError.Render("✕ "+m.lastError))
	}

	// Take the most recent lines that fit.
	visible := m.logs
	if len(visible) > maxLines {
		visible = visible[len(visible)-maxLines:]
	}

	// Truncate each line to the panel inner width.
	maxLineWidth := m.width
	if maxLineWidth < 1 {
		maxLineWidth = 40
	}
	for _, l := range visible {
		runes := []rune(l)
		if len(runes) > maxLineWidth {
			l = string(runes[:maxLineWidth-1]) + "…"
		}
		lines = append(lines, colorLogLine(l))
	}

	return strings.Join(lines, "\n")
}
