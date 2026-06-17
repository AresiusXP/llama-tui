package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModel is a full-screen help overlay listing all keybindings.
type HelpModel struct {
	width  int
	height int
}

// NewHelp creates a HelpModel sized to the given terminal dimensions.
func NewHelp(width, height int) HelpModel {
	return HelpModel{width: width, height: height}
}

// Init implements tea.Model.
func (m HelpModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m HelpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// View implements tea.Model.
func (m HelpModel) View() string {
	type binding struct {
		key    string
		action string
	}

	bindings := []binding{
		{"↑ / ↓", "Navigate model list"},
		{"Enter / l", "Load selected model"},
		{"u", "Unload model"},
		{"c", "Open chat panel"},
		{"d", "Download / search models"},
		{"x", "Cancel / resume download"},
		{"Ctrl+D", "Delete model file"},
		{"Ctrl+U", "Install / update llama-server"},
		{"s", "Settings"},
		{"Tab", "Switch panel focus"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}

	// Build aligned rows.
	var sb strings.Builder
	for _, b := range bindings {
		key := StyleKey.Width(14).Render(b.key)
		desc := StyleDim.Render(b.action)
		sb.WriteString(key + "  " + desc + "\n")
	}

	content := StyleTitle.Render("Key Bindings") + "\n\n" + strings.TrimRight(sb.String(), "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorAccent)).
		BorderBackground(lipgloss.Color(ColorBg)).
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorText)).
		Padding(1, 3)

	box := boxStyle.Render(content)

	// Center the box within the terminal.
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color(ColorBg)),
	)
}
