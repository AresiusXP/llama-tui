package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fillPanel normalises panel content so that every line begins with an explicit
// Background(ColorBgPanel) escape code and is exactly innerWidth columns wide.
//
// Why this is necessary: when lipgloss renders a styled panel border around
// content, it calls te.Styled(line) per line. Any inner style that emits a
// reset sequence (\x1b[0m) mid-line clears the outer panel background for all
// subsequent characters on that line. Terminals with a non-default background
// colour (e.g. Kitty with background_opacity < 1 or a custom background colour)
// then show the terminal's own background through those cells as "ghost blocks".
//
// By splitting the content into lines and re-rendering each line through a
// base background style, we guarantee that every line starts fresh with an
// explicit background colour, and that all remaining columns are filled with
// background-coloured spaces — leaving no transparent cells for the terminal
// background to bleed through.
//
// Each line is also hard-truncated to innerWidth *before* padding. This is a
// safety net: if any panel emits content wider than its allotted width,
// lipgloss would otherwise soft-wrap it into multiple physical lines (carrying
// the line's background onto every wrapped fragment and overflowing the panel's
// fixed Height). Truncating guarantees exactly one physical line per logical
// line regardless of what a panel emits.
func fillPanel(content string, innerWidth int) string {
	if innerWidth < 0 {
		innerWidth = 0
	}
	base := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Width(innerWidth)

	lines := strings.Split(content, "\n")
	for i, l := range lines {
		// Clip over-wide lines so lipgloss never soft-wraps them.
		if lipgloss.Width(l) > innerWidth {
			l = ansi.Truncate(l, innerWidth, "")
		}
		lines[i] = base.Render(l)
	}
	return strings.Join(lines, "\n")
}

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

// panelHPadding is the horizontal padding (in columns) applied inside each
// panel border by StylePanel/StylePanelFocused (Padding(0, 1)). The text
// content area is therefore PanelWidth − borderOverhead − 2*panelHPadding.
const panelHPadding = 1

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

	leftWidth := width * 30 / 100
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

	// Left column: library (top, 78%) + log panel (bottom, 22%).
	leftTopHeight := innerHeight * 78 / 100
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

	// Normalise content: stamp ColorBgPanel on every cell of every line so
	// that inner ANSI resets never leave transparent cells. The fill width is
	// the panel's text content area: PanelWidth − border (2) − horizontal
	// padding (2). This matches the width the sub-models lay out their content
	// at (see app.go: LeftWidth−4 / RightWidth−4).
	contentLeft := layout.LeftWidth - borderOverhead - 2*panelHPadding
	contentRight := layout.RightWidth - borderOverhead - 2*panelHPadding

	libraryContent = fillPanel(libraryContent, contentLeft)
	logContent = fillPanel(logContent, contentLeft)
	statusContent = fillPanel(statusContent, contentRight)
	detailContent = fillPanel(detailContent, contentRight)

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
