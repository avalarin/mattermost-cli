package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/avalarin/mattermost-cli/internal/mattermost"
)

const maxMemberLines = 10

// ChannelInfoPopup is an overlay popup showing channel details and a navigable members list.
// All mutation methods use value receivers and return a new ChannelInfoPopup.
type ChannelInfoPopup struct {
	channel     mattermost.Channel
	members     []mattermost.User
	loading     bool
	selectedIdx int // cursor in members list; -1 while loading
	scrollOff   int // index of first visible member

	outerW int
	outerH int
}

// NewChannelInfoPopup creates a new ChannelInfoPopup for the given channel.
// Members start in the loading state until SetMembers is called.
func NewChannelInfoPopup(channel mattermost.Channel) ChannelInfoPopup {
	return ChannelInfoPopup{
		channel:     channel,
		loading:     true,
		selectedIdx: -1,
	}
}

// SetSize stores the outer dimensions.
func (p ChannelInfoPopup) SetSize(outerW, outerH int) ChannelInfoPopup {
	p.outerW = outerW
	p.outerH = outerH
	return p
}

// SetMembers sets the loaded members list and positions the cursor at the first member.
func (p ChannelInfoPopup) SetMembers(members []mattermost.User) ChannelInfoPopup {
	p.members = members
	p.loading = false
	if len(members) > 0 {
		p.selectedIdx = 0
	} else {
		p.selectedIdx = -1
	}
	p.scrollOff = 0
	return p
}

// SetMembersError clears the loading state with an empty list (error shown in status bar).
func (p ChannelInfoPopup) SetMembersError() ChannelInfoPopup {
	p.members = nil
	p.loading = false
	p.selectedIdx = -1
	p.scrollOff = 0
	return p
}

// ChannelID returns the ID of the channel shown in this popup.
func (p ChannelInfoPopup) ChannelID() string { return p.channel.ID }

// SelectedMember returns the currently highlighted member, if any.
func (p ChannelInfoPopup) SelectedMember() (mattermost.User, bool) {
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.members) {
		return mattermost.User{}, false
	}
	return p.members[p.selectedIdx], true
}

// MoveUp moves the cursor up by one member.
func (p ChannelInfoPopup) MoveUp() ChannelInfoPopup {
	if p.selectedIdx > 0 {
		p.selectedIdx--
	}
	return p.clampScroll()
}

// MoveDown moves the cursor down by one member.
func (p ChannelInfoPopup) MoveDown() ChannelInfoPopup {
	if p.selectedIdx < len(p.members)-1 {
		p.selectedIdx++
	}
	return p.clampScroll()
}

func (p ChannelInfoPopup) clampScroll() ChannelInfoPopup {
	if p.selectedIdx < p.scrollOff {
		p.scrollOff = p.selectedIdx
	}
	if p.selectedIdx >= p.scrollOff+maxMemberLines {
		p.scrollOff = p.selectedIdx - maxMemberLines + 1
	}
	return p
}

// View renders the popup as a bordered box string (for use with lipgloss.Place).
func (p ChannelInfoPopup) View() string {
	innerW := p.outerW - 2
	if innerW < 20 {
		innerW = 20
	}
	// Fixed chrome: border(2) + title(1) + sep(1) + desc(1) + sep(1) + membersHeader(1) + sep(1) + footer(1) = 9
	const fixedChrome = 9
	visibleLines := maxMemberLines
	if p.outerH > 0 {
		avail := p.outerH - fixedChrome
		if avail < 1 {
			avail = 1
		}
		if avail < visibleLines {
			visibleLines = avail
		}
	}

	sep := func() string {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")).
			Render(strings.Repeat("─", innerW))
	}

	prefix := "#"
	if p.channel.Type == "D" || p.channel.Type == "G" {
		prefix = "@"
	}
	name := p.channel.DisplayName
	if name == "" {
		name = p.channel.Name
	}
	title := prefix + name
	if p.channel.DeleteAt > 0 {
		title = "[archived] " + title
	}
	titleLine := lipgloss.NewStyle().
		Width(innerW).
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Render(title)

	desc := p.channel.Purpose
	if desc == "" {
		desc = "No description"
	}
	descLine := lipgloss.NewStyle().Width(innerW).Foreground(lipgloss.Color("247")).Render(desc)

	var membersSection string
	if p.loading {
		membersSection = lipgloss.NewStyle().Width(innerW).Foreground(lipgloss.Color("241")).Render("Loading...")
	} else {
		header := fmt.Sprintf("Members (%d)", len(p.members))
		headerLine := lipgloss.NewStyle().Width(innerW).Bold(true).Foreground(lipgloss.Color("248")).Render(header)

		var memberLines []string
		if len(p.members) == 0 {
			memberLines = []string{
				lipgloss.NewStyle().Width(innerW).Foreground(lipgloss.Color("247")).Render("No members"),
			}
		} else {
			end := p.scrollOff + visibleLines
			if end > len(p.members) {
				end = len(p.members)
			}
			for i := p.scrollOff; i < end; i++ {
				if i == p.selectedIdx {
					memberLines = append(memberLines, lipgloss.NewStyle().
						Width(innerW).
						Background(lipgloss.Color("237")).
						Foreground(lipgloss.Color("15")).
						Bold(true).
						Render("● @"+p.members[i].Username))
				} else {
					memberLines = append(memberLines, lipgloss.NewStyle().
						Width(innerW).
						Foreground(lipgloss.Color("247")).
						Render("  @"+p.members[i].Username))
				}
			}
			if len(p.members) > p.scrollOff+visibleLines {
				remaining := len(p.members) - (p.scrollOff + visibleLines)
				memberLines = append(memberLines, lipgloss.NewStyle().
					Width(innerW).
					Foreground(lipgloss.Color("241")).
					Render(fmt.Sprintf("  ... and %d more", remaining)))
			}
		}
		membersSection = headerLine + "\n" + strings.Join(memberLines, "\n")
	}

	footerLine := lipgloss.NewStyle().
		Width(innerW).
		Foreground(lipgloss.Color("241")).
		Render("↑↓ navigate · Enter open DM · Esc close")

	inner := strings.Join([]string{
		titleLine,
		sep(),
		descLine,
		sep(),
		membersSection,
		sep(),
		footerLine,
	}, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(innerW).
		Render(inner)
}
