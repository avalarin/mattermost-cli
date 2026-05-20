package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

const allActivityID = "" // sentinel for All Activity filter

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

type channelItem struct {
	channel mattermost.Channel
	isAll   bool // true for the All Activity entry
}

// ChannelsView renders the channels sidebar.
// All mutation methods use value receivers and return a new ChannelsView.
type ChannelsView struct {
	items       []channelItem
	selectedIdx int // cursor position
	openIdx     int // currently open channel (-1 initially, 0 = All Activity)
	scrollOff   int // index of first visible item
	width           int
	height          int            // total height including header
	active          bool           // true when the channels panel has keyboard focus
	activeHeaderFg  string         // foreground color for header when active
	activeHeaderBg  string         // background color for header when active
	unreadCounts    map[string]int // channelID → unread count (set at render time)
}

// NewChannelsView creates a ChannelsView with All Activity pinned first,
// then channels sorted alphabetically by DisplayName (fallback to Name).
func NewChannelsView(channels []mattermost.Channel) ChannelsView {
	items := []channelItem{{isAll: true}}
	sorted := make([]mattermost.Channel, len(channels))
	copy(sorted, channels)
	sort.Slice(sorted, func(i, j int) bool {
		ni := sorted[i].DisplayName
		if ni == "" {
			ni = sorted[i].Name
		}
		nj := sorted[j].DisplayName
		if nj == "" {
			nj = sorted[j].Name
		}
		return strings.ToLower(ni) < strings.ToLower(nj)
	})
	for _, ch := range sorted {
		items = append(items, channelItem{channel: ch})
	}
	return ChannelsView{
		items:       items,
		selectedIdx: 0,
		openIdx:     0, // All Activity is open by default
	}
}

// SetSize sets the width and height of the channels view.
func (cv ChannelsView) SetSize(w, h int) ChannelsView {
	cv.width = w
	cv.height = h
	return cv
}

// ChannelList returns the slice of channels (excluding the All Activity sentinel),
// with resolved DM display names already applied.
func (cv ChannelsView) ChannelList() []mattermost.Channel {
	channels := make([]mattermost.Channel, 0, len(cv.items))
	for _, item := range cv.items {
		if !item.isAll {
			channels = append(channels, item.channel)
		}
	}
	return channels
}

// contentHeight returns the number of content rows (total height minus header row).
func (cv ChannelsView) contentHeight() int {
	h := cv.height - 1
	if h < 0 {
		h = 0
	}
	return h
}

// pageSize returns the number of items to scroll per PageUp/PageDown.
func (cv ChannelsView) pageSize() int {
	n := cv.contentHeight() / 2
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}
	return n
}

// MoveUp moves the cursor up by one item.
func (cv ChannelsView) MoveUp() ChannelsView {
	if cv.selectedIdx > 0 {
		cv.selectedIdx--
	}
	return cv.clampScroll()
}

// MoveDown moves the cursor down by one item.
func (cv ChannelsView) MoveDown() ChannelsView {
	if cv.selectedIdx < len(cv.items)-1 {
		cv.selectedIdx++
	}
	return cv.clampScroll()
}

// PageUp moves the cursor up by pageSize items.
func (cv ChannelsView) PageUp() ChannelsView {
	cv.selectedIdx -= cv.pageSize()
	if cv.selectedIdx < 0 {
		cv.selectedIdx = 0
	}
	return cv.clampScroll()
}

// PageDown moves the cursor down by pageSize items.
func (cv ChannelsView) PageDown() ChannelsView {
	cv.selectedIdx += cv.pageSize()
	if cv.selectedIdx >= len(cv.items) {
		cv.selectedIdx = len(cv.items) - 1
	}
	return cv.clampScroll()
}

// clampScroll adjusts scrollOff so selectedIdx stays within the visible window.
func (cv ChannelsView) clampScroll() ChannelsView {
	ch := cv.contentHeight()
	if ch <= 0 {
		cv.scrollOff = 0
		return cv
	}
	if cv.selectedIdx < cv.scrollOff {
		cv.scrollOff = cv.selectedIdx
	}
	if cv.selectedIdx >= cv.scrollOff+ch {
		cv.scrollOff = cv.selectedIdx - ch + 1
	}
	return cv
}

// SelectedChannelID returns the channel ID of the selected item ("" for All Activity).
func (cv ChannelsView) SelectedChannelID() string {
	if cv.selectedIdx < 0 || cv.selectedIdx >= len(cv.items) {
		return allActivityID
	}
	item := cv.items[cv.selectedIdx]
	if item.isAll {
		return allActivityID
	}
	return item.channel.ID
}

// OpenSelected sets openIdx to selectedIdx and returns the channel ID of the opened item.
func (cv ChannelsView) OpenSelected() (ChannelsView, string) {
	cv.openIdx = cv.selectedIdx
	return cv, cv.SelectedChannelID()
}

// SetOpenByID sets openIdx to the item with the given channel ID.
func (cv ChannelsView) SetOpenByID(channelID string) ChannelsView {
	for i, item := range cv.items {
		if item.isAll && channelID == allActivityID {
			cv.openIdx = i
			return cv
		}
		if !item.isAll && item.channel.ID == channelID {
			cv.openIdx = i
			return cv
		}
	}
	return cv
}

// IsSelectedArchived returns true if the selected channel has DeleteAt > 0.
func (cv ChannelsView) IsSelectedArchived() bool {
	if cv.selectedIdx < 0 || cv.selectedIdx >= len(cv.items) {
		return false
	}
	item := cv.items[cv.selectedIdx]
	if item.isAll {
		return false
	}
	return item.channel.DeleteAt > 0
}

// DisplayNameByID returns the display label for the given channel ID.
// Returns "All Activity" for the empty string sentinel and for unknown IDs.
func (cv ChannelsView) DisplayNameByID(channelID string) string {
	if channelID == allActivityID {
		return "All Activity"
	}
	for _, item := range cv.items {
		if !item.isAll && item.channel.ID == channelID {
			return channelLabel(item)
		}
	}
	return "All Activity"
}

// SetActive sets whether this panel has keyboard focus, controlling the header accent.
func (cv ChannelsView) SetActive(active bool) ChannelsView {
	cv.active = active
	return cv
}

// SetActiveFg sets the foreground color for the header when the panel is active.
func (cv ChannelsView) SetActiveFg(color string) ChannelsView {
	cv.activeHeaderFg = color
	return cv
}

// SetActiveBg sets the background color for the header when the panel is active.
func (cv ChannelsView) SetActiveBg(color string) ChannelsView {
	cv.activeHeaderBg = color
	return cv
}

// SetUnreadCounts sets the unread message counts used when rendering channel labels.
// The map is copied defensively so future mutations to the caller's map don't affect
// counts already captured in a ChannelsView value.
func (cv ChannelsView) SetUnreadCounts(counts map[string]int) ChannelsView {
	cp := make(map[string]int, len(counts))
	for k, v := range counts {
		cp[k] = v
	}
	cv.unreadCounts = cp
	return cv
}

// ApplyDMNames updates DisplayName for DM channels from the given map (channelID → displayName).
func (cv ChannelsView) ApplyDMNames(names map[string]string) ChannelsView {
	cv.items = append([]channelItem(nil), cv.items...)
	for i, item := range cv.items {
		if item.isAll {
			continue
		}
		if name, ok := names[item.channel.ID]; ok {
			cv.items[i].channel.DisplayName = name
		}
	}
	return cv
}

// WithSortAndFilter returns a new ChannelsView with sort order and unread filter applied.
// All Activity is always pinned at index 0. nil unreadCounts means no filtering.
// Both selectedIdx and openIdx are preserved by channel ID after reordering; if the
// previously selected channel was filtered out, selectedIdx falls back to 0.
func (cv ChannelsView) WithSortAndFilter(state ChannelFilterState, unreadCounts map[string]int) ChannelsView {
	// Capture channel IDs to restore cursor positions after reorder/filter.
	var openChannelID, selectedChannelID string
	if cv.openIdx >= 0 && cv.openIdx < len(cv.items) {
		if item := cv.items[cv.openIdx]; !item.isAll {
			openChannelID = item.channel.ID
		}
	}
	if cv.selectedIdx >= 0 && cv.selectedIdx < len(cv.items) {
		if item := cv.items[cv.selectedIdx]; !item.isAll {
			selectedChannelID = item.channel.ID
		}
	}

	var allItem channelItem
	var rest []channelItem
	for _, item := range cv.items {
		if item.isAll {
			allItem = item
		} else {
			rest = append(rest, item)
		}
	}

	if state.UnreadOnly {
		filtered := make([]channelItem, 0, len(rest))
		for _, item := range rest {
			if unreadCounts[item.channel.ID] > 0 {
				filtered = append(filtered, item)
			}
		}
		rest = filtered
	}

	switch state.SortOrder {
	case ChannelSortLastMessage:
		sort.Slice(rest, func(i, j int) bool {
			return rest[i].channel.LastPostAt > rest[j].channel.LastPostAt
		})
	default:
		sort.Slice(rest, func(i, j int) bool {
			ni := rest[i].channel.DisplayName
			if ni == "" {
				ni = rest[i].channel.Name
			}
			nj := rest[j].channel.DisplayName
			if nj == "" {
				nj = rest[j].channel.Name
			}
			return strings.ToLower(ni) < strings.ToLower(nj)
		})
	}

	items := make([]channelItem, 0, len(rest)+1)
	items = append(items, allItem)
	items = append(items, rest...)
	cv.items = items

	// Restore openIdx by channel ID; fall back to 0 (All Activity) if not found.
	cv.openIdx = 0
	if openChannelID != "" {
		for i, item := range cv.items {
			if !item.isAll && item.channel.ID == openChannelID {
				cv.openIdx = i
				break
			}
		}
	}

	// Restore selectedIdx by channel ID; fall back to 0 if filtered out.
	cv.selectedIdx = 0
	cv.scrollOff = 0
	if selectedChannelID != "" {
		for i, item := range cv.items {
			if !item.isAll && item.channel.ID == selectedChannelID {
				cv.selectedIdx = i
				break
			}
		}
	}
	cv = cv.clampScroll()
	return cv
}

// SelectedDisplayName returns the display name of the selected channel
// ("All Activity" for the aggregate filter).
func (cv ChannelsView) SelectedDisplayName() string {
	if cv.selectedIdx < 0 || cv.selectedIdx >= len(cv.items) {
		return "All Activity"
	}
	item := cv.items[cv.selectedIdx]
	if item.isAll {
		return "All Activity"
	}
	if item.channel.DisplayName != "" {
		return item.channel.DisplayName
	}
	return item.channel.Name
}

// channelLabel returns the display label for a channel item.
// Format: "#name", "@name" (for DMs/groups), or "[x] #name" (archived).
func channelLabel(item channelItem) string {
	if item.isAll {
		return "All Activity"
	}
	ch := item.channel
	prefix := "#"
	if ch.Type == "D" || ch.Type == "G" {
		prefix = "@"
	}
	name := ch.DisplayName
	if name == "" {
		name = ch.Name
	}
	label := prefix + name
	if ch.DeleteAt > 0 {
		label = "[x] " + label
	}
	return label
}

// renderLabel builds the display label for a channel item, appending the unread
// count badge when count > 0. The result fits within maxWidth runes.
func (cv ChannelsView) renderLabel(item channelItem, maxWidth int) string {
	if item.isAll {
		return truncateLabel("All Activity", maxWidth)
	}
	count := cv.unreadCounts[item.channel.ID]
	if count > 0 {
		suffix := fmt.Sprintf(" (%d)", count)
		suffixW := len([]rune(suffix))
		baseW := maxWidth - suffixW
		if baseW < 1 {
			baseW = 1
		}
		return truncateLabel(channelLabel(item), baseW) + suffix
	}
	return truncateLabel(channelLabel(item), maxWidth)
}

// truncateLabel truncates label with "…" if it exceeds maxWidth runes.
func truncateLabel(label string, maxWidth int) string {
	runes := []rune(label)
	if len(runes) <= maxWidth {
		return label
	}
	if maxWidth <= 1 {
		return "…"
	}
	return string(runes[:maxWidth-1]) + "…"
}

// View renders the full channels panel (header + list).
func (cv ChannelsView) View() string {
	var sb strings.Builder

	// Header line: active panel uses configured accent color; inactive is dimmed.
	var headerStyle lipgloss.Style
	if cv.active {
		fg := cv.activeHeaderFg
		if fg == "" {
			fg = "15"
		}
		bg := cv.activeHeaderBg
		if bg == "" {
			bg = "237"
		}
		headerStyle = lipgloss.NewStyle().
			Bold(true).
			Width(cv.width).
			Foreground(lipgloss.Color(fg)).
			Background(lipgloss.Color(bg))
	} else {
		headerStyle = lipgloss.NewStyle().Bold(true).Width(cv.width).Foreground(lipgloss.Color("241"))
	}
	sb.WriteString(headerStyle.Render("Channels"))

	ch := cv.contentHeight()
	end := cv.scrollOff + ch
	if end > len(cv.items) {
		end = len(cv.items)
	}

	normalStyle := lipgloss.NewStyle().Width(cv.width)
	selectedStyle := lipgloss.NewStyle().Width(cv.width).Background(lipgloss.Color("237"))
	openStyle := lipgloss.NewStyle().Width(cv.width).Background(lipgloss.Color("15")).Foreground(lipgloss.Color("0"))
	archivedStyle := lipgloss.NewStyle().Width(cv.width).Foreground(lipgloss.Color("241"))

	maxLabelW := cv.width
	if maxLabelW < 1 {
		maxLabelW = 1
	}

	for i := cv.scrollOff; i < end; i++ {
		item := cv.items[i]
		label := cv.renderLabel(item, maxLabelW)

		var line string
		switch {
		case i == cv.openIdx && i == cv.selectedIdx:
			// Open and cursor: show open style (open takes priority visually).
			line = openStyle.Render(label)
		case i == cv.openIdx:
			line = openStyle.Render(label)
		case i == cv.selectedIdx:
			line = selectedStyle.Render(label)
		case !item.isAll && item.channel.DeleteAt > 0:
			line = archivedStyle.Render(label)
		default:
			line = normalStyle.Render(label)
		}
		sb.WriteByte('\n')
		sb.WriteString(line)
	}

	// Pad remaining lines so the panel always occupies the full height.
	rendered := end - cv.scrollOff
	for i := rendered; i < ch; i++ {
		sb.WriteByte('\n')
		sb.WriteString(normalStyle.Render(""))
	}

	return sb.String()
}
