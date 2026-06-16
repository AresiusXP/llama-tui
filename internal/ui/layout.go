package ui

import "github.com/charmbracelet/lipgloss"

// Layout holds computed panel dimensions for the 4-panel layout.
type Layout struct {
	TotalWidth  int
	TotalHeight int

	LeftWidth  int
	RightWidth int

	// Left column heights.
	LeftTopHeight    int // LibraryModel (top-left)
	LeftBottomHeight int // LogPanelModel (bottom-left)

	// Right column heights.
	RightTopHeight    int // StatusPanelModel (top-right)
	RightBottomHeight int // DetailModel / ChatModel (bottom-right)

	// Retained for back-compat references in app.go that haven't been migrated.
	ContentHeight int

	StatusHeight int // always 1 (status bar row below panels)
	ActionHeight int // always 1 (action bar row below panels)
}

// borderOverhead is the number of rows/columns consumed by a rounded border
// (1 top + 1 bottom = 2 rows; 1 left + 1 right = 2 cols).
const borderOverhead = 2

// NewLayout computes panel dimensions from terminal size.
//
// metricsEnabled controls the height of the top-right status panel:
//   - false → 1 inner line (static info only)
//   - true  → 2 inner lines (static info + live t/s row)
//
// Height semantics: lipgloss Style.Height(h) sets the INNER content height.
// The final rendered block occupies h+2 rows (1 top border + content + 1 bottom border).
// Each column stacks two bordered panels, so the sum of the two inner heights
// must equal contentHeight − 4 to keep both columns from overflowing the terminal.
func NewLayout(width, height int, metricsEnabled bool) Layout {
	const statusHeight = 1
	const actionHeight = 1

	leftWidth := width * 35 / 100
	rightWidth := width - leftWidth

	// Total rows available for the two panel columns.
	contentHeight := height - statusHeight - actionHeight
	if contentHeight < 8 {
		contentHeight = 8
	}

	// Each column stacks two bordered panels (2 borders × 2 rows each = 4 rows overhead).
	// We compute inner heights that sum to contentHeight − 4.
	innerHeight := contentHeight - 4
	if innerHeight < 2 {
		innerHeight = 2
	}

	// Left column: library (top, 70%) + log panel (bottom, 30%).
	leftTopHeight := innerHeight * 70 / 100
	if leftTopHeight < 1 {
		leftTopHeight = 1
	}
	leftBottomHeight := innerHeight - leftTopHeight
	if leftBottomHeight < 1 {
		leftBottomHeight = 1
	}

	// Right column: status panel (top) + detail/chat (bottom).
	// The status panel is 1 line when metrics are disabled, 2 lines when enabled.
	statusPanelInnerHeight := 1
	if metricsEnabled {
		statusPanelInnerHeight = 2
	}
	rightTopHeight := statusPanelInnerHeight
	rightBottomHeight := innerHeight - rightTopHeight
	if rightBottomHeight < 1 {
		rightBottomHeight = 1
	}

	return Layout{
		TotalWidth:  width,
		TotalHeight: height,

		LeftWidth:  leftWidth,
		RightWidth: rightWidth,

		LeftTopHeight:    leftTopHeight,
		LeftBottomHeight: leftBottomHeight,

		RightTopHeight:    rightTopHeight,
		RightBottomHeight: rightBottomHeight,

		// ContentHeight retained as a back-compat approximation (inner height of library panel).
		ContentHeight: leftTopHeight,

		StatusHeight: statusHeight,
		ActionHeight: actionHeight,
	}
}

// RenderFrame renders the full 4-panel + action-bar + status-bar frame.
//
// Panel layout:
//
//	┌────────────────┬─────────────────────────────┐
//	│  libraryContent│  statusContent              │  ← top row
//	├────────────────┼─────────────────────────────┤
//	│  logContent    │  detailContent              │  ← bottom row
//	└────────────────┴─────────────────────────────┘
//	  actionContent
//	  statusBarContent
//
// leftFocused controls which column gets the accent border.
func RenderFrame(layout Layout, libraryContent, statusContent, logContent, detailContent, actionContent, statusBarContent string, leftFocused bool) string {
	leftStyle := StylePanel
	rightStyle := StylePanel
	if leftFocused {
		leftStyle = StylePanelFocused
	} else {
		rightStyle = StylePanelFocused
	}

	// Left column: library (top) + log (bottom).
	leftTopPanel := leftStyle.
		Width(layout.LeftWidth - borderOverhead).
		Height(layout.LeftTopHeight).
		Render(libraryContent)

	leftBottomPanel := leftStyle.
		Width(layout.LeftWidth - borderOverhead).
		Height(layout.LeftBottomHeight).
		Render(logContent)

	leftColumn := lipgloss.JoinVertical(lipgloss.Left, leftTopPanel, leftBottomPanel)

	// Right column: status (top, compact) + detail/chat (bottom).
	rightTopPanel := rightStyle.
		Width(layout.RightWidth - borderOverhead).
		Height(layout.RightTopHeight).
		Render(statusContent)

	rightBottomPanel := rightStyle.
		Width(layout.RightWidth - borderOverhead).
		Height(layout.RightBottomHeight).
		Render(detailContent)

	rightColumn := lipgloss.JoinVertical(lipgloss.Left, rightTopPanel, rightBottomPanel)

	// Join the two columns side by side.
	panelRow := lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, rightColumn)

	actionBar := StyleActionBar.
		Width(layout.TotalWidth).
		Render(actionContent)

	statusBar := StyleStatusBar.
		Width(layout.TotalWidth).
		Render(statusBarContent)

	return lipgloss.JoinVertical(lipgloss.Left, panelRow, actionBar, statusBar)
}
