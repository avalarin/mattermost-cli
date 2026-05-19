package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ChannelSortOrder is the sort key for the channel list.
type ChannelSortOrder int

const (
	ChannelSortAlphabetical ChannelSortOrder = iota
	ChannelSortLastMessage
)

// ChannelFilterState holds the sort+filter configuration for the channel list.
type ChannelFilterState struct {
	SortOrder  ChannelSortOrder
	UnreadOnly bool
}

// ChannelFilterPopup is the Ctrl+K sort/filter overlay.
// All methods use value receivers and return a new ChannelFilterPopup.
type ChannelFilterPopup struct {
	pending  ChannelFilterState // edits in progress, not yet applied
	original ChannelFilterState // saved on open, restored on Esc
	cursor   int                // 0=Alphabetical, 1=Last message, 2=Unread only
	outerW   int
	innerW   int
}

const filterPopupRows = 3 // Alphabetical, Last message, Unread only

// NewChannelFilterPopup creates a new ChannelFilterPopup with the given initial state.
func NewChannelFilterPopup(current ChannelFilterState) ChannelFilterPopup {
	return ChannelFilterPopup{
		pending:  current,
		original: current,
		cursor:   0,
	}
}

// SetSize stores the outer dimensions and derives innerW.
func (p ChannelFilterPopup) SetSize(outerW, _ int) ChannelFilterPopup {
	p.outerW = outerW
	p.innerW = outerW - 2
	if p.innerW < 1 {
		p.innerW = 1
	}
	return p
}

// MoveUp moves the cursor up, clamped to [0, filterPopupRows-1].
func (p ChannelFilterPopup) MoveUp() ChannelFilterPopup {
	if p.cursor > 0 {
		p.cursor--
	}
	return p
}

// MoveDown moves the cursor down, clamped to [0, filterPopupRows-1].
func (p ChannelFilterPopup) MoveDown() ChannelFilterPopup {
	if p.cursor < filterPopupRows-1 {
		p.cursor++
	}
	return p
}

// Toggle applies the change for the currently highlighted row.
func (p ChannelFilterPopup) Toggle() ChannelFilterPopup {
	switch p.cursor {
	case 0:
		p.pending.SortOrder = ChannelSortAlphabetical
	case 1:
		p.pending.SortOrder = ChannelSortLastMessage
	case 2:
		p.pending.UnreadOnly = !p.pending.UnreadOnly
	}
	return p
}

// Pending returns the current (not-yet-applied) filter state.
func (p ChannelFilterPopup) Pending() ChannelFilterState {
	return p.pending
}

// Original returns the state at the time the popup was opened (for Esc restore).
func (p ChannelFilterPopup) Original() ChannelFilterState {
	return p.original
}

// View renders the popup as a bordered box string.
func (p ChannelFilterPopup) View() string {
	w := p.innerW
	if w < 1 {
		w = 1
	}

	titleLine := lipgloss.NewStyle().
		Width(w).
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Render("Sort & Filter")

	sep := func() string {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")).
			Render(strings.Repeat("─", w))
	}

	sectionHeader := func(label string) string {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("241")).
			Render(label)
	}

	renderRow := func(idx int, symbol, label string) string {
		text := " " + symbol + " " + label
		switch {
		case idx == p.cursor:
			return lipgloss.NewStyle().
				Width(w).
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("15")).
				Render(text)
		case (idx == 0 && p.pending.SortOrder == ChannelSortAlphabetical) ||
			(idx == 1 && p.pending.SortOrder == ChannelSortLastMessage) ||
			(idx == 2 && p.pending.UnreadOnly):
			return lipgloss.NewStyle().
				Width(w).
				Foreground(lipgloss.Color("15")).
				Render(text)
		default:
			return lipgloss.NewStyle().
				Width(w).
				Foreground(lipgloss.Color("241")).
				Render(text)
		}
	}

	alphaSymbol := "○"
	if p.pending.SortOrder == ChannelSortAlphabetical {
		alphaSymbol = "●"
	}
	lastSymbol := "○"
	if p.pending.SortOrder == ChannelSortLastMessage {
		lastSymbol = "●"
	}
	unreadSymbol := "☐"
	if p.pending.UnreadOnly {
		unreadSymbol = "☑"
	}

	footer := lipgloss.NewStyle().
		Width(w).
		Foreground(lipgloss.Color("241")).
		Render("↑↓ move  Space toggle  Enter apply  Esc cancel")

	inner := strings.Join([]string{
		titleLine,
		sep(),
		sectionHeader("SORT"),
		renderRow(0, alphaSymbol, "Alphabetical"),
		renderRow(1, lastSymbol, "Last message"),
		"",
		sectionHeader("FILTERS"),
		renderRow(2, unreadSymbol, "Unread only"),
		sep(),
		footer,
	}, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(p.innerW).
		Height(10). // 10 content lines + 2 border = 12 outer height
		Render(inner)
}
