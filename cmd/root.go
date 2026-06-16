// Package cmd provides the CLI entrypoint for llama-tui.
package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/patriciodanos/llama-tui/internal/app"
	"github.com/patriciodanos/llama-tui/internal/config"
)

// Version is set at build time via -ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "llama-tui",
	Short:   "A TUI for managing and running LLM models via llama-server",
	Version: Version,
	RunE:    runTUI,
}

// Execute is the entrypoint called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	model := app.New(cfg, Version)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Wire the program back into the manager so async events (server start,
	// log lines, etc.) can be delivered to the TUI.
	model.SetProgram(p)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}
