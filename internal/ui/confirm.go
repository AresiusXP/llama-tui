package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfirmDeleteYesMsg is sent when the user confirms a deletion.
type ConfirmDeleteYesMsg struct {
	Model LocalModel
}

// ConfirmDeleteNoMsg is sent when the user cancels a deletion.
type ConfirmDeleteNoMsg struct{}

// ConfirmModel is a small centered modal asking the user to confirm
// deletion of a model file. It intercepts y/n/Esc keystrokes.
type ConfirmModel struct {
	target LocalModel
	width  int
	height int
}

// NewConfirm creates a ConfirmModel for the given model file.
func NewConfirm(target LocalModel, width, height int) ConfirmModel {
	return ConfirmModel{target: target, width: width, height: height}
}

// Init implements tea.Model.
func (m ConfirmModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "Y":
			lm := m.target
			return m, func() tea.Msg { return ConfirmDeleteYesMsg{Model: lm} }
		case "n", "N", "esc", "q":
			return m, func() tea.Msg { return ConfirmDeleteNoMsg{} }
		}
	}
	return m, nil
}

// View renders the confirmation modal centered on screen.
func (m ConfirmModel) View() string {
	name := truncate(m.target.Name, 50)

	title := StyleError.Render("⚠  Delete model?")
	body := fmt.Sprintf(
		"%s\n\n%s",
		StyleDim.Render(name),
		StyleDim.Render("This will permanently delete the file from disk."),
	)
	hint := StyleKey.Render("[y]") + StyleDim.Render(" Yes, delete") +
		"   " + StyleKey.Render("[n]") + StyleDim.Render(" No, cancel")

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		body,
		"",
		hint,
	)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorText)).
		Padding(1, 3)

	box := boxStyle.Render(content)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color(ColorBg)),
	)
}
