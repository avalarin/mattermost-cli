package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// ThreadPopup is an overlay popup that displays a message thread.
// It follows the value-receiver pattern of MessagesView.
type ThreadPopup struct {
	vp            viewport.Model
	feedItems     []feedItem
	selectedIdx   int
	lineOffsets   []int
	ready         bool
	outerW        int // total width including border
	outerH        int // total height including border
	innerW        int // content width (outerW - 2)
	viewportH     int // viewport height (outerH - 6: 2 border + 1 title + 1 sep + 1 footerSep + 1 footer)
	rootID        string
	channelName   string
	CurrentUserID string // set from client.CurrentUserID() after creation
}

// NewThreadPopup creates a new ThreadPopup for the given root message ID.
func NewThreadPopup(rootID, channelName string) ThreadPopup {
	return ThreadPopup{
		rootID:      rootID,
		channelName: channelName,
		selectedIdx: -1,
	}
}

// SetSize sets the outer dimensions of the popup and initializes/resizes the viewport.
func (tp ThreadPopup) SetSize(outerW, outerH int) ThreadPopup {
	tp.outerW = outerW
	tp.outerH = outerH
	tp.innerW = outerW - 2
	if tp.innerW < 1 {
		tp.innerW = 1
	}
	// innerH = outerH - 2 (border top+bottom)
	// viewportH = innerH - 4 (title line + title separator + footer separator + footer line)
	tp.viewportH = outerH - 6
	if tp.viewportH < 1 {
		tp.viewportH = 1
	}
	if !tp.ready {
		tp.vp = viewport.New(tp.innerW, tp.viewportH)
		tp.ready = true
	} else {
		tp.vp.Width = tp.innerW
		tp.vp.Height = tp.viewportH
	}
	return tp.rerenderFeed()
}

// SetFeedItems replaces all feed items, sorts them chronologically, and rerenders.
func (tp ThreadPopup) SetFeedItems(items []feedItem) ThreadPopup {
	sort.Slice(items, func(i, j int) bool {
		return items[i].createAt < items[j].createAt
	})
	tp.feedItems = items
	return tp.rerenderFeed()
}

// SelectByID selects the feedItem whose post ID matches postID.
// Falls back to SelectLast if not found.
func (tp ThreadPopup) SelectByID(postID string) ThreadPopup {
	for i, item := range tp.feedItems {
		if item.kind == feedItemKindMessage && item.msg.post.ID == postID {
			tp.selectedIdx = i
			tp = tp.rerenderFeed()
			return tp.scrollToSelected()
		}
	}
	return tp.SelectLast()
}

// AddFeedItem inserts a new feed item in chronological order and rerenders.
func (tp ThreadPopup) AddFeedItem(item feedItem) ThreadPopup {
	i := sort.Search(len(tp.feedItems), func(j int) bool {
		return tp.feedItems[j].createAt > item.createAt
	})
	tp.feedItems = slices.Insert(tp.feedItems, i, item)
	return tp.rerenderFeed()
}

// SelectLast selects the last message item.
func (tp ThreadPopup) SelectLast() ThreadPopup {
	for i := len(tp.feedItems) - 1; i >= 0; i-- {
		if tp.feedItems[i].kind == feedItemKindMessage {
			tp.selectedIdx = i
			return tp.scrollToSelected()
		}
	}
	tp.selectedIdx = -1
	return tp
}

// MoveCursorUp moves the selection to the previous message.
func (tp ThreadPopup) MoveCursorUp() ThreadPopup {
	for i := tp.selectedIdx - 1; i >= 0; i-- {
		if tp.feedItems[i].kind == feedItemKindMessage {
			tp.selectedIdx = i
			tp = tp.rerenderFeed()
			return tp.scrollToSelected()
		}
	}
	return tp
}

// MoveCursorDown moves the selection to the next message.
func (tp ThreadPopup) MoveCursorDown() ThreadPopup {
	for i := tp.selectedIdx + 1; i < len(tp.feedItems); i++ {
		if tp.feedItems[i].kind == feedItemKindMessage {
			tp.selectedIdx = i
			tp = tp.rerenderFeed()
			return tp.scrollToSelected()
		}
	}
	return tp
}

// PageSize returns the number of items to skip per page.
func (tp ThreadPopup) PageSize() int {
	n := tp.viewportH / 3
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}
	return n
}

// SelectedItem returns the currently selected feed item, if any.
func (tp ThreadPopup) SelectedItem() (feedItem, bool) {
	if tp.selectedIdx < 0 || tp.selectedIdx >= len(tp.feedItems) {
		return feedItem{}, false
	}
	return tp.feedItems[tp.selectedIdx], true
}

// rerenderFeed rebuilds the viewport content.
func (tp ThreadPopup) rerenderFeed() ThreadPopup {
	if len(tp.feedItems) == 0 {
		if tp.ready {
			tp.vp.SetContent("No messages")
		}
		tp.lineOffsets = nil
		return tp
	}
	parts := make([]string, 0, len(tp.feedItems))
	offsets := make([]int, len(tp.feedItems))
	lineCount := 0
	for idx, item := range tp.feedItems {
		offsets[idx] = lineCount
		var rendered string
		if item.kind == feedItemKindMessage {
			rendered = renderMessageLine(item.msg.post, item.msg.senderName, item.msg.channelName,
				"", "", tp.innerW, item.msg.isDM, false, true)
			if idx == tp.selectedIdx {
				rendered = highlightBlock(rendered, tp.innerW)
			}
		} else {
			rendered = item.system
		}
		parts = append(parts, rendered)
		lineCount += strings.Count(rendered, "\n") + 1
	}
	tp.lineOffsets = offsets
	if tp.ready {
		tp.vp.SetContent(strings.Join(parts, "\n"))
	}
	return tp
}

// scrollToSelected scrolls the viewport to show the selected message.
func (tp ThreadPopup) scrollToSelected() ThreadPopup {
	if !tp.ready {
		return tp
	}
	if tp.selectedIdx < 0 || tp.selectedIdx >= len(tp.lineOffsets) {
		return tp
	}
	tp.vp.SetYOffset(tp.lineOffsets[tp.selectedIdx])
	return tp
}

// View renders the popup as a bordered box string (for use with lipgloss.Place).
func (tp ThreadPopup) View() string {
	title := "Thread"
	if tp.channelName != "" {
		// channelName already includes the sigil (#general, @alice) from channelLabel.
		title = fmt.Sprintf("Thread — %s", tp.channelName)
	}

	titleLine := lipgloss.NewStyle().
		Width(tp.innerW).
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Render(title)

	titleSep := lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		Render(strings.Repeat("─", tp.innerW))

	content := ""
	if tp.ready {
		content = tp.vp.View()
	}

	footerSep := lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		Render(strings.Repeat("─", tp.innerW))

	footer := lipgloss.NewStyle().
		Width(tp.innerW).
		Foreground(lipgloss.Color("241")).
		Render("r reply · e edit · d delete · Esc close")

	inner := strings.Join([]string{titleLine, titleSep, content, footerSep, footer}, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(tp.innerW).
		Height(tp.outerH - 2).
		Render(inner)
}

