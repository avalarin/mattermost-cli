package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

// searchResultKind identifies what a search result item represents.
type searchResultKind int

const (
	searchResultAllActivity searchResultKind = iota
	searchResultChannel
	searchResultUser
)

// searchResultItem represents one entry in the search popup result list.
type searchResultItem struct {
	kind        searchResultKind
	channel     mattermost.Channel
	user        mattermost.User
	displayName string // pre-formatted display label
}

// searchFocus tracks which section of the popup has keyboard focus.
type searchFocus int

const (
	searchFocusResults searchFocus = iota // ↑/↓ navigates the results list
	searchFocusFilter                     // ↑/↓ navigates sort/filter controls
)

const searchPopupFilterRows = 4 // Alphabetical, Last message, Unread only, Archived only

// filterRowStart is the filterCursor value for the first filter checkbox row (Unread).
const filterRowStart = 2

// SearchPopup is the unified channel/user search + sort/filter overlay (Ctrl+K).
// All mutation methods use value receivers and return a new SearchPopup.
type SearchPopup struct {
	query   string
	cursor  int
	results []searchResultItem

	// local channel list (used when query < 2 runes)
	localChannels []mattermost.Channel
	unreadCounts  map[string]int
	errMsg        string // non-empty when the last search returned a non-retryable error

	// filter state
	filter       ChannelFilterState
	original     ChannelFilterState // saved on open, restored on Esc
	filterCursor int                // 0=Alpha, 1=LastMsg, 2=Unread
	focus        searchFocus

	searching bool // true while a REST search is in-flight

	outerW int
	outerH int
}

// NewSearchPopup creates a new SearchPopup with the given initial filter state,
// channel list (with resolved DM names), and unread counts.
func NewSearchPopup(filter ChannelFilterState, localChannels []mattermost.Channel, unreadCounts map[string]int) SearchPopup {
	p := SearchPopup{
		filter:        filter,
		original:      filter,
		localChannels: localChannels,
		unreadCounts:  unreadCounts,
		focus:         searchFocusResults,
	}
	p.results = p.buildLocalResults()
	return p
}

// SetSize stores the outer dimensions.
func (p SearchPopup) SetSize(outerW, outerH int) SearchPopup {
	p.outerW = outerW
	p.outerH = outerH
	return p
}

// Query returns the current search query.
func (p SearchPopup) Query() string { return p.query }

// Filter returns the current (pending) filter state.
func (p SearchPopup) Filter() ChannelFilterState { return p.filter }

// Original returns the filter state at open time (for Esc restore).
func (p SearchPopup) Original() ChannelFilterState { return p.original }

// IsSearchMode returns true when query has 2+ runes (REST search mode).
func (p SearchPopup) IsSearchMode() bool { return len([]rune(p.query)) >= 2 }

// Focus returns the current focused section.
func (p SearchPopup) Focus() searchFocus { return p.focus }

// TypeChar appends a rune to the query and refreshes local results if not in search mode.
func (p SearchPopup) TypeChar(r rune) SearchPopup {
	p.query += string(r)
	if !p.IsSearchMode() {
		p.results = p.buildLocalResults()
		p.cursor = 0
	}
	return p
}

// Backspace removes the last rune from the query.
func (p SearchPopup) Backspace() SearchPopup {
	runes := []rune(p.query)
	if len(runes) > 0 {
		p.query = string(runes[:len(runes)-1])
	}
	if !p.IsSearchMode() {
		p.results = p.buildLocalResults()
		if p.cursor >= len(p.results) {
			p.cursor = 0
		}
		p.focus = searchFocusResults
	}
	return p
}

// SetSearchResults replaces the result list with REST search results.
func (p SearchPopup) SetSearchResults(channels []mattermost.Channel, users []mattermost.User) SearchPopup {
	items := []searchResultItem{{kind: searchResultAllActivity, displayName: "All Activity"}}
	for _, ch := range channels {
		label := channelLabel(channelItem{channel: ch})
		items = append(items, searchResultItem{kind: searchResultChannel, channel: ch, displayName: label})
	}
	for _, u := range users {
		items = append(items, searchResultItem{kind: searchResultUser, user: u, displayName: "@" + u.Username})
	}
	p.results = items
	p.searching = false
	if p.cursor >= len(p.results) {
		p.cursor = 0
	}
	return p
}

// SetSearching marks the popup as waiting for REST search results.
func (p SearchPopup) SetSearching() SearchPopup {
	p.searching = true
	p.errMsg = ""
	return p
}

// SetError clears the searching state and records a display error message.
func (p SearchPopup) SetError(msg string) SearchPopup {
	p.searching = false
	p.errMsg = msg
	p.results = nil
	return p
}

// Searching reports whether a REST search is currently in-flight.
func (p SearchPopup) Searching() bool { return p.searching }

// filterLastRow is the filterCursor value for the last filter row (Archived).
const filterLastRow = searchPopupFilterRows - 1

// MoveUp moves the cursor up in the focused section.
// In filter focus: ↑ from any checkbox row (Unread/Archived) jumps back to Sort row (cursor 0).
func (p SearchPopup) MoveUp() SearchPopup {
	switch p.focus {
	case searchFocusResults:
		if p.cursor > 0 {
			p.cursor--
		}
	case searchFocusFilter:
		if p.filterCursor >= filterRowStart {
			p.filterCursor = 0
		}
	}
	return p
}

// MoveDown moves the cursor down in the focused section.
// In filter focus: ↓ from any Sort cursor jumps to the first checkbox row (Unread).
func (p SearchPopup) MoveDown() SearchPopup {
	switch p.focus {
	case searchFocusResults:
		if p.cursor < len(p.results)-1 {
			p.cursor++
		}
	case searchFocusFilter:
		if p.filterCursor < filterRowStart {
			p.filterCursor = filterRowStart
		}
	}
	return p
}

// MoveLeft moves the filter cursor left.
// Sort row: cursor 1 → 0. Checkbox row: cursor 3 (Archived) → 2 (Unread).
func (p SearchPopup) MoveLeft() SearchPopup {
	if p.focus == searchFocusFilter {
		switch p.filterCursor {
		case 1:
			p.filterCursor = 0
		case filterLastRow:
			p.filterCursor = filterRowStart
		}
	}
	return p
}

// MoveRight moves the filter cursor right.
// Sort row: cursor 0 → 1. Checkbox row: cursor 2 (Unread) → 3 (Archived).
func (p SearchPopup) MoveRight() SearchPopup {
	if p.focus == searchFocusFilter {
		switch p.filterCursor {
		case 0:
			p.filterCursor = 1
		case filterRowStart:
			p.filterCursor = filterLastRow
		}
	}
	return p
}

// ToggleFilter toggles the highlighted filter row.
func (p SearchPopup) ToggleFilter() SearchPopup {
	switch p.filterCursor {
	case 0:
		p.filter.SortOrder = ChannelSortAlphabetical
	case 1:
		p.filter.SortOrder = ChannelSortLastMessage
	case 2:
		p.filter.UnreadOnly = !p.filter.UnreadOnly
	case filterLastRow:
		p.filter.ArchivedOnly = !p.filter.ArchivedOnly
	}
	if !p.IsSearchMode() {
		p.results = p.buildLocalResults()
	}
	return p
}

// ToggleFocus switches focus between results and filter sections (only in local mode).
func (p SearchPopup) ToggleFocus() SearchPopup {
	if p.IsSearchMode() {
		return p
	}
	if p.focus == searchFocusResults {
		p.focus = searchFocusFilter
	} else {
		p.focus = searchFocusResults
	}
	return p
}

// SelectedItem returns the currently highlighted result item.
func (p SearchPopup) SelectedItem() (searchResultItem, bool) {
	if len(p.results) == 0 || p.cursor < 0 || p.cursor >= len(p.results) {
		return searchResultItem{}, false
	}
	return p.results[p.cursor], true
}

// buildLocalResults builds the result list from localChannels, applying sort/filter.
func (p SearchPopup) buildLocalResults() []searchResultItem {
	q := strings.ToLower(p.query)

	// Copy and optionally filter.
	channels := make([]mattermost.Channel, 0, len(p.localChannels))
	for _, ch := range p.localChannels {
		if p.filter.UnreadOnly && p.unreadCounts[ch.ID] == 0 {
			continue
		}
		if p.filter.ArchivedOnly && ch.DeleteAt == 0 {
			continue
		}
		label := channelLabel(channelItem{channel: ch})
		if q != "" && !strings.Contains(strings.ToLower(label), q) {
			continue
		}
		channels = append(channels, ch)
	}

	// Sort.
	switch p.filter.SortOrder {
	case ChannelSortLastMessage:
		sort.Slice(channels, func(i, j int) bool {
			return channels[i].LastPostAt > channels[j].LastPostAt
		})
	default:
		sort.Slice(channels, func(i, j int) bool {
			ni := channels[i].DisplayName
			if ni == "" {
				ni = channels[i].Name
			}
			nj := channels[j].DisplayName
			if nj == "" {
				nj = channels[j].Name
			}
			return strings.ToLower(ni) < strings.ToLower(nj)
		})
	}

	items := make([]searchResultItem, 0, len(channels)+1)
	items = append(items, searchResultItem{kind: searchResultAllActivity, displayName: "All Activity"})
	for _, ch := range channels {
		label := channelLabel(channelItem{channel: ch})
		items = append(items, searchResultItem{kind: searchResultChannel, channel: ch, displayName: label})
	}
	return items
}

// View renders the search popup as a bordered box.
// spinnerFrame is the current spinner character (e.g. from spinner.Model.View());
// it is shown next to the query while a REST search is in flight.
func (p SearchPopup) View(spinnerFrame string) string {
	innerW := p.outerW - 2
	if innerW < 20 {
		innerW = 20
	}

	sep := func() string {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")).
			Render(strings.Repeat("─", innerW))
	}

	// Search input line — spinner replaces the ">" prompt while a REST search is in flight.
	prompt := "> "
	if p.searching && spinnerFrame != "" {
		prompt = spinnerFrame + " "
	}
	queryLine := lipgloss.NewStyle().
		Width(innerW).
		Foreground(lipgloss.Color("15")).
		Render(prompt + p.query + "█")

	// Results list: compute max visible rows from available height.
	// Fixed overhead per mode (including 2 for border):
	//   local mode:  border(2) + query(1) + sep(1) + sep(1) + header(1) + sort(1) + filter(1) + sep(1) + hotkeys(1) = 10
	//   search mode: border(2) + query(1) + sep(1) + sep(1) + hotkeys(1) = 6
	overhead := 10
	if p.IsSearchMode() {
		overhead = 6
	}
	maxVisible := p.outerH - overhead
	if maxVisible < 1 {
		maxVisible = 1
	}
	var resultLines []string
	if len(p.results) == 0 {
		emptyText := "No results"
		switch {
		case p.searching:
			emptyText = "Searching..."
		case p.errMsg != "":
			emptyText = p.errMsg
		}
		resultLines = []string{
			lipgloss.NewStyle().Width(innerW).Foreground(lipgloss.Color("241")).Render("  " + emptyText),
		}
	} else {
		start := 0
		if p.cursor >= maxVisible {
			start = p.cursor - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(p.results) {
			end = len(p.results)
		}
		for i := start; i < end; i++ {
			item := p.results[i]
			isSelected := i == p.cursor
			if isSelected && p.focus == searchFocusResults {
				resultLines = append(resultLines, lipgloss.NewStyle().
					Width(innerW).
					Background(lipgloss.Color("237")).
					Foreground(lipgloss.Color("15")).
					Bold(true).
					Render("● "+item.displayName))
			} else if isSelected {
				resultLines = append(resultLines, lipgloss.NewStyle().
					Width(innerW).
					Foreground(lipgloss.Color("15")).
					Render("● "+item.displayName))
			} else {
				resultLines = append(resultLines, lipgloss.NewStyle().
					Width(innerW).
					Foreground(lipgloss.Color("7")).
					Render("  "+item.displayName))
			}
		}
	}

	parts := []string{queryLine, sep()}
	parts = append(parts, resultLines...)

	// Filter section — only in local mode (query < 2 runes).
	if !p.IsSearchMode() {
		parts = append(parts, sep())

		alphaSymbol := "○"
		if p.filter.SortOrder == ChannelSortAlphabetical {
			alphaSymbol = "●"
		}
		lastSymbol := "○"
		if p.filter.SortOrder == ChannelSortLastMessage {
			lastSymbol = "●"
		}
		unreadSymbol := "☐"
		if p.filter.UnreadOnly {
			unreadSymbol = "☑"
		}
		archivedSymbol := "☐"
		if p.filter.ArchivedOnly {
			archivedSymbol = "☑"
		}

		renderFilterItem := func(idx int, text string) string {
			if p.focus == searchFocusFilter && idx == p.filterCursor {
				return lipgloss.NewStyle().
					Background(lipgloss.Color("237")).
					Foreground(lipgloss.Color("15")).
					Render(text)
			}
			return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(text)
		}

		headerStyle := lipgloss.NewStyle().Width(innerW).Foreground(lipgloss.Color("248")).Bold(true)
		parts = append(parts, headerStyle.Render("Channel list settings"))

		sortLine := "Sort: " +
			renderFilterItem(0, alphaSymbol+" Alpha") + "  " +
			renderFilterItem(1, lastSymbol+" Last msg")
		parts = append(parts, lipgloss.NewStyle().Width(innerW).Render(sortLine))

		filterLine := "Filter: " +
			renderFilterItem(filterRowStart, unreadSymbol+" Unread") + "  " +
			renderFilterItem(filterLastRow, archivedSymbol+" Archived")
		parts = append(parts, lipgloss.NewStyle().Width(innerW).Render(filterLine))
	}

	// Hotkeys bar.
	parts = append(parts, sep())
	var hotkeys string
	switch {
	case p.IsSearchMode():
		hotkeys = "↑↓ navigate · Enter open · Esc discard"
	case p.focus == searchFocusFilter:
		hotkeys = "↑↓←→ navigate · Space toggle · Enter apply · Tab back · Esc discard"
	default:
		hotkeys = "↑↓ navigate · Enter open · Tab filter · Esc discard"
	}
	parts = append(parts, lipgloss.NewStyle().
		Width(innerW).
		Foreground(lipgloss.Color("241")).
		Render(hotkeys))

	inner := strings.Join(parts, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(innerW).
		Render(inner)
}
