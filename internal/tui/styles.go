package tui

import "github.com/charmbracelet/lipgloss"

// Styles holds all lipgloss styles for the TUI.
type Styles struct {
	Header    lipgloss.Style
	Feed      lipgloss.Style
	StatusBar lipgloss.Style
	Input     lipgloss.Style
}

// DefaultStyles returns the default styles.
func DefaultStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Padding(0, 1),
		Feed: lipgloss.NewStyle().
			Padding(0, 1),
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1),
		Input: lipgloss.NewStyle().
			Padding(0, 1),
	}
}
