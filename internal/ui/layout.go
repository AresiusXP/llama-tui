package ui

import "github.com/charmbracelet/lipgloss"

// Layout holds computed panel dimensions.
type Layout struct {
	TotalWidth    int
	TotalHeight   int
	LeftWidth     int
	RightWidth    int
	ContentHeight int // height available for panels (total − statusbar − actionbar − borders)
	StatusHeight  int // always 1
	ActionHeight  int // always 1
}

// borderOverhead is the number of rows/columns consumed by a rounded border
// (1 top + 1 bottom = 2 rows; 1 left + 1 right = 2 cols).
const borderOverhead = 2

// NewLayout computes panel dimensions from terminal size.
func NewLayout(width, height int) Layout {
	const statusHeight = 1
	const actionHeight = 1

	leftWidth := width * 35 / 100
	rightWidth := width - leftWidth

	// Content height: total minus status bar, action bar, and border rows.
	contentHeight := height - statusHeight - actionHeight - borderOverhead

	return Layout{
		TotalWidth:    width,
		TotalHeight:   height,
		LeftWidth:     leftWidth,
		RightWidth:    rightWidth,
		ContentHeight: contentHeight,
		StatusHeight:  statusHeight,
		ActionHeight:  actionHeight,
	}
}

// RenderFrame renders the full two-panel + action-bar + status-bar frame.
// leftContent and rightContent are already-rendered strings from sub-models.
// actionContent is the one-line k9s-style action hints bar.
// statusContent is the one-line status bar string.
// leftFocused controls which panel gets the accent border.
func RenderFrame(layout Layout, leftContent, rightContent, actionContent, statusContent string, leftFocused bool) string {
	leftStyle := StylePanel
	rightStyle := StylePanel
	if leftFocused {
		leftStyle = StylePanelFocused
	} else {
		rightStyle = StylePanelFocused
	}

	leftPanel := leftStyle.
		Width(layout.LeftWidth - borderOverhead).
		Height(layout.ContentHeight).
		Render(leftContent)

	rightPanel := rightStyle.
		Width(layout.RightWidth - borderOverhead).
		Height(layout.ContentHeight).
		Render(rightContent)

	panelRow := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	actionBar := StyleActionBar.
		Width(layout.TotalWidth).
		Render(actionContent)

	statusBar := StyleStatusBar.
		Width(layout.TotalWidth).
		Render(statusContent)

	return lipgloss.JoinVertical(lipgloss.Left, panelRow, actionBar, statusBar)
}
