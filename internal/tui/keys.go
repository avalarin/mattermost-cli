package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the TUI.
type KeyMap struct {
	Up            key.Binding
	Down          key.Binding
	Left          key.Binding
	Right         key.Binding
	PageUp        key.Binding
	PageDown      key.Binding
	End           key.Binding
	Prefix        key.Binding // ctrl+b — activates prefix mode (tmux-style)
	FocusMessages key.Binding // ctrl+j
	FocusChannels key.Binding // ctrl+l → ModeChannels
	Send          key.Binding // enter
	Cancel        key.Binding // esc
	CtrlC         key.Binding // ctrl+c
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "left"),
		),
		Right: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "right"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("PgUp", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("PgDn", "page down"),
		),
		End: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("End", "jump to bottom"),
		),
		Prefix: key.NewBinding(
			key.WithKeys("ctrl+b"),
			key.WithHelp("Ctrl+B", "prefix (then ↑/↓/←/→ to navigate panels)"),
		),
		FocusMessages: key.NewBinding(
			// Note: ctrl+m == enter in standard terminals (keyCR=13).
			// Using ctrl+j (keyLF=10) which is distinct and reliably bindable.
			key.WithKeys("ctrl+j"),
			key.WithHelp("Ctrl+J", "focus messages"),
		),
		FocusChannels: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("Ctrl+L", "focus channels"),
		),
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "send/execute"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "back to input"),
		),
		CtrlC: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("Ctrl+C", "clear"),
		),
	}
}
