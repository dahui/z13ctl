// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// windowcontrols.go — the full window's own title row and window controls, and
// the minimize bookkeeping they need.
//
// The window used to leave minimize, maximize and close to the window manager,
// on the reasoning that anything drawn here would be invisible under gamescope
// (2026-08-13). That premise was backwards: under gamescope the full window is
// a page inside voltaire's own overlay surface, so content drawn here is the
// *only* thing that shows, and a manager's controls never existed. Jeff found
// the window there with no way to close it by touch (2026-11-15), so the row is
// ours now and looks the same everywhere. Which buttons exist is
// mainwin.Controls: the hosted surface is already fullscreen and drops
// maximize.
//
// # Minimize is a hide that the compositor can undo
//
// A minimized window is not on screen, so it must stop being served: the
// controller grab released (hidblocker otherwise keeps the pad from the game),
// the polls stopped, fullVisible false so the hardware button opens the drawer
// instead of "hiding" a window nobody can see. Before this file existed, the
// GTK header's own minimize button did none of that.
//
// Wayland's xdg-shell has no minimized state, so the signal is the toplevel's
// *suspended* state, which KWin sends about 80 ms after a minimize and clears
// on restore — measured with a throwaway probe, 2026-11-15. It covers every
// minimize, the compositor's own (Meta+PgDown, a taskbar click) included, and
// also a window parked on another virtual desktop, which is not on screen
// either. A compositor that sends no suspended state still gets the minimize
// button's direct bookkeeping, and a restore there is caught by the window
// becoming active again.
//
// Bringing a minimized window back programmatically does not work on KWin:
// Present() is refused by focus-stealing prevention, and KWin keeps a window
// minimized across an unmap and remap, even one a second long. A fresh
// toplevel maps normally, so show() rebuilds it and moves the content across
// (replaceToplevel). Both behaviours were measured with the same probe.

import (
	"log/slog"

	"github.com/dahui/voltaire/v2/internal/mainwin"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// minimizeCheckMS is how long after a minimize request to look whether it took.
// KWin drops focus at once and suspends within ~80 ms; a compositor that
// ignored the request leaves the window focused.
const minimizeCheckMS = 500

// buildHeader builds the title row: the app name, then the controls.
// On a toplevel it is wrapped in a GtkWindowHandle, which supplies
// drag-to-move, double-click to maximize and the right-click window menu. The
// hosted page gets no handle: a drag there would ask gamescope to move the
// fullscreen overlay window itself.
func (m *mainWindow) buildHeader() gtk.Widgetter {
	hosted := m.w.fullHost() != nil

	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.AddCSSClass("window-header")

	title := gtk.NewLabel("VOLTAIRE")
	title.AddCSSClass("window-title")
	title.SetHAlign(gtk.AlignStart)
	title.SetHExpand(true)
	row.Append(title)

	controls := gtk.NewBox(gtk.OrientationHorizontal, 2)
	controls.AddCSSClass("window-controls")
	controls.SetVAlign(gtk.AlignCenter)
	for _, c := range mainwin.Controls(hosted) {
		btn := gtk.NewButton()
		switch c {
		case mainwin.Minimize:
			btn.SetIconName("window-minimize-symbolic")
			m.w.setHint(btn, "Minimize")
			btn.ConnectClicked(m.minimize)
		case mainwin.Maximize:
			btn.SetIconName("window-maximize-symbolic")
			m.w.setHint(btn, "Maximize")
			btn.ConnectClicked(m.toggleMaximized)
			m.maxBtn = btn
		case mainwin.Close:
			btn.SetIconName("window-close-symbolic")
			btn.AddCSSClass("window-close")
			m.w.setHint(btn, "Close")
			btn.ConnectClicked(m.hide)
		}
		btn.SetFocusable(false)
		controls.Append(btn)
	}
	row.Append(controls)

	if hosted {
		return row
	}
	handle := gtk.NewWindowHandle()
	handle.SetChild(row)
	return handle
}

// toggleMaximized is the maximize button. The icon is not set here: it follows
// the window's own maximized property (watchToplevel), so a double-click on
// the title row or a compositor shortcut moves it too.
func (m *mainWindow) toggleMaximized() {
	if m.win == nil {
		return
	}
	if m.win.IsMaximized() {
		m.win.Unmaximize()
	} else {
		m.win.Maximize()
	}
}

// syncMaxIcon shows restore while maximized and maximize otherwise. The hint
// text is changed in the map rather than through setHint, which would stack a
// second motion controller on the button every time; showHint reads the map
// when the hint appears.
func (m *mainWindow) syncMaxIcon() {
	if m.maxBtn == nil || m.win == nil {
		return
	}
	icon, hint := "window-maximize-symbolic", "Maximize"
	if m.win.IsMaximized() {
		icon, hint = "window-restore-symbolic", "Restore"
	}
	m.maxBtn.SetIconName(icon)
	m.w.hints[coreglib.BaseObject(m.maxBtn).Native()] = hint
}

// minimize is the minimize button. Hosted, it is a hide: there is no taskbar
// to restore from, and the hardware button or the drawer's expand button brings
// the window back. On a toplevel it stops serving the window first, so nothing
// waits on the compositor's suspended state to release the controller.
func (m *mainWindow) minimize() {
	if m.win == nil {
		m.hide()
		return
	}
	if m.w.fullVisible.Load() {
		m.deactivate()
	}
	m.minimized = true
	win := m.win
	win.Minimize()
	glib.TimeoutAdd(minimizeCheckMS, func() bool {
		// Still focused and still this window: the compositor did not
		// minimize it, so it is on screen and must be served again.
		if m.minimized && m.win == win && win.IsVisible() && win.IsActive() {
			slog.Debug("full window: minimize was not honoured, resuming")
			m.minimized = false
			m.activate()
		}
		return false
	})
}

// watchToplevel connects the state the controls and the minimize bookkeeping
// follow. Called for every toplevel newToplevel builds.
func (m *mainWindow) watchToplevel(win *gtk.Window) {
	win.NotifyProperty("maximized", func() {
		if win == m.win {
			m.syncMaxIcon()
		}
	})
	// A restore on a compositor that sends no suspended state.
	win.NotifyProperty("is-active", func() {
		if win == m.win && win.IsActive() && m.minimized && win.IsVisible() {
			m.restored("activated")
		}
	})
	win.ConnectRealize(func() {
		tl := &gdk.Toplevel{Surface: *gdk.BaseSurface(win.Surface())}
		tl.NotifyProperty("state", func() {
			if win != m.win {
				return
			}
			suspended := tl.State()&gdk.ToplevelStateSuspended != 0
			switch {
			case suspended && m.w.fullVisible.Load():
				slog.Debug("full window: suspended by the compositor")
				m.minimized = true
				m.deactivate()
			case !suspended && m.minimized && win.IsVisible():
				m.restored("unsuspended")
			}
		})
	})
}

// restored resumes serving a window the compositor brought back.
func (m *mainWindow) restored(how string) {
	slog.Debug("full window: restored", "by", how)
	m.minimized = false
	m.activate()
}

// replaceToplevel swaps a minimized toplevel for a fresh one holding the same
// content, keeping its size and maximized state. See the file comment for why
// the existing window cannot simply be presented again.
func (m *mainWindow) replaceToplevel() {
	old := m.win
	width, height := old.Width(), old.Height()
	maxed := old.IsMaximized()
	old.SetVisible(false)
	old.SetChild(nil)
	old.Destroy()

	m.win = m.newToplevel()
	if width > 0 && height > 0 {
		m.win.SetDefaultSize(width, height)
	}
	if maxed {
		m.win.Maximize()
	}
	m.minimized = false
	slog.Debug("full window: rebuilt the toplevel to bring it back from minimized")
}
