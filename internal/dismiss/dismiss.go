// Package dismiss decides what a "put it away" gesture closes: the innermost
// thing on screen first. The order was written out in each handler (gamepad B,
// the hardware button), and when one handler checked the drawer before the full
// window, B could not close the window at all.
package dismiss

// State is what is on screen.
type State struct {
	Editing       bool // a slider is in gamepad edit mode
	PopupOpen     bool // a dropdown or the colour picker is open
	FullVisible   bool // the full window is up
	SubView       bool // the drawer shows a page other than its main one
	DrawerVisible bool // the drawer is up
}

// Action is what to close.
type Action int

// The actions, innermost first.
const (
	None       Action = iota // nothing is up (the hardware button then opens)
	ExitEdit                 // leave gamepad edit mode, keeping the value
	ClosePopup               // close the popup, not the surface beneath it
	HideFull                 // close the full window
	BackToMain               // return the drawer to its main page
	HideDrawer               // close the drawer
)

// Back is gamepad B: one level out. A popup closes before the surface beneath
// it (closing the surface first would strand the popup's focus frame), and the
// full window before the drawer (the drawer is hidden while it is up).
func Back(s State) Action {
	switch {
	case s.Editing:
		return ExitEdit
	case s.PopupOpen:
		return ClosePopup
	case s.FullVisible:
		return HideFull
	case s.SubView:
		return BackToMain
	default:
		return HideDrawer // B arrives only while something is up
	}
}

// Toggle is the hardware button: put away whatever surface is in front, or
// None when nothing is, in which case the caller opens the user's primary
// surface. The full window wins: the button must mean the same thing on both
// surfaces, and reading only the drawer's visibility opened the drawer behind
// a window the user was looking at.
func Toggle(s State) Action {
	switch {
	case s.FullVisible:
		return HideFull
	case s.DrawerVisible:
		return HideDrawer
	default:
		return None
	}
}
