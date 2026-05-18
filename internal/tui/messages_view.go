package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
	"github.com/avalarin/mattermost-cli/internal/store"
)

// feedItemKind distinguishes the two kinds of item that can appear in the feed.
type feedItemKind int

const (
	feedItemKindMessage feedItemKind = iota
	feedItemKindSystem
)

// feedMessage stores raw message data for re-rendering on resize.
type feedMessage struct {
	post        mattermost.Message
	senderName  string
	channelName string
	isDM        bool // true for DM/group channels — suppresses channel prefix in header
}

// feedItem is a union type for the feed: either a chat message or a system-generated line.
type feedItem struct {
	kind     feedItemKind
	createAt int64       // unix milliseconds; used for stable chronological ordering
	msg      feedMessage // valid when kind == feedItemKindMessage
	system   string      // valid when kind == feedItemKindSystem (pre-formatted text)
}

const bodyIndent = "  "

// MessagesView holds the messages feed state and viewport.
// All mutation methods use value receivers and return a new MessagesView.
type MessagesView struct {
	vp             viewport.Model
	feedItems      []feedItem
	selectedIdx    int
	lineOffsets    []int
	atBottom       bool
	store          *store.Store
	width          int    // total width of this panel
	height         int    // total height (including header line)
	ready          bool
	fullDateFormat string // Go time format for dates outside today (e.g. "02.01.2006")
}

// NewMessagesView creates a new MessagesView with the given store.
func NewMessagesView(st *store.Store) MessagesView {
	return MessagesView{
		store:          st,
		selectedIdx:    -1,
		atBottom:       true,
		fullDateFormat: "02.01.2006",
	}
}

// SetFullDateFormat sets the date format used for messages not from today.
func (mv MessagesView) SetFullDateFormat(format string) MessagesView {
	if format != "" {
		mv.fullDateFormat = format
	}
	return mv
}

// SetSize sets the width and height of the messages view and creates or resizes the viewport.
// The viewport gets height-1 lines (first line is reserved for the header rendered by the caller).
func (mv MessagesView) SetSize(w, h int) MessagesView {
	mv.width = w
	mv.height = h
	vpH := h - 1
	if vpH < 0 {
		vpH = 0
	}
	if !mv.ready {
		mv.vp = viewport.New(w, vpH)
		mv.ready = true
	} else {
		mv.vp.Width = w
		mv.vp.Height = vpH
	}
	mv = mv.rerenderFeed()
	return mv
}

// AddFeedItem inserts a new feed item in chronological order (by createAt),
// rerenders, and auto-scrolls if atBottom.
func (mv MessagesView) AddFeedItem(item feedItem) MessagesView {
	i := sort.Search(len(mv.feedItems), func(j int) bool {
		return mv.feedItems[j].createAt > item.createAt
	})
	mv.feedItems = slices.Insert(mv.feedItems, i, item)
	mv = mv.rerenderFeed()
	if mv.atBottom {
		mv.vp.GotoBottom()
	}
	return mv
}

// SetFeedItems replaces all items, sorts them chronologically, and rerenders.
func (mv MessagesView) SetFeedItems(items []feedItem) MessagesView {
	sort.Slice(items, func(i, j int) bool {
		return items[i].createAt < items[j].createAt
	})
	mv.feedItems = items
	mv = mv.rerenderFeed()
	return mv
}

// SelectLast selects the last feedItemKindMessage.
func (mv MessagesView) SelectLast() MessagesView {
	for i := len(mv.feedItems) - 1; i >= 0; i-- {
		if mv.feedItems[i].kind == feedItemKindMessage {
			mv.selectedIdx = i
			mv = mv.scrollToSelected()
			return mv
		}
	}
	mv.selectedIdx = -1
	return mv
}

// ClearSelection sets selectedIdx=-1 and rerenders.
func (mv MessagesView) ClearSelection() MessagesView {
	mv.selectedIdx = -1
	mv = mv.rerenderFeed()
	return mv
}

// MoveCursorUp moves the selection to the previous message.
func (mv MessagesView) MoveCursorUp() MessagesView {
	for i := mv.selectedIdx - 1; i >= 0; i-- {
		if mv.feedItems[i].kind == feedItemKindMessage {
			mv.selectedIdx = i
			mv.atBottom = false
			mv = mv.rerenderFeed()
			return mv.scrollToSelected()
		}
	}
	return mv // already at top
}

// MoveCursorDown moves the selection to the next message.
func (mv MessagesView) MoveCursorDown() MessagesView {
	for i := mv.selectedIdx + 1; i < len(mv.feedItems); i++ {
		if mv.feedItems[i].kind == feedItemKindMessage {
			mv.selectedIdx = i
			if !mv.hasSelectableAfter(i) {
				mv.atBottom = true
				mv.vp.GotoBottom()
			}
			mv = mv.rerenderFeed()
			return mv.scrollToSelected()
		}
	}
	return mv // already at bottom
}

// GotoBottom sets atBottom=true and scrolls the viewport to the bottom.
func (mv MessagesView) GotoBottom() MessagesView {
	mv.atBottom = true
	mv.vp.GotoBottom()
	return mv
}

// SetAtBottom explicitly sets the atBottom flag.
func (mv MessagesView) SetAtBottom(v bool) MessagesView {
	mv.atBottom = v
	return mv
}

// PageSize returns the number of messages to skip per PageUp/PageDown.
func (mv MessagesView) PageSize() int {
	n := mv.vp.Height / 3
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}
	return n
}

// rerenderFeed rebuilds the viewport content from stored feed items.
func (mv MessagesView) rerenderFeed() MessagesView {
	if len(mv.feedItems) == 0 {
		if mv.ready {
			mv.vp.SetContent("Waiting for messages...")
		}
		mv.lineOffsets = nil
		return mv
	}

	parts := make([]string, 0, len(mv.feedItems))
	offsets := make([]int, len(mv.feedItems))
	lineCount := 0

	for idx, item := range mv.feedItems {
		offsets[idx] = lineCount
		var rendered string
		switch item.kind {
		case feedItemKindMessage:
			fm := item.msg
			snippet := ""
			if fm.post.RootID != "" && mv.store != nil {
				snippet = mv.store.GetParentSnippet(fm.post.RootID)
			}
			rendered = renderMessageLine(fm.post, fm.senderName, fm.channelName, snippet, mv.fullDateFormat, mv.width, fm.isDM)
			if idx == mv.selectedIdx {
				rendered = highlightBlock(rendered, mv.width)
			}
		case feedItemKindSystem:
			rendered = item.system
		}
		parts = append(parts, rendered)
		lineCount += strings.Count(rendered, "\n") + 1
	}

	mv.lineOffsets = offsets
	if mv.ready {
		mv.vp.SetContent(strings.Join(parts, "\n"))
	}
	return mv
}

// scrollToSelected scrolls the viewport to show the selected message.
func (mv MessagesView) scrollToSelected() MessagesView {
	if !mv.ready {
		return mv
	}
	if mv.selectedIdx < 0 || mv.selectedIdx >= len(mv.lineOffsets) {
		return mv
	}
	mv.vp.SetYOffset(mv.lineOffsets[mv.selectedIdx])
	return mv
}

// hasSelectableAfter returns true if there is a feedItemKindMessage after index i.
func (mv MessagesView) hasSelectableAfter(i int) bool {
	for j := i + 1; j < len(mv.feedItems); j++ {
		if mv.feedItems[j].kind == feedItemKindMessage {
			return true
		}
	}
	return false
}

// SelectedItem returns a copy of the currently selected feed item and true, or zero value and false.
func (mv MessagesView) SelectedItem() (feedItem, bool) {
	if mv.selectedIdx < 0 || mv.selectedIdx >= len(mv.feedItems) {
		return feedItem{}, false
	}
	return mv.feedItems[mv.selectedIdx], true
}

// UpdateVP forwards a tea.Msg to the underlying viewport.
func (mv MessagesView) UpdateVP(msg tea.Msg) (MessagesView, tea.Cmd) {
	var cmd tea.Cmd
	mv.vp, cmd = mv.vp.Update(msg)
	return mv, cmd
}

// View returns the viewport content (NOT the header; the caller renders the header separately).
func (mv MessagesView) View() string {
	return mv.vp.View()
}

// AtBottom returns the atBottom flag.
func (mv MessagesView) AtBottom() bool {
	return mv.atBottom
}

// AtTop returns true when the viewport Y offset is 0 (scrolled to the top).
func (mv MessagesView) AtTop() bool {
	return mv.vp.YOffset == 0
}

// IsEmpty returns true when there are no feed items.
func (mv MessagesView) IsEmpty() bool {
	return len(mv.feedItems) == 0
}

// VPYOffset returns the viewport Y offset (for tests).
func (mv MessagesView) VPYOffset() int {
	return mv.vp.YOffset
}

// highlightBlock applies a dark-gray background to all lines of a multi-line string.
func highlightBlock(s string, width int) string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("237")).
		Width(width)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = style.Render(l)
	}
	return strings.Join(lines, "\n")
}

// renderMessageLine formats a single message for display in the feed.
// Each message renders as 2–5 lines: a header line then up to 3 indented body
// lines, plus an overflow indicator when the text exceeds 3 lines.
// Thread replies include ↩ in the header and may show a parent snippet.
// isDM suppresses the channel prefix for direct message channels.
// fullDateFormat is the Go time layout used when the message is not from today.
func renderMessageLine(msg mattermost.Message, senderName, channelName, snippet, fullDateFormat string, width int, isDM bool) string {
	msgTime := time.UnixMilli(msg.CreateAt)
	now := time.Now()
	var ts string
	if msgTime.Year() == now.Year() && msgTime.YearDay() == now.YearDay() {
		ts = msgTime.Format("15:04")
	} else {
		layout := fullDateFormat
		if layout == "" {
			layout = "02.01.2006"
		}
		ts = msgTime.Format(layout + " 15:04")
	}

	var headerLine string
	if isDM {
		// DM channels: no channel prefix, just sender
		if msg.RootID != "" {
			if snippet != "" {
				snippet = strings.ReplaceAll(snippet, "\n", " ")
				headerLine = fmt.Sprintf("[%s] ↩ @%s  (%s)", ts, senderName, snippet)
			} else {
				headerLine = fmt.Sprintf("[%s] ↩ @%s", ts, senderName)
			}
		} else {
			headerLine = fmt.Sprintf("[%s] @%s", ts, senderName)
		}
	} else if msg.RootID != "" {
		if snippet != "" {
			snippet = strings.ReplaceAll(snippet, "\n", " ")
			headerLine = fmt.Sprintf("[%s] #%s  ↩ @%s  (%s)", ts, channelName, senderName, snippet)
		} else {
			headerLine = fmt.Sprintf("[%s] #%s  ↩ @%s", ts, channelName, senderName)
		}
	} else {
		headerLine = fmt.Sprintf("[%s] #%s  @%s", ts, channelName, senderName)
	}

	// Word-wrap the body, accounting for indent, and cap at 3 visible lines.
	wrapWidth := width - len([]rune(bodyIndent))
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	if width <= 0 {
		wrapWidth = 120
	}
	bodyLines := wrapText(msg.Text, wrapWidth)

	overflowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		Italic(true)

	allLines := []string{headerLine}
	if len(bodyLines) <= 3 {
		for _, l := range bodyLines {
			allLines = append(allLines, bodyIndent+l)
		}
	} else {
		for _, l := range bodyLines[:3] {
			allLines = append(allLines, bodyIndent+l)
		}
		remaining := len(bodyLines) - 3
		indicator := overflowStyle.Render(fmt.Sprintf("⌄⌄⌄  %d more lines", remaining))
		allLines = append(allLines, bodyIndent+indicator)
	}

	return strings.Join(allLines, "\n")
}

// wrapText splits text into lines of at most width runes, breaking on word boundaries.
// Newlines in the source split into separate paragraphs; blank lines (whitespace-only) are dropped.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var result []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}
		var line strings.Builder
		lineWidth := 0
		for _, word := range words {
			wordWidth := len([]rune(word))
			if lineWidth == 0 {
				line.WriteString(word)
				lineWidth = wordWidth
			} else if lineWidth+1+wordWidth <= width {
				line.WriteByte(' ')
				line.WriteString(word)
				lineWidth += 1 + wordWidth
			} else {
				result = append(result, line.String())
				line.Reset()
				line.WriteString(word)
				lineWidth = wordWidth
			}
		}
		if lineWidth > 0 {
			result = append(result, line.String())
		}
	}
	return result
}
