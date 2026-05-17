package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// buildHelpContent renders the full help popup content as a plain string.
// contentWidth is the inner width (inside the popup border).
func buildHelpContent(keys KeyMap, registry *Registry, contentWidth int) string {
	divStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true)
	comingSoonStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)

	hline := func(w int) string {
		return divStyle.Render(strings.Repeat("─", w))
	}

	sectionHeader := func(title string) string {
		return " " + headerStyle.Render(title) + "\n" + hline(contentWidth)
	}

	keyRow := func(keyStr, desc string) string {
		keyStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render(keyStr)
		// Two leading spaces; key field occupies up to 18 chars of the row.
		const keyFieldWidth = 18
		padding := keyFieldWidth - len([]rune(keyStr))
		if padding < 1 {
			padding = 1
		}
		return "  " + keyStyled + strings.Repeat(" ", padding) + desc
	}

	var sections []string

	// Global section.
	sections = append(sections,
		sectionHeader("Global"),
		keyRow("Ctrl+C, Ctrl+C", "Exit application"),
	)

	// Two-column section: Channel Panel | Messages Feed + Thread Panel.
	halfW := contentWidth / 2
	leftW := halfW
	rightW := contentWidth - halfW - 1 // -1 for the │ separator

	leftLines := []string{
		" " + headerStyle.Render("Channel Panel"),
		hline(leftW),
		keyRow("↑ / ↓", "Navigate channels"),
		keyRow("Enter", "Open channel"),
		keyRow("Esc", "Return to input"),
		keyRow("Ctrl+B ↓", "Return to input"),
		keyRow("Ctrl+B →", "Focus messages"),
	}

	rightLines := []string{
		" " + headerStyle.Render("Messages Feed"),
		hline(rightW),
		keyRow("↑ / ↓", "Navigate messages"),
		keyRow("PgUp/PgDn", "Page navigation"),
		keyRow("End", "Jump to bottom"),
		keyRow("Esc", "Return to input"),
		keyRow("Ctrl+B ↓", "Return to input"),
		keyRow("Ctrl+B ←", "Focus channels"),
		keyRow("/", "Start a command"),
	}

	// Pad both columns to the same height.
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	zipped := zipColumns(leftLines, rightLines, leftW, rightW, divStyle)
	// Append Thread Panel below the Messages Feed column.
	threadDivider := strings.Repeat(" ", leftW) + divStyle.Render("│") + hline(rightW)
	threadHeader := strings.Repeat(" ", leftW) + divStyle.Render("│") + " " + headerStyle.Render("Thread Panel")
	threadSoon := strings.Repeat(" ", leftW) + divStyle.Render("│") + "  " + comingSoonStyle.Render("(coming soon)")
	zipped = append(zipped, threadDivider, threadHeader, threadSoon, "")

	sections = append(sections, strings.Join(zipped, "\n"))

	// Input Field section.
	sections = append(sections,
		sectionHeader("Input Field"),
		keyRow("Enter", "Send / execute command"),
		keyRow("Alt/Opt+Enter", "Insert newline"),
		keyRow("↑  (empty input)", "Go to messages"),
		keyRow("Ctrl+J", "Go to messages"),
		keyRow("Ctrl+B ↑", "Go to messages"),
		keyRow("Ctrl+L", "Go to channels"),
		keyRow("Ctrl+B ←", "Go to channels"),
		keyRow("Esc, Esc", "Clear input & deselect"),
	)

	// Commands section.
	sections = append(sections,
		sectionHeader("Commands"),
		keyRow("/help", "Open keyboard shortcuts"),
		keyRow("/send <t> <text>", "Send a message"),
		keyRow("/quit", "Exit application"),
	)

	return strings.Join(sections, "\n")
}

// zipColumns merges two string slices side-by-side with a │ separator.
// Each left line is padded to leftW; right lines are left as-is.
func zipColumns(left, right []string, leftW, _ int, divStyle lipgloss.Style) []string {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	result := make([]string, n)
	sep := divStyle.Render("│")
	for i := range result {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		result[i] = lipgloss.NewStyle().Width(leftW).Render(l) + sep + r
	}
	return result
}
