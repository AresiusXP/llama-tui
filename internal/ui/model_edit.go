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
)

// OpenModelEditMsg is sent by DetailModel when the user presses [E].
type OpenModelEditMsg struct {
	Model LocalModel
}

// CloseModelEditMsg is sent when the user saves or cancels the model-edit panel.
// If Saved is true, Override contains the updated ModelConfig to persist.
type CloseModelEditMsg struct {
	Saved    bool
	Name     string // model filename (key for overrides map)
	Override config.ModelConfig
}

// GlobalDefaults carries the global server settings so the editor can show
// "(global: X)" placeholder hints in each field.
type GlobalDefaults struct {
	ContextSize   int
	GPULayers     int
	ParallelSlots int
	KVCacheTypeK  string
	KVCacheTypeV  string
}

// Model-edit field indices.
const (
	meFieldContextSize   = iota
	meFieldGPULayers
	meFieldParallelSlots
	meFieldKVCacheTypeK
	meFieldKVCacheTypeV
	meFieldThreads
	meFieldBatchSize
	meFieldTotal
)

// ModelEditModel is the Bubble Tea model for editing per-model server config.
// It is displayed in the bottom-right panel in place of DetailModel.
type ModelEditModel struct {
	modelName   string // filename — key into overrides map
	override    config.ModelConfig
	globals     GlobalDefaults
	fields      []textinput.Model
	activeField int
	focused     bool
	errMsg      string
	successMsg  string
	width       int
	height      int
}

// NewModelEdit constructs a ModelEditModel for the given model, pre-filled with
// any existing override values and global defaults for hint display.
func NewModelEdit(model LocalModel, override config.ModelConfig, globals GlobalDefaults, width, height int) ModelEditModel {
	fields := make([]textinput.Model, meFieldTotal)

	// Helper to create a styled textinput.
	newTI := func(placeholder, value string) textinput.Model {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholder
		ti.SetValue(value)
		ti.Width = 36
		ti.PromptStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(ColorBgPanel)).
			Foreground(lipgloss.Color(ColorTextDim))
		ti.TextStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(ColorBgPanel)).
			Foreground(lipgloss.Color(ColorText))
		ti.PlaceholderStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(ColorBgPanel)).
			Foreground(lipgloss.Color(ColorTextMuted))
		return ti
	}

	// Context size — 0 stored as "" (empty = use global).
	ctxVal := ""
	if override.ContextSize > 0 {
		ctxVal = strconv.Itoa(override.ContextSize)
	}
	fields[meFieldContextSize] = newTI(
		fmt.Sprintf("global: %d", globals.ContextSize),
		ctxVal,
	)

	// GPU layers — nil stored as "" (empty = use global).
	gpuVal := ""
	if override.GPULayers != nil {
		gpuVal = strconv.Itoa(*override.GPULayers)
	}
	fields[meFieldGPULayers] = newTI(
		fmt.Sprintf("global: %d", globals.GPULayers),
		gpuVal,
	)

	// Parallel slots — 0 stored as "" (empty = use global).
	parallelVal := ""
	if override.ParallelSlots != 0 {
		parallelVal = strconv.Itoa(override.ParallelSlots)
	}
	fields[meFieldParallelSlots] = newTI(
		fmt.Sprintf("global: %d", globals.ParallelSlots),
		parallelVal,
	)

	// KV cache types.
	kvKHint := "global: f16"
	if globals.KVCacheTypeK != "" {
		kvKHint = "global: " + globals.KVCacheTypeK
	}
	fields[meFieldKVCacheTypeK] = newTI(kvKHint, override.KVCacheTypeK)

	kvVHint := "global: f16"
	if globals.KVCacheTypeV != "" {
		kvVHint = "global: " + globals.KVCacheTypeV
	}
	fields[meFieldKVCacheTypeV] = newTI(kvVHint, override.KVCacheTypeV)

	// Threads — 0 stored as "" (empty = omit flag).
	threadsVal := ""
	if override.Threads > 0 {
		threadsVal = strconv.Itoa(override.Threads)
	}
	fields[meFieldThreads] = newTI("empty = omit", threadsVal)

	// BatchSize — 0 stored as "" (empty = omit flag, server default 2048).
	batchVal := ""
	if override.BatchSize > 0 {
		batchVal = strconv.Itoa(override.BatchSize)
	}
	fields[meFieldBatchSize] = newTI("empty = server default (2048)", batchVal)

	// Focus first field.
	fields[meFieldContextSize].Focus()

	return ModelEditModel{
		modelName:   model.Name,
		override:    override,
		globals:     globals,
		fields:      fields,
		activeField: meFieldContextSize,
		focused:     true,
		width:       width,
		height:      height,
	}
}

// SetSize updates the panel dimensions.
func (m *ModelEditModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init implements tea.Model.
func (m ModelEditModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m ModelEditModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While a save is in progress (successMsg set, waiting for the close delay),
	// ignore all key input to prevent Esc from cancelling the already-committed save.
	if m.successMsg != "" {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg {
				return CloseModelEditMsg{Saved: false}
			}

		case "ctrl+s":
			return m.save()

		case "tab":
			m.fields[m.activeField].Blur()
			m.activeField = (m.activeField + 1) % meFieldTotal
			m.fields[m.activeField].Focus()
			return m, textinput.Blink

		case "shift+tab":
			m.fields[m.activeField].Blur()
			m.activeField = (m.activeField - 1 + meFieldTotal) % meFieldTotal
			m.fields[m.activeField].Focus()
			return m, textinput.Blink

		default:
			// All other keys go to the active text field.
			var cmd tea.Cmd
			m.fields[m.activeField], cmd = m.fields[m.activeField].Update(msg)
			return m, cmd
		}
	}

	// Forward non-key messages (e.g. blink tick) to the active field.
	var cmd tea.Cmd
	m.fields[m.activeField], cmd = m.fields[m.activeField].Update(msg)
	return m, cmd
}

// meCloseAfterDelay sends CloseModelEditMsg{Saved:true} after a brief pause
// so the user can see the "Saved." confirmation message.
func meCloseAfterDelay(name string, ov config.ModelConfig) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(400 * time.Millisecond)
		return CloseModelEditMsg{Saved: true, Name: name, Override: ov}
	}
}

// save validates, builds the ModelConfig, and schedules a close message.
func (m ModelEditModel) save() (tea.Model, tea.Cmd) {
	m.errMsg = ""
	m.successMsg = ""

	rawCtx := strings.TrimSpace(m.fields[meFieldContextSize].Value())
	rawGPU := strings.TrimSpace(m.fields[meFieldGPULayers].Value())
	rawParallel := strings.TrimSpace(m.fields[meFieldParallelSlots].Value())
	rawKVK := strings.ToLower(strings.TrimSpace(m.fields[meFieldKVCacheTypeK].Value()))
	rawKVV := strings.ToLower(strings.TrimSpace(m.fields[meFieldKVCacheTypeV].Value()))
	rawThreads := strings.TrimSpace(m.fields[meFieldThreads].Value())
	rawBatch := strings.TrimSpace(m.fields[meFieldBatchSize].Value())

	var ov config.ModelConfig

	// Context size.
	if rawCtx != "" {
		v, err := strconv.Atoi(rawCtx)
		if err != nil || v <= 0 {
			m.errMsg = "Context size must be > 0 or empty (use global)"
			return m, nil
		}
		ov.ContextSize = v
	}

	// GPU layers — allow -1 (auto) explicitly.
	if rawGPU != "" {
		v, err := strconv.Atoi(rawGPU)
		if err != nil || v < -1 {
			m.errMsg = "GPU layers must be >= -1 (-1 = auto) or empty (use global)"
			return m, nil
		}
		ov.GPULayers = &v
	}

	// Parallel slots.
	if rawParallel != "" {
		v, err := strconv.Atoi(rawParallel)
		if err != nil || (v < 1 && v != -1) {
			m.errMsg = "Parallel slots must be >= 1 or -1 (auto) or empty (use global)"
			return m, nil
		}
		ov.ParallelSlots = v
	}

	// KV cache types.
	if rawKVK != "" && !validKVCacheTypes[rawKVK] {
		m.errMsg = "KV cache K: use f32/f16/bf16/q8_0/q4_0/q4_1/iq4_nl/q5_0/q5_1 or empty"
		return m, nil
	}
	ov.KVCacheTypeK = rawKVK

	if rawKVV != "" && !validKVCacheTypes[rawKVV] {
		m.errMsg = "KV cache V: use f32/f16/bf16/q8_0/q4_0/q4_1/iq4_nl/q5_0/q5_1 or empty"
		return m, nil
	}
	ov.KVCacheTypeV = rawKVV

	// Threads.
	if rawThreads != "" {
		v, err := strconv.Atoi(rawThreads)
		if err != nil || v < 1 {
			m.errMsg = "Threads must be >= 1 or empty (omit flag)"
			return m, nil
		}
		ov.Threads = v
	}

	// Batch size.
	if rawBatch != "" {
		v, err := strconv.Atoi(rawBatch)
		if err != nil || v < 1 {
			m.errMsg = "Batch size must be >= 1 or empty (use server default)"
			return m, nil
		}
		ov.BatchSize = v
	}

	m.successMsg = "Saved."
	return m, meCloseAfterDelay(m.modelName, ov)
}

// View renders the model-edit panel content (without its own border —
// RenderFrame provides the border for the bottom-right slot).
func (m ModelEditModel) View() string {
	w := m.width
	if w < 1 {
		w = 40
	}

	titleLine := StylePanelTitle.Render("MODEL CONFIG")
	sep := StyleDim.Render(strings.Repeat("─", w))

	// Header: truncate long model names.
	modelNameStr := m.modelName
	maxNameW := w - 4
	if len(modelNameStr) > maxNameW && maxNameW > 3 {
		modelNameStr = "…" + modelNameStr[len(modelNameStr)-maxNameW+1:]
	}
	nameLine := StyleBold.Render(modelNameStr)

	const labelW = 18

	lbl := func(i int, label string) string {
		if m.activeField == i {
			return lipgloss.NewStyle().
				Background(lipgloss.Color(ColorBgPanel)).
				Foreground(lipgloss.Color(ColorAccent)).
				Width(labelW).
				Render(label)
		}
		return lipgloss.NewStyle().
			Background(lipgloss.Color(ColorBgPanel)).
			Foreground(lipgloss.Color(ColorTextDim)).
			Width(labelW).
			Render(label)
	}

	fieldLabels := map[int]string{
		meFieldContextSize:   "Context size",
		meFieldGPULayers:     "GPU layers",
		meFieldParallelSlots: "Parallel slots",
		meFieldKVCacheTypeK:  "KV cache type K",
		meFieldKVCacheTypeV:  "KV cache type V",
		meFieldThreads:       "Threads",
		meFieldBatchSize:     "Batch size",
	}

	suffix := func(i int) string {
		v := m.fields[i].Value()
		switch i {
		case meFieldContextSize:
			if v == "" {
				return StyleDim.Render(fmt.Sprintf("  (global: %d)", m.globals.ContextSize))
			}
		case meFieldGPULayers:
			if v == "" {
				return StyleDim.Render(fmt.Sprintf("  (global: %d)", m.globals.GPULayers))
			} else if v == "-1" {
				return StyleDim.Render("  (auto)")
			}
		case meFieldParallelSlots:
			if v == "" {
				return StyleDim.Render(fmt.Sprintf("  (global: %d)", m.globals.ParallelSlots))
			} else if v == "-1" {
				return StyleDim.Render("  (auto)")
			}
		case meFieldKVCacheTypeK:
			if v == "" {
				hint := m.globals.KVCacheTypeK
				if hint == "" {
					hint = "f16"
				}
				return StyleDim.Render(fmt.Sprintf("  (global: %s)", hint))
			}
		case meFieldKVCacheTypeV:
			if v == "" {
				hint := m.globals.KVCacheTypeV
				if hint == "" {
					hint = "f16"
				}
				return StyleDim.Render(fmt.Sprintf("  (global: %s)", hint))
			}
		case meFieldThreads:
			if v == "" {
				return StyleDim.Render("  (omit flag)")
			}
		case meFieldBatchSize:
			if v == "" {
				return StyleDim.Render("  (server default: 2048)")
			}
		}
		return ""
	}

	var lines []string
	lines = append(lines,
		titleLine,
		sep,
		StyleDim.Render("  ")+nameLine,
		StyleDim.Render("  ")+StyleDim.Render(strings.Repeat("─", w-4)),
		"",
	)

	for i := 0; i < meFieldTotal; i++ {
		row := fmt.Sprintf("  %s %s%s", lbl(i, fieldLabels[i]), m.fields[i].View(), suffix(i))
		lines = append(lines, row)
	}

	lines = append(lines, "")

	// Status messages.
	if m.errMsg != "" {
		lines = append(lines, StyleError.Render("  "+m.errMsg))
	} else if m.successMsg != "" {
		lines = append(lines, StyleSuccess.Render("  "+m.successMsg))
	} else {
		lines = append(lines, "")
	}

	lines = append(lines,
		"",
		"  "+buildKeyHints([]keyHint{
			{"Ctrl+S", "Save"},
			{"Tab", "Next field"},
			{"Esc", "Cancel"},
		}),
	)

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
