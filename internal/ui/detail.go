package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// OpenChatMsg is sent when user presses C to open the chat view.
type OpenChatMsg struct{}

// DetailModel is the Bubble Tea model for the right panel (detail + controls).
type DetailModel struct {
	model       *LocalModel // currently selected model (nil = nothing selected)
	serverState string      // "STOPPED", "STARTING", "RUNNING", "ERROR"
	address     string      // e.g. "http://localhost:8080"
	gpuName     string      // active GPU name for display
	focused     bool
	width       int
	height      int
}

// NewDetail creates a new DetailModel.
func NewDetail(width, height int) DetailModel {
	return DetailModel{
		serverState: "STOPPED",
		width:       width,
		height:      height,
	}
}

// SetFocus sets whether this panel has keyboard focus.
func (m *DetailModel) SetFocus(focused bool) {
	m.focused = focused
}

// SetSize updates panel dimensions.
func (m *DetailModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetModel updates the currently selected model.
func (m *DetailModel) SetModel(model *LocalModel) {
	m.model = model
}

// SetServerState updates the server state fields.
func (m *DetailModel) SetServerState(state, address, gpuName string) {
	m.serverState = state
	m.address = address
	m.gpuName = gpuName
}

// Init implements tea.Model.
func (m DetailModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "l":
			if m.model != nil {
				lm := *m.model
				return m, func() tea.Msg { return ModelLoadRequestMsg{Model: lm} }
			}
		case "u":
			return m, func() tea.Msg { return ModelUnloadRequestMsg{} }
		case "c":
			return m, func() tea.Msg { return OpenChatMsg{} }
		case "delete", "backspace", "ctrl+d":
			if m.model != nil {
				lm := *m.model
				return m, func() tea.Msg { return ModelDeleteRequestMsg{Model: lm} }
			}
		}
	}

	return m, nil
}

// View renders the detail panel content.
func (m DetailModel) View() string {
	if m.model == nil {
		hint := StyleDim.Render("Select a model from the left panel\nor press [d] to download a new model.")
		return hint
	}

	lm := m.model

	// Separator line.
	sep := StyleDim.Render(strings.Repeat("─", m.width-6))
	if m.width <= 6 {
		sep = StyleDim.Render("─────────────────────────────────────────")
	}

	// Title.
	title := StyleBold.Render(lm.Name)

	// Status badge.
	var statusStr string
	switch lm.Status {
	case StatusLoaded:
		statusStr = StyleBadgeLoaded.Render("● LOADED")
	case StatusDownloading:
		pct := int(lm.Progress * 100)
		statusStr = StyleBadgeDownload.Render(fmt.Sprintf("⣾ DOWNLOADING %d%%", pct))
	case StatusPaused:
		pct := int(lm.Progress * 100)
		statusStr = StyleBadgeDownload.Render(fmt.Sprintf("⏸ PAUSED %d%%", pct))
	default:
		statusStr = StyleBadgeAvail.Render("○ AVAILABLE")
	}

	// Field label width for alignment.
	const labelW = 15

	fieldLabel := func(label string) string {
		return StyleDim.Render(fmt.Sprintf("%-*s", labelW, label))
	}

	// GPU line.
	gpuLine := ""
	if m.gpuName != "" {
		gpu := fmt.Sprintf("%s · Metal", m.gpuName)
		gpuLine = fmt.Sprintf("\n  %s%s\n", fieldLabel("GPU"), gpu)
	}

	// Downloaded date.
	downloaded := lm.DownloadedAt.Format("2006-01-02")

	details := fmt.Sprintf(
		"  %s\n  %s%s\n  %s%s\n  %s%s\n  %s%s",
		fieldLabel("Quantization")+lm.Quant,
		fieldLabel("Size"), lm.SizeDisplay,
		fieldLabel("Downloaded"), downloaded,
		fieldLabel("Status"), statusStr,
		fieldLabel("Path"), StyleDim.Render(truncatePath(lm.Path, m.width-labelW-6)),
	)

	// Action keys.
	keysLine := buildKeyHints([]keyHint{
		{"L", "Load"},
		{"U", "Unload"},
		{"C", "Chat"},
		{"Ctrl+D", "Delete"},
	})

	view := lipgloss.JoinVertical(
		lipgloss.Left,
		"  "+title,
		"  "+sep,
		"",
		details,
		gpuLine,
		"  "+sep,
		"",
		"  "+keysLine,
	)

	return view
}

// keyHint is a key+label pair for the hints bar.
type keyHint struct {
	key   string
	label string
}

// buildKeyHints renders a row of [Key] Label hints.
func buildKeyHints(hints []keyHint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = StyleKey.Render("["+h.key+"]") + " " + h.label
	}
	return strings.Join(parts, "    ")
}

// truncatePath shortens a path for display.
func truncatePath(path string, maxLen int) string {
	if maxLen <= 0 || len(path) <= maxLen {
		return path
	}
	return "…" + path[len(path)-maxLen+1:]
}
