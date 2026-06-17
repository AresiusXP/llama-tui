package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ModelSelectedMsg is sent when the user selects (highlights) a model.
type ModelSelectedMsg struct {
	Model LocalModel
}

// ModelLoadRequestMsg is sent when the user presses Enter/L to load a model.
type ModelLoadRequestMsg struct {
	Model LocalModel
}

// ModelUnloadRequestMsg is sent when the user presses U.
type ModelUnloadRequestMsg struct{}

// ModelDeleteRequestMsg is sent when the user presses Del.
type ModelDeleteRequestMsg struct {
	Model LocalModel
}

// CancelDownloadMsg is sent when the user presses X on a downloading model.
type CancelDownloadMsg struct {
	Filename string
	Path     string
}

// ResumeDownloadMsg is sent when the user presses X on a paused model.
type ResumeDownloadMsg struct {
	RepoID         string
	RemoteFilename string
	LocalPath      string
}

// OpenSearchMsg is sent when user presses D/d (open download/search).
type OpenSearchMsg struct{}

// LibraryModel is the Bubble Tea model for the left panel.
type LibraryModel struct {
	models       []LocalModel
	cursor       int
	focused      bool
	width        int
	height       int
	modelsDir    string
	activeModel  string // path of the currently loaded model
	spinnerFrame int    // cycles through spinnerFrames for downloading items
}

// NewLibrary creates a new LibraryModel.
func NewLibrary(modelsDir string, width, height int) LibraryModel {
	return LibraryModel{
		modelsDir: modelsDir,
		width:     width,
		height:    height,
	}
}

// SetFocus sets whether this panel has keyboard focus.
func (m *LibraryModel) SetFocus(focused bool) {
	m.focused = focused
}

// SetSize updates panel dimensions.
func (m *LibraryModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetActiveModel marks which model is currently loaded.
func (m *LibraryModel) SetActiveModel(path string) {
	m.activeModel = path
	// Update status of models to reflect currently loaded one.
	for i := range m.models {
		if m.models[i].Path == path {
			m.models[i].Status = StatusLoaded
		} else if m.models[i].Status == StatusLoaded {
			m.models[i].Status = StatusAvailable
		}
	}
}

// localShardPattern matches split GGUF filenames like "model-00001-of-00003.gguf".
// Capture group 2 is the shard index.
var localShardPattern = regexp.MustCompile(`(?i)-(\d{5})-of-\d{5}\.gguf$`)

// isNonFirstShard returns true for shard files whose index is > 1.
// Such files are auto-downloaded alongside shard 1 and must not appear as
// standalone loadable models in the library.
func isNonFirstShard(name string) bool {
	m := localShardPattern.FindStringSubmatch(name)
	if m == nil {
		return false
	}
	idx, _ := strconv.Atoi(m[1])
	return idx > 1
}

// Refresh re-scans modelsDir for .gguf files.
func (m *LibraryModel) Refresh() error {
	entries, err := os.ReadDir(m.modelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			m.models = nil
			return nil
		}
		return err
	}

	// Build a map of existing models by path so we can preserve status.
	existing := make(map[string]LocalModel, len(m.models))
	for _, lm := range m.models {
		existing[lm.Path] = lm
	}

	var models []LocalModel
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
			continue
		}
		// Non-first shards (e.g. model-00002-of-00002.gguf) are downloaded
		// automatically alongside shard 1 and are not loadable on their own.
		if isNonFirstShard(name) {
			continue
		}

		fullPath := filepath.Join(m.modelsDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		status := StatusAvailable
		if fullPath == m.activeModel {
			status = StatusLoaded
		}

		// Preserve downloading status from existing model.
		if prev, ok := existing[fullPath]; ok && prev.Status == StatusDownloading {
			status = StatusDownloading
		}

		lm := LocalModel{
			Name:         name,
			Path:         fullPath,
			SizeBytes:    info.Size(),
			SizeDisplay:  FormatSize(info.Size()),
			Quant:        ParseQuant(name),
			Status:       status,
			DownloadedAt: info.ModTime(),
		}

		// Preserve progress if downloading.
		if prev, ok := existing[fullPath]; ok {
			lm.Progress = prev.Progress
		}

		models = append(models, lm)
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})

	m.models = models

	// Clamp cursor.
	if m.cursor >= len(m.models) {
		m.cursor = max(0, len(m.models)-1)
	}

	return nil
}

// SelectedModel returns the currently highlighted model (nil if empty).
func (m LibraryModel) SelectedModel() *LocalModel {
	if len(m.models) == 0 || m.cursor < 0 || m.cursor >= len(m.models) {
		return nil
	}
	lm := m.models[m.cursor]
	return &lm
}

// Init implements tea.Model.
func (m LibraryModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m LibraryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Tick the spinner on every DownloadProgressMsg regardless of focus.
	if _, ok := msg.(DownloadProgressMsg); ok {
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
	}

	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if sel := m.SelectedModel(); sel != nil {
					return m, func() tea.Msg { return ModelSelectedMsg{Model: *sel} }
				}
			}

		case "down", "j":
			if m.cursor < len(m.models)-1 {
				m.cursor++
				if sel := m.SelectedModel(); sel != nil {
					return m, func() tea.Msg { return ModelSelectedMsg{Model: *sel} }
				}
			}

		case "enter", "l":
			if sel := m.SelectedModel(); sel != nil {
				return m, func() tea.Msg { return ModelLoadRequestMsg{Model: *sel} }
			}

		case "u":
			return m, func() tea.Msg { return ModelUnloadRequestMsg{} }

		case "d", "D":
			return m, func() tea.Msg { return OpenSearchMsg{} }

		case "x", "X":
			if sel := m.SelectedModel(); sel != nil {
				lm := *sel
				switch lm.Status {
				case StatusDownloading:
					return m, func() tea.Msg {
						return CancelDownloadMsg{Filename: lm.Name, Path: lm.Path}
					}
				case StatusPaused:
					return m, func() tea.Msg {
						return ResumeDownloadMsg{
							RepoID:         lm.RepoID,
							RemoteFilename: lm.RemoteFilename,
							LocalPath:      lm.Path,
						}
					}
				}
			}

		case "ctrl+d":
			if sel := m.SelectedModel(); sel != nil {
				return m, func() tea.Msg { return ModelDeleteRequestMsg{Model: *sel} }
			}
		}
	}

	return m, nil
}

// spinnerFrames used when a model is downloading.
var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// View renders the library panel content.
func (m LibraryModel) View() string {
	titleLine := StylePanelTitle.Render("LOCAL MODELS")
	sep := StyleDim.Render(strings.Repeat("─", m.width))

	if len(m.models) == 0 {
		empty := StyleDim.Render("No models found\nPress [d] to download")
		return lipgloss.JoinVertical(lipgloss.Left, titleLine, sep, "", empty)
	}

	// Calculate how many rows we can display.
	// Title takes 1 line + separator 1 line + 1 blank line = 3 overhead.
	const overhead = 3
	visibleRows := m.height - overhead
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Determine scroll window.
	start := 0
	if m.cursor >= visibleRows {
		start = m.cursor - visibleRows + 1
	}
	end := start + visibleRows
	if end > len(m.models) {
		end = len(m.models)
	}

	var rows []string
	for i := start; i < end; i++ {
		lm := m.models[i]
		row := renderModelRow(lm, i == m.cursor, m.width, m.spinnerFrame)
		rows = append(rows, row)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.JoinVertical(lipgloss.Left, titleLine, sep, "", content)
}

// renderModelRow renders a single model list entry.
// spinnerFrame is used to animate the download spinner.
func renderModelRow(lm LocalModel, selected bool, width, spinnerFrame int) string {
	// Choose background for all row elements — selected rows use ColorBgSelected
	// so every styled token in the row matches the highlight background.
	rowBg := lipgloss.Color(ColorBgPanel)
	if selected {
		rowBg = lipgloss.Color(ColorBgSelected)
	}

	// Per-row style copies so every token has an explicit background.
	badgeLoaded := StyleBadgeLoaded.Copy().Background(rowBg)
	badgeDownload := StyleBadgeDownload.Copy().Background(rowBg)
	badgeAvail := StyleBadgeAvail.Copy().Background(rowBg)
	textStyle := StyleBold.Copy().Background(rowBg)
	dimStyle := StyleDim.Copy().Background(rowBg)

	// Status badge.
	var badge string
	switch lm.Status {
	case StatusLoaded:
		badge = badgeLoaded.Render("●")
	case StatusDownloading:
		frame := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		badge = badgeDownload.Render(frame)
	case StatusPaused:
		badge = badgeDownload.Render("⏸")
	default:
		badge = badgeAvail.Render("○")
	}

	// Quant column value.
	quant := lm.Quant
	if quant == "" {
		quant = "—"
	}

	// Size column. While downloading/paused, the inline progress bar conveys
	// progress, so keep this column compact (total size only) to leave room.
	sizeStr := lm.SizeDisplay
	if (lm.Status == StatusDownloading || lm.Status == StatusPaused) && lm.TotalBytes > 0 {
		sizeStr = FormatSize(lm.TotalBytes)
	}

	// ── Responsive column widths ────────────────────────────────────────────
	// The row must never exceed the panel content width, otherwise the panel
	// wraps it (see fillPanel) and the row's background bleeds onto the wrapped
	// fragments. Fixed pieces: badge(1) + 3 two-space gaps(6) = 7 columns.
	// The remainder is split between name (flexible) and the quant+size columns.
	const (
		gap       = 2
		quantW    = 10
		fixedCols = 1 + gap*3 + quantW // badge + 3 gaps + quant column
		// Progress area: spacer(2) + bar(8) + space(1) + pct(4) = 15 columns.
		progressCols = gap + 8 + 1 + 4
	)
	avail := width
	if avail < 1 {
		avail = 1
	}

	// Reserve space for an inline progress bar up front so the name column
	// shrinks to make room (a download/pause row shows a bar at the end).
	showProgress := lm.Status == StatusDownloading || lm.Status == StatusPaused
	progressW := 0
	if showProgress {
		progressW = progressCols
	}

	// Reserve space for the size string (clamped), then give the rest to name.
	sizeW := lipgloss.Width(sizeStr)
	nameW := avail - fixedCols - sizeW - progressW

	// If the bar doesn't fit, drop it and reclaim its space for the name.
	if nameW < 8 && showProgress {
		showProgress = false
		progressW = 0
		nameW = avail - fixedCols - sizeW
	}

	if nameW < 8 {
		// Very narrow panel: drop quant/size and let the name take what's left.
		nameW = avail - 1 - gap // badge + one gap
		if nameW < 1 {
			nameW = 1
		}
		namePart := textStyle.Width(nameW).Render(truncate(lm.Name, nameW))
		line := badge + dimStyle.Render("  ") + namePart
		return clampRow(line, width, selected, rowBg)
	}

	namePart := textStyle.Width(nameW).Render(truncate(lm.Name, nameW))
	quantPart := dimStyle.Width(quantW).Render(truncate(quant, quantW))
	sizePart := dimStyle.Render(sizeStr)
	spacer := dimStyle.Render("  ")

	line := badge + spacer + namePart + spacer + quantPart + spacer + sizePart

	// Append progress bar if downloading or paused and there is room.
	if showProgress {
		bar := renderMiniProgressBar(lm.Progress, 8, rowBg)
		pct := dimStyle.Render(fmt.Sprintf("%3d%%", int(lm.Progress*100)))
		line += spacer + bar + dimStyle.Render(" ") + pct
	}

	return clampRow(line, width, selected, rowBg)
}

// clampRow pads a selected row to the full content width (so the highlight
// covers it). Truncation is handled centrally by fillPanel, so this only pads.
func clampRow(line string, width int, selected bool, rowBg lipgloss.Color) string {
	if width <= 0 {
		return line
	}
	if selected {
		// Background-only style: pad to width without re-styling content.
		return lipgloss.NewStyle().Background(rowBg).Width(width).Render(line)
	}
	// Non-selected rows are left as-is; fillPanel pads them to the panel width.
	return line
}

// renderMiniProgressBar renders a mini inline progress bar of barWidth chars.
// bg is the background color to use (matches the row's highlight state).
func renderMiniProgressBar(progress float64, barWidth int, bg lipgloss.Color) string {
	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", barWidth-filled)
	return StyleBadgeDownload.Copy().Background(bg).Render(bar)
}

// truncate shortens s to maxLen display columns, appending "…" if needed.
// It is rune-aware so multi-byte names are measured and cut correctly.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	// Reserve one column for the ellipsis.
	cut := maxLen - 1
	if cut < 0 {
		cut = 0
	}
	if cut > len(runes) {
		cut = len(runes)
	}
	return string(runes[:cut]) + "…"
}

// MarkPaused marks a downloading model as paused in-place, preserving progress
// and storing the resume information (repoID, remoteFilename) so the download
// can be resumed later without opening the search overlay.
// firstShardName maps any shard filename back to the first shard filename.
// For example "model-00002-of-00003.gguf" → "model-00001-of-00003.gguf".
// Non-shard filenames are returned unchanged.
func firstShardName(name string) string {
	m := localShardPattern.FindStringSubmatch(name)
	if m == nil {
		return name
	}
	idx, _ := strconv.Atoi(m[1])
	if idx == 1 {
		return name
	}
	// Replace the shard index with 00001.
	// localShardPattern matches "-NNNNN-of-MMMMM.gguf" at the end,
	// so we can replace the suffix after the base prefix.
	ext := ".gguf"
	if strings.HasSuffix(name, ".GGUF") {
		ext = ".GGUF"
	}
	// Strip the matched suffix and rebuild with index 00001.
	suffix := m[0] // the whole match, e.g. "-00002-of-00003.gguf"
	base := name[:len(name)-len(suffix)]
	// Preserve the total-shards digits from the original match.
	// m[0] is "-00002-of-00003.gguf"; we need the total part.
	// Re-extract from the full shard pattern.
	full := localShardFullPattern.FindStringSubmatch(name)
	if full == nil {
		return name
	}
	return fmt.Sprintf("%s-00001-of-%s%s", base, full[2], ext)
}

// localShardFullPattern captures (base, total) for firstShardName reconstruction.
var localShardFullPattern = regexp.MustCompile(`(?i)^(.*)-\d{5}-of-(\d{5})\.gguf$`)

func (m *LibraryModel) MarkPaused(filename, repoID, remoteFilename string) {
	// For split GGUFs, progress and pause state is tracked on the first shard.
	target := firstShardName(filename)
	for i := range m.models {
		if m.models[i].Name == target {
			m.models[i].Status = StatusPaused
			m.models[i].RepoID = repoID
			m.models[i].RemoteFilename = remoteFilename
			return
		}
	}
}

// UpdateDownloadProgress updates the progress of a downloading model by filename.
// progress is 0.0–1.0; bytesDone is the number of bytes written so far;
// totalBytes is the expected final size (0 if unknown);
// done=true clears the downloading status.
// For split GGUFs, non-first shards are mapped to the first shard entry so the
// progress bar reflects the overall download state.
// Returns true if the model was found and updated, false if not found in the list.
func (m *LibraryModel) UpdateDownloadProgress(filename string, progress float64, bytesDone, totalBytes int64, done bool) bool {
	// For split GGUFs, track progress on the first shard entry.
	target := firstShardName(filename)
	for i := range m.models {
		if m.models[i].Name == target {
			if done {
				m.models[i].Status = StatusAvailable
				m.models[i].Progress = 0
				m.models[i].TotalBytes = 0
			} else {
				m.models[i].Status = StatusDownloading
				m.models[i].Progress = progress
				m.models[i].SizeBytes = bytesDone // live bytes-on-disk count
				if totalBytes > 0 {
					m.models[i].TotalBytes = totalBytes
				}
			}
			return true
		}
	}
	return false
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
