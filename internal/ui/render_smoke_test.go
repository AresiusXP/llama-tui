package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestRenderFrameNoOverflowNoGray verifies that at a typical wide terminal the
// rendered frame has no wrapped/overflowing panel rows and that gray
// (ColorBgSelected) does not bleed into non-selected areas like the detail panel.
func TestRenderFrameNoOverflowNoGray(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	// Simulate a 200-col x 50-row terminal.
	layout := NewLayout(200, 50, false)

	lib := NewLibrary("/tmp", layout.LeftWidth-4, layout.LeftTopHeight)
	// Inject a model with a long name.
	lib.models = []LocalModel{
		{Name: "gemma-4-12B-it-qat-UD-Q4_K_XL.gguf", Quant: "Q4_K_XL", SizeDisplay: "6.26 GB", Status: StatusAvailable},
	}
	lib.cursor = 0

	detail := NewDetail(layout.RightWidth-4, layout.RightBottomHeight)
	detail.SetModel(&lib.models[0])
	detail.SetServerState("STOPPED", "", "Apple M2 Max", "apple")

	status := NewStatusPanelModel()
	logs := NewLogPanelModel()

	frame := RenderFrame(layout,
		lib.View(), status.View(), logs.View(), detail.View(),
		"<l> Load  <d> Download", "● STOPPED", true)

	lines := strings.Split(frame, "\n")

	// 1. Total height should equal terminal height (no vertical overflow).
	if len(lines) != 50 {
		t.Errorf("frame height = %d lines, want 50 (vertical overflow from wrapped rows?)", len(lines))
	}

	// 2. Every physical line's visible width must equal 200 (no horizontal overflow).
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 200 {
			t.Errorf("line %d width = %d, want 200: %q", i, w, l)
			break
		}
	}

	// 3. The selected library row's gray (ColorBgSelected) must only appear in
	//    the top-left library list region — never in the detail panel, status
	//    bar, or action bar. We verify total gray occurrences are bounded to the
	//    single selected row (it spans one physical line after the fixes).
	graySGR := "48;2;69;71;89" // ColorBgSelected #45475a in truecolor
	grayLines := 0
	for _, l := range lines {
		if strings.Contains(l, graySGR) {
			grayLines++
		}
	}
	// Exactly one line (the single selected row) should carry gray.
	if grayLines != 1 {
		t.Errorf("gray ColorBgSelected appears on %d lines, want exactly 1 (the selected row); >1 indicates wrap/bleed", grayLines)
	}
}

// TestRenderModelRowProgressBar verifies a downloading row renders its inline
// progress bar without exceeding the panel content width (no wrap).
func TestRenderModelRowProgressBar(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	const width = 56 // typical left-panel content width
	lm := LocalModel{
		Name:        "gemma-4-12B-it-qat-UD-Q4_K_XL.gguf",
		Quant:       "Q4_K_XL",
		SizeDisplay: "3.1 GB",
		Status:      StatusDownloading,
		Progress:    0.42,
		SizeBytes:   3_100_000_000,
		TotalBytes:  6_260_000_000,
	}

	row := renderModelRow(lm, false, width, 0)

	// Must fit on one line within the content width.
	if w := lipgloss.Width(row); w > width {
		t.Errorf("downloading row width = %d, want <= %d (overflow/wrap)", w, width)
	}

	// The progress percentage must actually be present (bar shown).
	if !strings.Contains(row, "42%") {
		t.Errorf("downloading row missing progress percentage %q; bar was not rendered", "42%")
	}
}

func TestFirstShardName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Non-shard — unchanged.
		{"model.gguf", "model.gguf"},
		{"Qwen2.5-7B-Q4_K_M.gguf", "Qwen2.5-7B-Q4_K_M.gguf"},
		// Shard 1 — unchanged.
		{"model-00001-of-00002.gguf", "model-00001-of-00002.gguf"},
		// Shard 2 — mapped to shard 1.
		{
			"Qwen2.5-Coder-14B-Instruct-Q4_K_M-00002-of-00002.gguf",
			"Qwen2.5-Coder-14B-Instruct-Q4_K_M-00001-of-00002.gguf",
		},
		{
			"model-00003-of-00005.gguf",
			"model-00001-of-00005.gguf",
		},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := firstShardName(tc.input)
			if got != tc.want {
				t.Errorf("firstShardName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
