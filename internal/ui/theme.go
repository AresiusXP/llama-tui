// Package ui provides shared styles, layout helpers, and overlay models
// for the llama-tui Bubble Tea application.
package ui

import "github.com/charmbracelet/lipgloss"

// Colors — dark Tokyo-Night inspired palette.
const (
	ColorBg        = "#1a1b26" // dark charcoal
	ColorBgPanel   = "#1f2335" // slightly lighter panel bg
	ColorBorder    = "#3b4261" // muted blue-grey border
	ColorAccent    = "#7aa2f7" // blue accent
	ColorAccent2   = "#bb9af7" // purple accent
	ColorGreen     = "#9ece6a" // success / loaded
	ColorYellow    = "#e0af68" // warning / downloading
	ColorRed       = "#f7768e" // error / stopped
	ColorText      = "#c0caf5" // primary text
	ColorTextDim   = "#565f89" // dimmed / secondary text
	ColorTextMuted = "#414868" // very dim, metadata
	ColorCyan      = "#7dcfff" // highlight cyan
)

// Styles — initialised in init(), exported so sub-packages can compose them.
var (
	StyleBase          lipgloss.Style
	StylePanel         lipgloss.Style
	StylePanelFocused  lipgloss.Style
	StyleTitle         lipgloss.Style
	StyleSelected      lipgloss.Style
	StyleStatusBar     lipgloss.Style
	StyleActionBar     lipgloss.Style
	StyleBadgeLoaded   lipgloss.Style
	StyleBadgeRunning  lipgloss.Style
	StyleBadgeStopped  lipgloss.Style
	StyleBadgeDownload lipgloss.Style
	StyleBadgeAvail    lipgloss.Style
	StyleKey           lipgloss.Style
	StyleDim           lipgloss.Style
	StyleError         lipgloss.Style
	StyleSuccess       lipgloss.Style
	StyleBold          lipgloss.Style
)

func init() {
	StyleBase = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBg)).
		Foreground(lipgloss.Color(ColorText))

	StylePanel = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorText)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorder))

	StylePanelFocused = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorText)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorAccent))

	StyleTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true)

	StyleSelected = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorAccent)).
		Foreground(lipgloss.Color(ColorBg)).
		Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorTextDim)).
		Padding(0, 1)

	// Action bar: sits between the panels and status bar, k9s-style.
	// Slightly darker than the panel bg, with accent-colored key labels.
	StyleActionBar = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBg)).
		Foreground(lipgloss.Color(ColorTextDim)).
		Padding(0, 1)

	StyleBadgeLoaded = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGreen)).
		Bold(true)

	StyleBadgeRunning = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGreen)).
		Bold(true)

	StyleBadgeStopped = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorRed)).
		Bold(true)

	StyleBadgeDownload = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorYellow)).
		Bold(true)

	StyleBadgeAvail = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim))

	StyleKey = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCyan)).
		Bold(true)

	StyleDim = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim))

	StyleError = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorRed))

	StyleSuccess = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGreen))

	StyleBold = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorText)).
		Bold(true)
}
