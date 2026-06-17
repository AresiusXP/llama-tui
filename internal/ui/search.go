package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/AresiusXP/llama-tui/internal/huggingface"
)

// CloseSearchMsg is sent when the user closes the search overlay.
type CloseSearchMsg struct{}

// DownloadRequestMsg is sent when user selects a file to download.
type DownloadRequestMsg struct {
	RepoID   string
	Filename string
	FileSize int64
}

// SearchResultsMsg carries results from a HF search.
type SearchResultsMsg struct {
	Models []huggingface.ModelInfo
	Err    error
}

// RepoFilesMsg carries the file listing for a selected repo.
type RepoFilesMsg struct {
	RepoID string
	Files  []huggingface.RepoFile
	Err    error
}

// SearchModel is the full-screen search overlay model.
type SearchModel struct {
	width         int
	height        int
	hfToken       string
	activeTab     int // 0=Popular, 1=Search
	input         textinput.Model
	popular       []huggingface.PopularModel
	searchResults []huggingface.ModelInfo
	repoFiles     []huggingface.RepoFile
	selectedRepo  string
	cursor        int
	subCursor     int  // cursor within file list
	showFiles     bool // true when showing files for a selected repo
	loading       bool
	err           string
}

// NewSearch creates a new SearchModel.
func NewSearch(hfToken string, width, height int) SearchModel {
	ti := textinput.New()
	ti.Placeholder = "Search models…"
	ti.CharLimit = 200
	ti.Width = width - 6

	return SearchModel{
		hfToken: hfToken,
		width:   width,
		height:  height,
		input:   ti,
	}
}

// SetSize updates the overlay dimensions.
func (m *SearchModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.input.Width = width - 6
}

// Init loads the popular models list.
func (m SearchModel) Init() tea.Cmd {
	return func() tea.Msg {
		models, err := huggingface.LoadPopularModels()
		if err != nil {
			return SearchResultsMsg{Err: err}
		}
		return popularModelsMsg{models: models}
	}
}

// popularModelsMsg is an internal message for loaded popular models.
type popularModelsMsg struct {
	models []huggingface.PopularModel
}

// Update implements tea.Model.
func (m SearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case popularModelsMsg:
		m.popular = msg.models
		return m, nil

	case SearchResultsMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err.Error()
		} else {
			m.searchResults = msg.Models
			m.cursor = 0
			m.err = ""
		}
		// Keep the input blurred after results arrive.
		// Blurred = navigate/select mode (Enter selects).
		// To edit the query, press / or any letter key to re-focus.
		m.input.Blur()
		return m, nil

	case RepoFilesMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err.Error()
		} else {
			m.repoFiles = msg.Files
			m.selectedRepo = msg.RepoID
			m.showFiles = true
			m.subCursor = 0
			m.err = ""
		}
		return m, nil

	case tea.KeyMsg:
		// Handle file list view.
		if m.showFiles {
			return m.updateFileList(msg)
		}
		return m.updateModelList(msg)
	}

	return m, nil
}

// updateModelList handles key events on the model/results list.
//
// Two modes:
//   - Input focused  → user is editing the query. Enter = search, Esc = close.
//     ↑/↓ = navigate results WITHOUT losing focus (so user can pick & press Enter).
//     Wait — that causes the original Enter-fires-search bug. So instead:
//     when input focused, Enter = search. To select, blur the input first (press Esc-from-input).
//   - Input blurred  → navigation/select mode. ↑/↓ = move cursor, Enter = open files.
//     Any printable key = re-focus input and start editing (append the key).
func (m SearchModel) updateModelList(msg tea.KeyMsg) (SearchModel, tea.Cmd) {

	// ── Input-focused: editing mode ───────────────────────────────────────
	if m.activeTab == 1 && m.input.Focused() {
		switch msg.Type {
		case tea.KeyEsc:
			// Esc from inside the input: blur it (enter navigate mode), don't close.
			m.input.Blur()
			return m, nil
		case tea.KeyEnter:
			// Submit the current query as a new search.
			query := strings.TrimSpace(m.input.Value())
			if query == "" {
				return m, nil
			}
			m.loading = true
			m.input.Blur()
			token := m.hfToken
			return m, func() tea.Msg {
				client := huggingface.NewClient(token)
				models, err := client.SearchModels(context.Background(), query)
				return SearchResultsMsg{Models: models, Err: err}
			}
		}
		// All other keys go to the text input.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	// ── Input blurred: navigate/select mode ──────────────────────────────
	switch msg.String() {
	case "tab":
		m.activeTab = 1 - m.activeTab
		m.cursor = 0
		if m.activeTab == 1 {
			m.input.Focus()
			return m, textinput.Blink
		}
		m.input.Blur()

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		listLen := m.currentListLen()
		if m.cursor < listLen-1 {
			m.cursor++
		}

	case "enter":
		// Open the file list for the highlighted result.
		repoID := m.currentRepoID()
		if repoID == "" {
			return m, nil
		}
		m.loading = true
		token := m.hfToken
		return m, func() tea.Msg {
			client := huggingface.NewClient(token)
			files, err := client.ListGGUFFiles(context.Background(), repoID)
			return RepoFilesMsg{RepoID: repoID, Files: files, Err: err}
		}

	case "/":
		// Vim-style: / focuses the search input for editing.
		if m.activeTab == 1 {
			m.input.Focus()
			return m, textinput.Blink
		}

	case "esc", "q":
		return m, func() tea.Msg { return CloseSearchMsg{} }

	default:
		// Any printable key on Search tab auto-focuses the input and types the char.
		if m.activeTab == 1 && msg.Type == tea.KeyRunes {
			m.input.Focus()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, tea.Batch(cmd, textinput.Blink)
		}
	}

	return m, nil
}

// updateFileList handles key events when on the file list.
func (m SearchModel) updateFileList(msg tea.KeyMsg) (SearchModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.subCursor > 0 {
			m.subCursor--
		}

	case "down", "j":
		if m.subCursor < len(m.repoFiles)-1 {
			m.subCursor++
		}

	case "enter":
		if m.subCursor >= 0 && m.subCursor < len(m.repoFiles) {
			f := m.repoFiles[m.subCursor]
			repoID := m.selectedRepo
			return m, func() tea.Msg {
				var size int64
				if f.LFS != nil {
					size = f.LFS.Size
				} else {
					size = f.Size
				}
				return DownloadRequestMsg{
					RepoID:   repoID,
					Filename: f.RFilename,
					FileSize: size,
				}
			}
		}

	case "esc":
		m.showFiles = false
		m.repoFiles = nil
		m.selectedRepo = ""
		m.subCursor = 0

	case "q":
		return m, func() tea.Msg { return CloseSearchMsg{} }
	}

	return m, nil
}

// currentListLen returns the length of the active list.
func (m SearchModel) currentListLen() int {
	if m.activeTab == 0 {
		return len(m.popular)
	}
	return len(m.searchResults)
}

// currentRepoID returns the repo ID at the current cursor position.
func (m SearchModel) currentRepoID() string {
	if m.activeTab == 0 {
		if m.cursor >= 0 && m.cursor < len(m.popular) {
			return m.popular[m.cursor].RepoID
		}
	} else {
		if m.cursor >= 0 && m.cursor < len(m.searchResults) {
			return m.searchResults[m.cursor].ID
		}
	}
	return ""
}

// View renders the search overlay.
func (m SearchModel) View() string {
	// Title bar.
	titleBar := StyleTitle.Render("HuggingFace Model Browser")

	// Tabs.
	tabs := renderTabs([]string{"Popular", "Search"}, m.activeTab)

	// Content area.
	var content string
	if m.showFiles {
		content = m.renderFileList()
	} else if m.loading {
		content = StyleDim.Render("Loading…")
	} else if m.err != "" {
		content = StyleError.Render("Error: " + m.err)
	} else {
		switch m.activeTab {
		case 0:
			content = m.renderPopularList()
		case 1:
			content = m.renderSearchTab()
		}
	}

	// Bottom hint — context-aware.
	var hint string
	if m.showFiles {
		hint = buildKeyHints([]keyHint{
			{"Enter", "Download"},
			{"Esc", "Back"},
		})
	} else if m.activeTab == 1 && m.input.Focused() {
		// Editing mode: Enter submits, Esc goes back to navigate mode.
		hint = buildKeyHints([]keyHint{
			{"Enter", "Search"},
			{"Esc", "Stop editing"},
		})
	} else if m.activeTab == 1 {
		// Navigate mode: Enter selects, / or any letter re-edits.
		hint = buildKeyHints([]keyHint{
			{"Enter", "Select"},
			{"↑↓", "Navigate"},
			{"/", "Edit query"},
			{"Esc", "Close"},
		})
	} else {
		hint = buildKeyHints([]keyHint{
			{"Enter", "Select"},
			{"Tab", "Search tab"},
			{"Esc", "Close"},
		})
	}
	hintLine := StyleDim.Render(hint)

	// Wrap in a centered box.
	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	boxStyle := lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorAccent)).
		BorderBackground(lipgloss.Color(ColorBg)).
		Background(lipgloss.Color(ColorBgPanel)).
		Padding(1, 2)

	inner := lipgloss.JoinVertical(
		lipgloss.Left,
		titleBar,
		"",
		tabs,
		"",
		content,
		"",
		hintLine,
	)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(inner),
		lipgloss.WithWhitespaceBackground(lipgloss.Color(ColorBg)),
	)
}

// renderTabs renders the tab bar.
func renderTabs(labels []string, active int) string {
	var parts []string
	for i, label := range labels {
		if i == active {
			parts = append(parts, StyleSelected.Render("[ "+label+" ]"))
		} else {
			parts = append(parts, StyleDim.Render("[ "+label+" ]"))
		}
	}
	return strings.Join(parts, "  ")
}

// renderPopularList renders the popular models tab content.
func (m SearchModel) renderPopularList() string {
	if len(m.popular) == 0 {
		return StyleDim.Render("No popular models found.")
	}

	var rows []string
	for i, pm := range m.popular {
		name := StyleBold.Render(truncate(pm.Name, 30))
		desc := StyleDim.Render(truncate(pm.Description, 40))
		quant := StyleBadgeAvail.Render(pm.RecommendedQuant)
		size := StyleDim.Render(fmt.Sprintf("~%.1fGB", pm.ApproxSizeGB))

		line := fmt.Sprintf("  %-30s  %-12s  %-8s  %s", name, quant, size, desc)

		if i == m.cursor {
			rows = append(rows, StyleSelected.Render(line))
		} else {
			rows = append(rows, line)
		}
	}
	return strings.Join(rows, "\n")
}

// renderSearchTab renders the search tab content.
func (m SearchModel) renderSearchTab() string {
	var parts []string
	parts = append(parts, m.input.View())
	parts = append(parts, "")

	if len(m.searchResults) == 0 {
		parts = append(parts, StyleDim.Render("Type a query and press Enter to search."))
	} else {
		for i, mi := range m.searchResults {
			dl := fmt.Sprintf("↓%d", mi.Downloads)
			line := fmt.Sprintf("  %-50s  %s", truncate(mi.ID, 50), StyleDim.Render(dl))
			if i == m.cursor {
				line = StyleSelected.Render(line)
			}
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, "\n")
}

// recommendedIndex returns the index of the best file to download from a list.
// It prefers common high-quality quants in priority order; falls back to the
// middle file by index if none match.
func recommendedIndex(files []huggingface.RepoFile) int {
	priority := []string{"Q4_K_M", "Q4_K_S", "Q5_K_M", "Q5_K_S", "Q6_K", "Q8_0", "Q4_0", "Q4_K_XL", "Q4_K_L"}
	for _, want := range priority {
		for i, f := range files {
			if strings.EqualFold(ParseQuant(f.RFilename), want) {
				return i
			}
		}
	}
	// No preferred quant found — pick the middle file.
	return len(files) / 2
}

// renderFileList renders the GGUF file list for a selected repo.
func (m SearchModel) renderFileList() string {
	header := StyleTitle.Render("Files in " + m.selectedRepo)
	var rows []string
	rows = append(rows, header, "")

	if len(m.repoFiles) == 0 {
		rows = append(rows, StyleDim.Render("No GGUF files found in this repository."))
		return strings.Join(rows, "\n")
	}

	recIdx := recommendedIndex(m.repoFiles)

	for i, f := range m.repoFiles {
		size := f.EffectiveSize()
		quant := ParseQuant(f.RFilename)
		if quant == "" {
			quant = "—"
		}

		// Recommended badge.
		var badge string
		if i == recIdx {
			badge = StyleBadgeLoaded.Render("★ ")
		} else {
			badge = "  "
		}

		quantStr := quant
		if i == recIdx {
			quantStr = StyleBadgeLoaded.Render(quant)
		}

		line := fmt.Sprintf("%s%-50s  %-12s  %s", badge, truncate(f.RFilename, 50), quantStr, FormatSize(size))
		if i == m.subCursor {
			line = StyleSelected.Render(line)
		}
		rows = append(rows, line)
	}
	return strings.Join(rows, "\n")
}
