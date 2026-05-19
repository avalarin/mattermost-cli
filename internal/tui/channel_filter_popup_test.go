package tui

import "testing"

// TestNewChannelFilterPopup_DefaultState verifies cursor starts at 0 and pending matches initial state.
func TestNewChannelFilterPopup_DefaultState(t *testing.T) {
	state := ChannelFilterState{SortOrder: ChannelSortLastMessage, UnreadOnly: true}
	p := NewChannelFilterPopup(state)

	if p.cursor != 0 {
		t.Errorf("expected cursor=0, got %d", p.cursor)
	}
	if p.Pending().SortOrder != ChannelSortLastMessage {
		t.Error("pending SortOrder should match initial state")
	}
	if !p.Pending().UnreadOnly {
		t.Error("pending UnreadOnly should match initial state")
	}
}

// TestChannelFilterPopup_MoveUpDown verifies cursor movement is clamped at [0, filterPopupRows-1].
func TestChannelFilterPopup_MoveUpDown(t *testing.T) {
	p := NewChannelFilterPopup(ChannelFilterState{})

	// At top: MoveUp should clamp at 0.
	p = p.MoveUp()
	if p.cursor != 0 {
		t.Errorf("expected cursor=0 after MoveUp at top, got %d", p.cursor)
	}

	// Move to bottom.
	p = p.MoveDown().MoveDown()
	if p.cursor != filterPopupRows-1 {
		t.Errorf("expected cursor=%d, got %d", filterPopupRows-1, p.cursor)
	}

	// At bottom: MoveDown should clamp.
	p = p.MoveDown()
	if p.cursor != filterPopupRows-1 {
		t.Errorf("expected cursor to stay at %d after MoveDown at bottom, got %d", filterPopupRows-1, p.cursor)
	}
}

// TestChannelFilterPopup_Toggle_Sort verifies Space on rows 0 and 1 sets the sort order.
func TestChannelFilterPopup_Toggle_Sort(t *testing.T) {
	p := NewChannelFilterPopup(ChannelFilterState{SortOrder: ChannelSortAlphabetical})

	// Row 0: Alphabetical (already set, stays).
	p = p.Toggle()
	if p.Pending().SortOrder != ChannelSortAlphabetical {
		t.Error("cursor=0 toggle should set SortOrder=Alphabetical")
	}

	// Row 1: Last message.
	p = p.MoveDown()
	p = p.Toggle()
	if p.Pending().SortOrder != ChannelSortLastMessage {
		t.Error("cursor=1 toggle should set SortOrder=LastMessage")
	}

	// Back to row 0 reverts sort to alphabetical.
	p = p.MoveUp()
	p = p.Toggle()
	if p.Pending().SortOrder != ChannelSortAlphabetical {
		t.Error("cursor=0 toggle should reset SortOrder=Alphabetical")
	}
}

// TestChannelFilterPopup_Toggle_Unread verifies Space on row 2 toggles UnreadOnly.
func TestChannelFilterPopup_Toggle_Unread(t *testing.T) {
	p := NewChannelFilterPopup(ChannelFilterState{})

	// Navigate to row 2.
	p = p.MoveDown().MoveDown()
	if p.cursor != 2 {
		t.Fatalf("expected cursor=2, got %d", p.cursor)
	}

	// First toggle: turns on.
	p = p.Toggle()
	if !p.Pending().UnreadOnly {
		t.Error("first toggle should enable UnreadOnly")
	}

	// Second toggle: turns off.
	p = p.Toggle()
	if p.Pending().UnreadOnly {
		t.Error("second toggle should disable UnreadOnly")
	}
}

// TestChannelFilterPopup_OriginalPreservedOnEsc verifies Original is immutable after Toggle.
func TestChannelFilterPopup_OriginalPreservedOnEsc(t *testing.T) {
	initial := ChannelFilterState{SortOrder: ChannelSortAlphabetical, UnreadOnly: false}
	p := NewChannelFilterPopup(initial)

	// Toggle sort to LastMessage.
	p = p.MoveDown()
	p = p.Toggle()

	// Pending should differ.
	if p.Pending().SortOrder != ChannelSortLastMessage {
		t.Error("pending SortOrder should be LastMessage after toggle")
	}

	// Original should be unchanged.
	orig := p.Original()
	if orig.SortOrder != ChannelSortAlphabetical {
		t.Error("original SortOrder should remain Alphabetical")
	}
	if orig.UnreadOnly != false {
		t.Error("original UnreadOnly should remain false")
	}
}
