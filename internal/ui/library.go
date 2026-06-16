package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// Status badge.
	var badge string
	switch lm.Status {
	case StatusLoaded:
		badge = StyleBadgeLoaded.Render("●")
	case StatusDownloading:
		frame := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		badge = StyleBadgeDownload.Render(frame)
	case StatusPaused:
		badge = StyleBadgeDownload.Render("⏸")
	default:
		badge = StyleBadgeAvail.Render("○")
	}

	// Build the display line.
	quant := lm.Quant
	if quant == "" {
		quant = "—"
	}

	// Size column: during active download or pause show "done / total".
	sizeStr := lm.SizeDisplay
	if (lm.Status == StatusDownloading || lm.Status == StatusPaused) && lm.TotalBytes > 0 {
		sizeStr = FormatSize(lm.SizeBytes) + " / " + FormatSize(lm.TotalBytes)
	}

	line := fmt.Sprintf("%s  %-40s  %-10s  %s", badge, truncate(lm.Name, 40), quant, sizeStr)

	// Append progress bar if downloading or paused.
	if lm.Status == StatusDownloading || lm.Status == StatusPaused {
		bar := renderMiniProgressBar(lm.Progress, 8)
		pct := int(lm.Progress * 100)
		line += fmt.Sprintf("  %s %d%%", bar, pct)
	}

	if selected {
		// Pad to width so the highlight covers the full row.
		if width > 0 {
			line = StyleSelected.Width(width - 4).Render(line)
		} else {
			line = StyleSelected.Render(line)
		}
	}

	return line
}

// renderMiniProgressBar renders a mini inline progress bar of barWidth chars.
func renderMiniProgressBar(progress float64, barWidth int) string {
	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", barWidth-filled)
	return StyleBadgeDownload.Render(bar)
}

// truncate shortens s to maxLen, appending "…" if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// MarkPaused marks a downloading model as paused in-place, preserving progress
// and storing the resume information (repoID, remoteFilename) so the download
// can be resumed later without opening the search overlay.
func (m *LibraryModel) MarkPaused(filename, repoID, remoteFilename string) {
	for i := range m.models {
		if m.models[i].Name == filename {
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
// Returns true if the model was found and updated, false if not found in the list.
func (m *LibraryModel) UpdateDownloadProgress(filename string, progress float64, bytesDone, totalBytes int64, done bool) bool {
	for i := range m.models {
		if m.models[i].Name == filename {
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
