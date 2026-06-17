package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/AresiusXP/llama-tui/internal/config"
	"github.com/AresiusXP/llama-tui/internal/hardware"
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
	fieldModelsDir     = iota
	fieldPort
	fieldContextSize
	fieldGPULayers
	fieldParallelSlots
	fieldKVCacheTypeK
	fieldKVCacheTypeV
	fieldMetrics // virtual field (bool toggle, not a textinput) — must be between KV fields and HFToken to match visual order
	fieldHFToken
	fieldTotal // total number of navigable field slots (text inputs + the metrics toggle)
)

// Valid KV cache type values accepted by llama-server.
var validKVCacheTypes = map[string]bool{
	"f32": true, "f16": true, "bf16": true,
	"q8_0": true, "q4_0": true, "q4_1": true,
	"iq4_nl": true, "q5_0": true, "q5_1": true,
}

// SettingsModel is the full-screen settings model.
type SettingsModel struct {
	cfg           *config.Config
	gpus          []hardware.GPU
	gpuCursor     int // currently highlighted GPU in the GPU list
	fields        []textinput.Model
	activeField   int  // which text field has focus
	inGPUList     bool // true when navigating the GPU list
	fieldsFocused bool // true when a text field is in editing mode
	metricsOn     bool // bool toggle for MetricsEnabled (not a textinput)
	width         int
	height        int
	saved         bool
	errMsg        string
	successMsg    string
}

// NewSettings constructs a SettingsModel pre-populated from cfg.
func NewSettings(cfg *config.Config, gpus []hardware.GPU, width, height int) SettingsModel {
	// fields is indexed by the field constants. The slot at fieldMetrics is
	// intentionally left as a zero-value textinput — it is never used; the
	// metrics toggle is handled separately via m.metricsOn.
	fields := make([]textinput.Model, fieldTotal)

	type fieldDef struct {
		placeholder string
		value       string
	}
	defs := map[int]fieldDef{
		fieldModelsDir:     {"/path/to/models", cfg.ModelsDir},
		fieldPort:          {"8080", strconv.Itoa(cfg.Server.Port)},
		fieldContextSize:   {"4096", strconv.Itoa(cfg.Server.ContextSize)},
		fieldGPULayers:     {"-1", strconv.Itoa(cfg.Server.GPULayers)},
		fieldParallelSlots: {"-1", strconv.Itoa(cfg.Server.ParallelSlots)},
		fieldKVCacheTypeK:  {"f16", cfg.Server.KVCacheTypeK},
		fieldKVCacheTypeV:  {"f16", cfg.Server.KVCacheTypeV},
		fieldHFToken:       {"hf_...", cfg.HuggingFace.Token},
	}

	for i := 0; i < fieldTotal; i++ {
		if i == fieldMetrics {
			continue // virtual toggle — no textinput
		}
		d := defs[i]
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = d.placeholder
		ti.SetValue(d.value)
		ti.Width = 40
		ti.PromptStyle = lipgloss.NewStyle().Background(lipgloss.Color(ColorBgPanel)).Foreground(lipgloss.Color(ColorTextDim))
		ti.TextStyle = lipgloss.NewStyle().Background(lipgloss.Color(ColorBgPanel)).Foreground(lipgloss.Color(ColorText))
		ti.PlaceholderStyle = lipgloss.NewStyle().Background(lipgloss.Color(ColorBgPanel)).Foreground(lipgloss.Color(ColorTextMuted))
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
		metricsOn:     cfg.Server.MetricsEnabled,
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
		// Note: fieldsFocused can only be true for real textinput fields (not the metrics toggle).
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
				m.activeField = (m.activeField + 1) % fieldTotal
				if m.activeField != fieldMetrics {
					m.fields[m.activeField].Focus()
					return m, textinput.Blink
				}
				// Landed on the metrics toggle — exit editing mode.
				m.fieldsFocused = false
				return m, nil

			case "shift+tab":
				m.fields[m.activeField].Blur()
				m.activeField = (m.activeField - 1 + fieldTotal) % fieldTotal
				if m.activeField != fieldMetrics {
					m.fields[m.activeField].Focus()
					return m, textinput.Blink
				}
				// Landed on the metrics toggle — exit editing mode.
				m.fieldsFocused = false
				return m, nil
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

		case "enter":
			// Enter toggles metrics when that field is active; otherwise re-enters editing mode.
			if m.activeField == fieldMetrics {
				m.metricsOn = !m.metricsOn
				return m, nil
			}
			m.fields[m.activeField].Focus()
			m.fieldsFocused = true
			return m, textinput.Blink

		case "tab":
			// Tab always navigates; never toggles.
			m.activeField = (m.activeField + 1) % fieldTotal
			if m.activeField != fieldMetrics {
				m.fields[m.activeField].Focus()
				m.fieldsFocused = true
				return m, textinput.Blink
			}
			return m, nil

		case " ":
			// Space always toggles the metrics field when it's active.
			if m.activeField == fieldMetrics {
				m.metricsOn = !m.metricsOn
			}
			return m, nil

		case "shift+tab":
			// Move to previous field.
			m.activeField = (m.activeField - 1 + fieldTotal) % fieldTotal
			if m.activeField != fieldMetrics {
				m.fields[m.activeField].Focus()
				m.fieldsFocused = true
				return m, textinput.Blink
			}
			return m, nil

		case "up", "k":
			m.activeField = (m.activeField - 1 + fieldTotal) % fieldTotal

		case "down", "j":
			m.activeField = (m.activeField + 1) % fieldTotal

		default:
			// Any printable key re-enters the active text field in editing mode.
			// Not applicable to the metrics toggle.
			if msg.Type == tea.KeyRunes && m.activeField != fieldMetrics {
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
	// Guard: only forward to textinput fields, not to the metrics toggle.
	if m.activeField != fieldMetrics {
		var cmd tea.Cmd
		m.fields[m.activeField], cmd = m.fields[m.activeField].Update(msg)
		return m, cmd
	}
	return m, nil
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
	parallelSlotsStr := strings.TrimSpace(m.fields[fieldParallelSlots].Value())
	kvCacheTypeK := strings.ToLower(strings.TrimSpace(m.fields[fieldKVCacheTypeK].Value()))
	kvCacheTypeV := strings.ToLower(strings.TrimSpace(m.fields[fieldKVCacheTypeV].Value()))
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

	parallelSlots, err := strconv.Atoi(parallelSlotsStr)
	if err != nil || (parallelSlots < 1 && parallelSlots != -1) {
		m.errMsg = "Parallel slots must be >= 1 or -1 (auto)"
		return m, nil
	}

	if kvCacheTypeK != "" && !validKVCacheTypes[kvCacheTypeK] {
		m.errMsg = "KV cache type K: use f32/f16/bf16/q8_0/q4_0/q4_1/iq4_nl/q5_0/q5_1 or leave empty"
		return m, nil
	}

	if kvCacheTypeV != "" && !validKVCacheTypes[kvCacheTypeV] {
		m.errMsg = "KV cache type V: use f32/f16/bf16/q8_0/q4_0/q4_1/iq4_nl/q5_0/q5_1 or leave empty"
		return m, nil
	}

	// Apply.
	m.cfg.ModelsDir = modelsDir
	m.cfg.Server.Port = port
	m.cfg.Server.ContextSize = ctx
	m.cfg.Server.GPULayers = gpuLayers
	m.cfg.Server.ParallelSlots = parallelSlots
	m.cfg.Server.KVCacheTypeK = kvCacheTypeK
	m.cfg.Server.KVCacheTypeV = kvCacheTypeV
	m.cfg.Server.MetricsEnabled = m.metricsOn
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
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true)

	divider := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorBorder)).
		Render(strings.Repeat("─", 42))

	labelStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorTextDim)).
		Width(22)

	activeLabelStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorAccent)).
		Width(22)

	gpuDotActive := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorGreen)).
		Render("●")

	gpuDotInactive := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorTextMuted)).
		Render("●")

	badgeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorAccent2)).
		Bold(true)

	keyStyle := StyleKey
	dimStyle := StyleDim

	// isActive returns true when the given field index is currently highlighted.
	isActive := func(i int) bool {
		return !m.inGPUList && m.activeField == i
	}

	lbl := func(i int, label string) string {
		if isActive(i) {
			return activeLabelStyle.Render(label)
		}
		return labelStyle.Render(label)
	}

	// ── field labels ──────────────────────────────────────────────────────
	// Use a map so indexing by field constants is safe despite the fieldMetrics hole.
	fieldLabels := map[int]string{
		fieldModelsDir:     "Models directory",
		fieldPort:          "Server port",
		fieldContextSize:   "Context size",
		fieldGPULayers:     "GPU layers",
		fieldParallelSlots: "Parallel slots",
		fieldKVCacheTypeK:  "KV cache type K",
		fieldKVCacheTypeV:  "KV cache type V",
		fieldHFToken:       "API token",
	}

	// ── content builder ───────────────────────────────────────────────────
	var b strings.Builder

	// General section — basic server fields.
	b.WriteString("\n")
	b.WriteString("  " + sectionHeader.Render("General") + "\n")
	b.WriteString("  " + divider + "\n")

	for _, i := range []int{fieldModelsDir, fieldPort, fieldContextSize, fieldGPULayers} {
		suffix := ""
		if i == fieldGPULayers && m.fields[i].Value() == "-1" {
			suffix = dimStyle.Render("  (auto)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n", lbl(i, fieldLabels[i]), m.fields[i].View(), suffix))
	}

	b.WriteString("\n")

	// Advanced section — parallel slots, KV cache, metrics.
	b.WriteString("  " + sectionHeader.Render("Server (advanced)") + "\n")
	b.WriteString("  " + divider + "\n")

	// Parallel slots.
	{
		suffix := ""
		if m.fields[fieldParallelSlots].Value() == "-1" {
			suffix = dimStyle.Render("  (auto)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n",
			lbl(fieldParallelSlots, fieldLabels[fieldParallelSlots]),
			m.fields[fieldParallelSlots].View(), suffix))
	}

	// KV cache type K.
	{
		suffix := ""
		if m.fields[fieldKVCacheTypeK].Value() == "" {
			suffix = dimStyle.Render("  (default: f16)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n",
			lbl(fieldKVCacheTypeK, fieldLabels[fieldKVCacheTypeK]),
			m.fields[fieldKVCacheTypeK].View(), suffix))
	}

	// KV cache type V.
	{
		suffix := ""
		if m.fields[fieldKVCacheTypeV].Value() == "" {
			suffix = dimStyle.Render("  (default: f16)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n",
			lbl(fieldKVCacheTypeV, fieldLabels[fieldKVCacheTypeV]),
			m.fields[fieldKVCacheTypeV].View(), suffix))
	}

	// Metrics endpoint toggle.
	{
		metLbl := lbl(fieldMetrics, "Metrics endpoint")
		var toggleStr string
		if m.metricsOn {
			toggleStr = lipgloss.NewStyle().Background(lipgloss.Color(ColorBgPanel)).Foreground(lipgloss.Color(ColorGreen)).Render("[✓] enabled")
		} else {
			toggleStr = dimStyle.Render("[ ] disabled")
		}
		hint := dimStyle.Render("  (Space/Enter to toggle)")
		b.WriteString(fmt.Sprintf("  %s %s%s\n", metLbl, toggleStr, hint))
	}

	b.WriteString("\n")

	// HuggingFace section.
	b.WriteString("  " + sectionHeader.Render("HuggingFace") + "\n")
	b.WriteString("  " + divider + "\n")
	{
		i := fieldHFToken
		suffix := ""
		if m.fields[i].Value() != "" {
			suffix = dimStyle.Render("  (masked)")
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n", lbl(i, fieldLabels[i]), m.fields[i].View(), suffix))
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
					Background(lipgloss.Color(ColorBgPanel)).
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
		lipgloss.WithWhitespaceBackground(lipgloss.Color(ColorBg)),
	)
}
