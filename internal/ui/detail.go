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
	w := m.width
	if w < 1 {
		w = 40
	}

	// Panel title header (consistent with all other panels).
	titleLine := StylePanelTitle.Render("MODEL DETAIL")
	sep := StyleDim.Render(strings.Repeat("─", w))

	if m.model == nil {
		hint := StyleDim.Render("Select a model from the left panel\nor press [d] to download a new model.")
		return lipgloss.JoinVertical(lipgloss.Left, titleLine, sep, "", hint)
	}

	lm := m.model

	// Inner separator (narrower, after fields).
	innerSep := StyleDim.Render(strings.Repeat("─", w-4))
	if w <= 4 {
		innerSep = StyleDim.Render("──────────────────────────────")
	}

	// Model name as sub-header — bold accent so it stands out beneath the panel title.
	modelName := StyleBold.Render(lm.Name)

	// Status badge — downloads/pauses take absolute priority.
	// For a loaded model, the live serverState refines the display.
	// For an available model, serverState is irrelevant — show AVAILABLE.
	var statusStr string
	switch {
	case lm.Status == StatusDownloading:
		pct := int(lm.Progress * 100)
		statusStr = StyleBadgeDownload.Render(fmt.Sprintf("⣾ DOWNLOADING %d%%", pct))
	case lm.Status == StatusPaused:
		pct := int(lm.Progress * 100)
		statusStr = StyleBadgeDownload.Render(fmt.Sprintf("⏸ PAUSED %d%%", pct))
	case lm.Status == StatusLoaded:
		switch m.serverState {
		case "RUNNING":
			statusStr = StyleBadgeLoaded.Render("● RUNNING")
		case "STARTING":
			statusStr = StyleBadgeDownload.Render("◌ STARTING")
		case "ERROR":
			statusStr = StyleBadgeStopped.Render("✕ ERROR")
		default:
			statusStr = StyleBadgeLoaded.Render("● LOADED")
		}
	default:
		statusStr = StyleBadgeAvail.Render("○ AVAILABLE")
	}

	// Field label width for alignment.
	const labelW = 15

	fieldLabel := func(label string) string {
		return StyleDim.Render(fmt.Sprintf("%-*s", labelW, label))
	}

	// Downloaded date.
	downloaded := lm.DownloadedAt.Format("2006-01-02")

	// indent is a styled two-space prefix used for all field rows.
	indent := StyleDim.Render("  ")

	// Core fields.
	fields := []string{
		indent + fieldLabel("Quantization") + StyleDim.Render(lm.Quant),
		indent + fieldLabel("Size") + StyleDim.Render(lm.SizeDisplay),
		indent + fieldLabel("Downloaded") + StyleDim.Render(downloaded),
		indent + fieldLabel("Status") + statusStr,
	}

	// Address row — only shown when server is running.
	if m.address != "" {
		fields = append(fields, indent+fieldLabel("Address")+StyleKey.Render(m.address))
	}

	// Path row.
	fields = append(fields, indent+fieldLabel("Path")+StyleDim.Render(truncatePath(lm.Path, w-labelW-6)))

	// GPU line — appended separately after fields so we can insert a blank line.
	var gpuLine string
	if m.gpuName != "" {
		gpu := StyleDim.Render(fmt.Sprintf("%s · Metal", m.gpuName))
		gpuLine = indent + fieldLabel("GPU") + gpu
	}

	// Action keys.
	keysLine := buildKeyHints([]keyHint{
		{"L", "Load"},
		{"U", "Unload"},
		{"C", "Chat"},
		{"Ctrl+D", "Delete"},
	})

	// panelDim is a helper for bare spaces/separators inside the panel.
	panelDim := func(s string) string { return StyleDim.Render(s) }

	parts := []string{
		titleLine,
		sep,
		panelDim("  ") + modelName,
		panelDim("  ") + innerSep,
		"",
	}
	parts = append(parts, fields...)
	// GPU line gets a blank separator before it.
	if gpuLine != "" {
		parts = append(parts, "", gpuLine)
	}
	parts = append(parts,
		panelDim("  ")+innerSep,
		"",
		panelDim("  ")+keysLine,
	)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// keyHint is a key+label pair for the hints bar.
type keyHint struct {
	key   string
	label string
}

// buildKeyHints renders a row of [Key] Label hints.
func buildKeyHints(hints []keyHint) string {
	sep := StyleDim.Render("    ")
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = StyleKey.Render("["+h.key+"]") + StyleDim.Render(" "+h.label)
	}
	return strings.Join(parts, sep)
}

// truncatePath shortens a path for display.
func truncatePath(path string, maxLen int) string {
	if maxLen <= 0 || len(path) <= maxLen {
		return path
	}
	return "…" + path[len(path)-maxLen+1:]
}
