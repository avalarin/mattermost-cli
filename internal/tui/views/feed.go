package views

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// FeedView displays the message feed using a viewport.
type FeedView struct {
	viewport viewport.Model
	ready    bool
}

// NewFeedView creates a new FeedView.
func NewFeedView() FeedView {
	return FeedView{}
}

// SetSize resizes the feed view.
func (f *FeedView) SetSize(width, height int) {
	if !f.ready {
		f.viewport = viewport.New(width, height)
		f.viewport.SetContent("Waiting for messages...")
		f.ready = true
	} else {
		f.viewport.Width = width
		f.viewport.Height = height
	}
}

// Update handles messages for the feed view.
func (f FeedView) Update(msg tea.Msg) (FeedView, tea.Cmd) {
	var cmd tea.Cmd
	f.viewport, cmd = f.viewport.Update(msg)
	return f, cmd
}

// View renders the feed view.
func (f FeedView) View() string {
	if !f.ready {
		return "Waiting for messages..."
	}
	return f.viewport.View()
}
