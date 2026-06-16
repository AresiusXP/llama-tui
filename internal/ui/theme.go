// Package ui provides shared styles, layout helpers, and overlay models
// for the llama-tui Bubble Tea application.
package ui

import "github.com/charmbracelet/lipgloss"

// Colors — Catppuccin Mocha palette.
const (
	ColorBg          = "#1e1e2e" // Crust — darkest background
	ColorBgPanel     = "#181825" // Mantle — panel background
	ColorBgSelected  = "#45475a" // Surface1 — selected row highlight
	ColorBorder      = "#313244" // Surface0 — subtle borders
	ColorAccent      = "#89b4fa" // Blue — primary accent
	ColorAccent2     = "#cba6f7" // Mauve — secondary accent
	ColorGreen       = "#a6e3a1" // Green — success / loaded / running
	ColorYellow      = "#f9e2af" // Peach — warnings / downloading
	ColorRed         = "#f38ba8" // Red — errors / stopped
	ColorText        = "#cdd6f4" // Text — primary foreground
	ColorTextDim     = "#6c7086" // Overlay0 — secondary/dim text
	ColorTextMuted   = "#585b70" // Surface2 — very dim metadata
	ColorCyan        = "#89dceb" // Sky — key hints / cyan highlights
)

// Styles — initialised in init(), exported so sub-packages can compose them.
var (
	StyleBase          lipgloss.Style
	StylePanel         lipgloss.Style
	StylePanelFocused  lipgloss.Style
	StyleTitle         lipgloss.Style
	StylePanelTitle    lipgloss.Style
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
	StyleMuted         lipgloss.Style
	StyleError         lipgloss.Style
	StyleWarning       lipgloss.Style
	StyleSuccess       lipgloss.Style
	StyleLogInfo       lipgloss.Style
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

	// StyleTitle is the legacy title style (kept for back-compat with search/settings overlays).
	StyleTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true)

	// StylePanelTitle is the consistent in-content panel section header.
	// Green + bold — matches the llmserve aesthetic for panel labels.
	StylePanelTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGreen)).
		Bold(true)

	// StyleSelected — Surface1 background so the selection is clearly visible
	// without washing out the model name text with a bright accent colour.
	StyleSelected = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgSelected)).
		Foreground(lipgloss.Color(ColorText)).
		Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
		Background(lipgloss.Color(ColorBgPanel)).
		Foreground(lipgloss.Color(ColorTextDim)).
		Padding(0, 1)

	// Action bar: sits between the panels and status bar, k9s-style.
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

	// StyleBadgeAvail — full-brightness text so "○ AVAILABLE" is actually readable.
	StyleBadgeAvail = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorText))

	StyleKey = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorCyan)).
		Bold(true)

	StyleDim = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim))

	// StyleMuted is even dimmer than StyleDim — for timestamps and very secondary metadata.
	StyleMuted = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextMuted))

	StyleError = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorRed))

	// StyleWarning is for log warning lines and caution states.
	StyleWarning = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorYellow))

	StyleSuccess = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGreen))

	// StyleLogInfo is full-brightness text for important log events (model loaded, listening, etc.).
	StyleLogInfo = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorText))

	StyleBold = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorText)).
		Bold(true)
}
