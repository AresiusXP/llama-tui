package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriciodanos/llama-tui/internal/huggingface"
)

// DownloadCompleteMsg is sent when a download finishes successfully.
type DownloadCompleteMsg struct {
	RepoID   string
	Filename string
	Path     string
}

// DownloadErrorMsg is sent when a download fails.
type DownloadErrorMsg struct {
	Err error
}

// DownloadProgressMsg carries a DownloadProgress update.
type DownloadProgressMsg struct {
	Progress huggingface.DownloadProgress
}

// DownloadModel shows download progress for a single file.
type DownloadModel struct {
	repoID   string
	filename string
	progress huggingface.DownloadProgress
	bar      progress.Model
	done     bool
	err      error
	width    int
	height   int
}

// NewDownload creates a new DownloadModel.
func NewDownload(repoID, filename string, width, height int) DownloadModel {
	bar := progress.New(
		progress.WithSolidFill(ColorAccent),
		progress.WithoutPercentage(),
	)
	bar.Width = width - 10
	if bar.Width < 10 {
		bar.Width = 10
	}

	return DownloadModel{
		repoID:   repoID,
		filename: filename,
		bar:      bar,
		width:    width,
		height:   height,
	}
}

// SetSize updates the overlay dimensions.
func (m *DownloadModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.bar.Width = width - 10
	if m.bar.Width < 10 {
		m.bar.Width = 10
	}
}

// Init implements tea.Model.
func (m DownloadModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m DownloadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DownloadProgressMsg:
		p := msg.Progress
		m.progress = p

		pct := p.Percent() / 100.0
		var barCmd tea.Cmd
		var barModel tea.Model
		barModel, barCmd = m.bar.Update(m.bar.SetPercent(pct))
		m.bar = barModel.(progress.Model)

		if p.Err != nil {
			m.err = p.Err
			m.done = false
			return m, func() tea.Msg { return DownloadErrorMsg{Err: p.Err} }
		}

		if p.Done {
			m.done = true
			repoID := m.repoID
			filename := m.filename
			path := p.Filename // DownloadProgress.Filename carries the full dest path
			return m, func() tea.Msg {
				return DownloadCompleteMsg{
					RepoID:   repoID,
					Filename: filename,
					Path:     path,
				}
			}
		}

		return m, barCmd

	case tea.KeyMsg:
		if msg.String() == "esc" {
			// Cancel: parent model is responsible for stopping the download goroutine.
			return m, func() tea.Msg { return DownloadErrorMsg{Err: fmt.Errorf("download cancelled")} }
		}
	}

	var barCmd tea.Cmd
	var barModel tea.Model
	barModel, barCmd = m.bar.Update(msg)
	m.bar = barModel.(progress.Model)
	return m, barCmd
}

// View renders the download progress overlay.
func (m DownloadModel) View() string {
	title := StyleTitle.Render("Downloading")
	filename := StyleBold.Render(m.filename)

	pct := m.progress.Percent()
	bytesDone := FormatSize(m.progress.BytesDone)
	bytesTotal := FormatSize(m.progress.BytesTotal)

	barStr := m.bar.View()
	progressLine := fmt.Sprintf("%s  %.0f%%  (%s / %s)", barStr, pct, bytesDone, bytesTotal)

	var statusLine string
	if m.err != nil {
		statusLine = StyleError.Render("Error: " + m.err.Error())
	} else if m.done {
		statusLine = StyleSuccess.Render("Complete!")
	} else {
		statusLine = StyleDim.Render("[Esc] Cancel")
	}

	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	boxStyle := lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorAccent)).
		Padding(1, 2)

	inner := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		filename,
		"",
		progressLine,
		"",
		statusLine,
	)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(inner),
	)
}
