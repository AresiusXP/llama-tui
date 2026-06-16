package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriciodanos/llama-tui/internal/config"
	"github.com/patriciodanos/llama-tui/internal/hardware"
)

// CloseSettingsMsg is sent when the user closes settings (Esc or saves).
type CloseSettingsMsg struct {
	Saved bool
}

// GPUsDetectedMsg carries freshly-detected GPUs.
type GPUsDetectedMsg struct {
	GPUs []hardware.GPU
}

// Field indices (for fields slice).
const (
	fieldModelsDir  = iota
	fieldPort
	fieldContextSize
	fieldGPULayers
	fieldHFToken
	fieldCount // sentinel
)

// SettingsModel is the full-screen settings model.
type SettingsModel struct {
	cfg          *config.Config
	gpus         []hardware.GPU
	gpuCursor    int // currently highlighted GPU in the GPU list
	fields       []textinput.Model
	activeField  int  // which text field has focus
	inGPUList    bool // true when navigating the GPU list
	fieldsFocused bool // true when a text field is in editing mode
	width        int
	height       int
	saved        bool
	errMsg       string
	successMsg   string
}

// NewSettings constructs a SettingsModel pre-populated from cfg.
func NewSettings(cfg *config.Config, gpus []hardware.GPU, width, height int) SettingsModel {
	fields := make([]textinput.Model, fieldCount)

	labels := []string{
		"Models directory",
		"Server port",
		"Context size",
		"GPU layers",
		"HF API token",
	}
	placeholders := []string{
		"/path/to/models",
		"8080",
		"4096",
		"-1",
		"hf_...",
	}
	values := []string{
		cfg.ModelsDir,
		strconv.Itoa(cfg.Server.Port),
		strconv.Itoa(cfg.Server.ContextSize),
		strconv.Itoa(cfg.Server.GPULayers),
		cfg.HuggingFace.Token,
	}

	for i := 0; i < fieldCount; i++ {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholders[i]
		ti.SetValue(values[i])
		ti.Width = 40
		ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorTextDim))
		ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText))
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorTextMuted))
		_ = labels[i] // used in View
		fields[i] = ti
	}

	// Mask the HF token field.
	fields[fieldHFToken].EchoMode = textinput.EchoPassword
	fields[fieldHFToken].EchoCharacter = '●'

	// Focus the first field.
	fields[fieldModelsDir].Focus()

	// Bounds-check gpuCursor: stored value may be stale if GPU hardware changed.
	gpuCursor := cfg.Server.SelectedGPUIndex
	if len(gpus) == 0 || gpuCursor >= len(gpus) {
		gpuCursor = 0
	}

	return SettingsModel{
		cfg:           cfg,
		gpus:          gpus,
		gpuCursor:     gpuCursor,
		fields:        fields,
		activeField:   fieldModelsDir,
		fieldsFocused: true, // start in editing mode on the first field
		width:         width,
		height:        height,
	}
}

// SetSize updates the terminal dimensions.
func (m *SettingsModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init starts GPU detection in the background.
func (m SettingsModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		detectGPUsCmd(),
	)
}

// detectGPUsCmd runs GPU detection as a Bubble Tea command.
func detectGPUsCmd() tea.Cmd {
	return func() tea.Msg {
		return GPUsDetectedMsg{GPUs: hardware.DetectGPUs()}
	}
}

// closeAfterDelay sends CloseSettingsMsg{Saved:true} after a short delay.
func closeAfterDelay() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return CloseSettingsMsg{Saved: true}
	}
}

// Update handles key events and messages.
func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case GPUsDetectedMsg:
		m.gpus = msg.GPUs
		if m.gpuCursor >= len(m.gpus) && len(m.gpus) > 0 {
			m.gpuCursor = 0
		}
		return m, nil

	case tea.KeyMsg:
		// ── GPU list navigation (always takes priority) ──────────────────
		if m.inGPUList {
			switch msg.String() {
			case "up", "k":
				if m.gpuCursor > 0 {
					m.gpuCursor--
				}
			case "down", "j":
				if m.gpuCursor < len(m.gpus)-1 {
					m.gpuCursor++
				}
			case "enter":
				m.cfg.Server.SelectedGPUIndex = m.gpuCursor
			case "esc":
				m.inGPUList = false
			}
			return m, nil
		}

		// ── Field-editing mode ────────────────────────────────────────────
		// When fieldsFocused=true the user is typing in a text field.
		// Only Tab, Shift+Tab, Ctrl+S, and Esc are intercepted.
		// All other keys go straight to the active field.
		if m.fieldsFocused {
			switch msg.String() {
			case "esc":
				// Blur the active field → enter command mode (don't close yet).
				m.fields[m.activeField].Blur()
				m.fieldsFocused = false
				return m, nil

			case "ctrl+s":
				return m.save()

			case "tab":
				m.fields[m.activeField].Blur()
				m.activeField = (m.activeField + 1) % fieldCount
				m.fields[m.activeField].Focus()
				return m, textinput.Blink

			case "shift+tab":
				m.fields[m.activeField].Blur()
				m.activeField = (m.activeField - 1 + fieldCount) % fieldCount
				m.fields[m.activeField].Focus()
				return m, textinput.Blink
			}

			// All other keys go to the active text field.
			var cmd tea.Cmd
			m.fields[m.activeField], cmd = m.fields[m.activeField].Update(msg)
			return m, cmd
		}

		// ── Command mode (fieldsFocused=false) ────────────────────────────
		// All single-letter shortcuts are safe here — no field is active.
		switch msg.String() {
		case "esc":
			// Esc in command mode closes settings.
			return m, func() tea.Msg { return CloseSettingsMsg{Saved: false} }

		case "s", "ctrl+s":
			return m.save()

		case "r":
			return m, detectGPUsCmd()

		case "g":
			if len(m.gpus) > 0 {
				m.inGPUList = true
			}
			return m, nil

		case "tab", "enter":
			// Re-enter editing mode on the current field.
			m.fields[m.activeField].Focus()
			m.fieldsFocused = true
			return m, textinput.Blink

		case "shift+tab":
			// Move to previous field and enter editing mode.
			m.activeField = (m.activeField - 1 + fieldCount) % fieldCount
			m.fields[m.activeField].Focus()
			m.fieldsFocused = true
			return m, textinput.Blink

		case "up", "k":
			m.activeField = (m.activeField - 1 + fieldCount) % fieldCount

		case "down", "j":
			m.activeField = (m.activeField + 1) % fieldCount

		default:
			// Any printable key re-enters the active field in editing mode.
			if msg.Type == tea.KeyRunes {
				m.fields[m.activeField].Focus()
				m.fieldsFocused = true
				var cmd tea.Cmd
				m.fields[m.activeField], cmd = m.fields[m.activeField].Update(msg)
				return m, tea.Batch(cmd, textinput.Blink)
			}
		}
		return m, nil
	}

	// Pass non-key messages to the active text field (e.g. blink ticks).
	var cmd tea.Cmd
	m.fields[m.activeField], cmd = m.fields[m.activeField].Update(msg)
	return m, cmd
}

// blurField removes focus from a field.
func (m *SettingsModel) blurField(i int) {
	m.fields[i].Blur()
}

// save validates inputs, updates cfg, persists, and schedules close.
func (m SettingsModel) save() (tea.Model, tea.Cmd) {
	m.errMsg = ""
	m.successMsg = ""

	modelsDir := strings.TrimSpace(m.fields[fieldModelsDir].Value())
	portStr := strings.TrimSpace(m.fields[fieldPort].Value())
	ctxStr := strings.TrimSpace(m.fields[fieldContextSize].Value())
	gpuLayersStr := strings.TrimSpace(m.fields[fieldGPULayers].Value())
	hfToken := m.fields[fieldHFToken].Value()

	// Validate.
	if modelsDir == "" {
		m.errMsg = "Models directory must not be empty"
		return m, nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		m.errMsg = "Port must be between 1 and 65535"
		return m, nil
	}

	ctx, err := strconv.Atoi(ctxStr)
	if err != nil || ctx <= 0 {
		m.errMsg = "Context size must be greater than 0"
		return m, nil
	}

	gpuLayers, err := strconv.Atoi(gpuLayersStr)
	if err != nil || gpuLayers < -1 {
		m.errMsg = "GPU layers must be >= -1 (-1 = auto)"
		return m, nil
	}

	// Apply.
	m.cfg.ModelsDir = modelsDir
	m.cfg.Server.Port = port
	m.cfg.Server.ContextSize = ctx
	m.cfg.Server.GPULayers = gpuLayers
	m.cfg.HuggingFace.Token = hfToken

	if err := m.cfg.Save(); err != nil {
		m.errMsg = fmt.Sprintf("Save failed: %v", err)
		return m, nil
	}

	m.successMsg = "Settings saved."
	m.saved = true
	return m, closeAfterDelay()
}

// View renders the full-screen settings panel.
func (m SettingsModel) View() string {
	// ── styles ────────────────────────────────────────────────────────────
	sectionHeader := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true)

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorBorder)).
		Render(strings.Repeat("─", 42))

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim)).
		Width(22)

	activeLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Width(22)

	gpuDotActive := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGreen)).
		Render("●")

	gpuDotInactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextMuted)).
		Render("●")

	badgeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent2)).
		Bold(true)

	keyStyle := StyleKey
	dimStyle := StyleDim

	// ── field labels ──────────────────────────────────────────────────────
	fieldLabels := []string{
		"Models directory",
		"Server port",
		"Context size",
		"GPU layers",
		"API token",
	}

	// ── content builder ───────────────────────────────────────────────────
	var b strings.Builder

	// General section.
	b.WriteString("\n")
	b.WriteString("  " + sectionHeader.Render("General") + "\n")
	b.WriteString("  " + divider + "\n")

	for i := 0; i < fieldHFToken; i++ {
		lbl := labelStyle.Render(fieldLabels[i])
		if !m.inGPUList && m.activeField == i {
			lbl = activeLabelStyle.Render(fieldLabels[i])
		}
		suffix := ""
		if i == fieldGPULayers && m.fields[i].Value() == "-1" {
			suffix = dimStyle.Render("  (auto)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n", lbl, m.fields[i].View(), suffix))
	}

	b.WriteString("\n")

	// HuggingFace section.
	b.WriteString("  " + sectionHeader.Render("HuggingFace") + "\n")
	b.WriteString("  " + divider + "\n")
	{
		i := fieldHFToken
		lbl := labelStyle.Render(fieldLabels[i])
		if !m.inGPUList && m.activeField == i {
			lbl = activeLabelStyle.Render(fieldLabels[i])
		}
		suffix := ""
		if m.fields[i].Value() != "" {
			suffix = dimStyle.Render("  (masked)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n", lbl, m.fields[i].View(), suffix))
	}

	b.WriteString("\n")

	// GPU Selection section.
	b.WriteString("  " + sectionHeader.Render("GPU Selection") + "\n")
	b.WriteString("  " + divider + "\n")

	if len(m.gpus) == 0 {
		b.WriteString("  " + dimStyle.Render("No GPUs detected — press [R] to refresh") + "\n")
	} else {
		for i, gpu := range m.gpus {
			cursor := "  "
			if m.inGPUList && m.gpuCursor == i {
				cursor = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ColorAccent)).
					Render("▸ ")
			}

			dot := gpuDotInactive
			if m.cfg.Server.SelectedGPUIndex == i {
				dot = gpuDotActive
			}

			name := gpu.Name
			if gpu.VRAM != "" {
				name = fmt.Sprintf("%s (%s)", gpu.Name, gpu.VRAM)
			}

			badge := ""
			if m.cfg.Server.SelectedGPUIndex == i {
				badge = "  " + badgeStyle.Render("[default]")
			}

			b.WriteString(fmt.Sprintf("  %s%s %s%s\n", cursor, dot, name, badge))
		}
	}

	b.WriteString("\n")

	// Status messages.
	if m.errMsg != "" {
		b.WriteString("  " + StyleError.Render("  "+m.errMsg) + "\n\n")
	} else if m.successMsg != "" {
		b.WriteString("  " + StyleSuccess.Render("  "+m.successMsg) + "\n\n")
	} else {
		b.WriteString("\n")
	}

	// Footer hint — changes based on current mode.
	var footer string
	if m.inGPUList {
		footer = fmt.Sprintf("  %s",
			dimStyle.Render("[↑↓] navigate  [Enter] select  [Esc] back"),
		)
	} else if m.fieldsFocused {
		// Editing mode: only safe shortcuts shown.
		footer = fmt.Sprintf(
			"  %s Save  %s Next field  %s Stop editing",
			keyStyle.Render("[Ctrl+S]"),
			keyStyle.Render("[Tab]"),
			keyStyle.Render("[Esc]"),
		)
	} else {
		// Command mode: all shortcuts available.
		footer = fmt.Sprintf(
			"  %s Save  %s Refresh GPUs  %s GPU list  %s Close",
			keyStyle.Render("[S]"),
			keyStyle.Render("[R]"),
			keyStyle.Render("[G]"),
			keyStyle.Render("[Esc]"),
		)
		footer += "  " + dimStyle.Render("(press any key or Enter to edit)")
	}
	b.WriteString(footer + "\n")

	content := b.String()

	// ── outer panel ───────────────────────────────────────────────────────
	panelWidth := m.width - 4
	if panelWidth < 60 {
		panelWidth = 60
	}

	panelStyle := StylePanel.
		Width(panelWidth).
		Padding(0, 1)

	title := StyleTitle.Render("Settings")
	panel := panelStyle.Render(
		"\n" + "  " + title + "\n" + content,
	)

	// Centre the panel on screen.
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		panel,
	)
}
