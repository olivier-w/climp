package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func isQuit(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return true
	}
	return false
}

// keyMap defines all keybindings for the help component.
type keyMap struct {
	Pause      key.Binding
	Seek       key.Binding
	Volume     key.Binding
	Repeat     key.Binding
	Speed      key.Binding
	Shuffle    key.Binding
	Visualizer key.Binding
	NextTrack  key.Binding
	PrevTrack  key.Binding
	Scroll     key.Binding
	Play       key.Binding
	Remove     key.Binding
	Save       key.Binding
	Help       key.Binding
	Quit       key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Pause: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "pause"),
		),
		Seek: key.NewBinding(
			key.WithKeys("left", "right"),
			key.WithHelp("←/→", "seek"),
		),
		Volume: key.NewBinding(
			key.WithKeys("+", "-"),
			key.WithHelp("+/-", "volume"),
		),
		Repeat: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "repeat"),
		),
		Speed: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "speed"),
		),
		Shuffle: key.NewBinding(
			key.WithKeys("z"),
			key.WithHelp("z", "shuffle"),
			key.WithDisabled(),
		),
		Visualizer: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "visualizer"),
		),
		NextTrack: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "next track"),
			key.WithDisabled(),
		),
		PrevTrack: key.NewBinding(
			key.WithKeys("N", "p"),
			key.WithHelp("N/p", "prev track"),
			key.WithDisabled(),
		),
		Scroll: key.NewBinding(
			key.WithKeys("j", "k"),
			key.WithHelp("j/k", "scroll"),
			key.WithDisabled(),
		),
		Play: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "play"),
			key.WithDisabled(),
		),
		Remove: key.NewBinding(
			key.WithKeys("delete", "backspace"),
			key.WithHelp("del", "remove"),
			key.WithDisabled(),
		),
		Save: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "save"),
			key.WithDisabled(),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
	}
}

// updateEnabled enables or disables conditional bindings.
func (k *keyMap) updateEnabled(canSave bool, hasQueue bool) {
	k.NextTrack.SetEnabled(hasQueue)
	k.PrevTrack.SetEnabled(hasQueue)
	k.Scroll.SetEnabled(hasQueue)
	k.Play.SetEnabled(hasQueue)
	k.Remove.SetEnabled(hasQueue)
	k.Shuffle.SetEnabled(hasQueue)
	k.Save.SetEnabled(canSave)
}

// ShortHelp returns the keybindings shown in the collapsed help view.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Pause, k.Seek, k.Volume, k.Help, k.Quit}
}

// FullHelp returns keybindings organized into columns for the expanded help view.
func (k keyMap) FullHelp() [][]key.Binding {
	playback := []key.Binding{k.Pause, k.Seek, k.Volume, k.Repeat, k.Speed, k.Shuffle, k.Visualizer}
	queue := []key.Binding{k.NextTrack, k.PrevTrack, k.Scroll, k.Play, k.Remove}
	other := []key.Binding{k.Save, k.Help, k.Quit}
	return [][]key.Binding{playback, queue, other}
}
