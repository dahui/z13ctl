// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// mainwindow.go — the full window: a real toplevel with a tab per page,
// opened by a double press of the hardware button (api.EventGUIOpenFull).
//
// It is a different surface from the drawer, not a wider one. The drawer is a
// 320px column reached in a hurry and it already shows every control; what it
// cannot show is a chart at a size worth reading, which is why the telemetry
// tab leads. Both surfaces host the same view implementations — a second
// *instance*, never a second copy of the code, which is what the per-view
// structs and the viewHost seam exist for.
//
// Which tabs exist, in what order, and how large the window may open are in
// internal/mainwin, where `make test` can reach them. What is here is the GTK.
//
// # Gamescope is not handled here yet — but not because it cannot be
//
// What does not work under gamescope is a second *toplevel*, which is what this
// file creates: only one window carries the STEAM_OVERLAY atom, and gamescope's
// GetPossibleFocusWindows() skips windows flagged isOverlay. That is a fact
// about second windows, not about screen space — and it has been mis-stated
// here as "gamescope cannot show a full window", which is wrong.
//
// The gamescope backend's window is *already fullscreen*: Configure sizes it to
// the whole output and keeps it mapped, and WrapContent puts a click-to-dismiss
// backdrop plus a right-aligned 320px panel inside it. So the full window there
// is a different **layout of the surface we already own** — swap the wrapper's
// child for full-window content and it fills the screen.
//
// HHD is the existence proof, and it is the same shape: its sidebar and its
// larger settings view are one Electron surface re-laying-out its contents, not
// two windows. Same reason its menus work where GtkDropDown does not (see the
// popup-layer notes in internal/gui/CLAUDE.md) — everything lives in the one
// surface gamescope composites.
//
// The one claim that does hold is why the drawer's *existing* view stack is not
// the answer: it lives inside the 320px panel the backend sizes, so a page
// added to it would be a 320px "full window". The seam wanted is a stack at the
// *wrapper* level — a Backend method to swap the wrapped child, which
// layer-shell and overlay satisfy by going on using this toplevel. Until it
// lands, openFull degrades to opening the drawer's own dashboard: the double
// press still gets the user to the charts, on the surface that session has.

import (
	"log/slog"

	"github.com/dahui/voltaire/v2/internal/mainwin"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// mainWindow is the full window and everything in it.
type mainWindow struct {
	w *Window

	// win is the toplevel, and is nil when the backend hosts the full window
	// inside its own surface instead (gamescope — see fullSurfaceHost). Every
	// use is guarded; host() is what the rest of this file branches on, so the
	// nil is confined to a handful of places rather than being a condition the
	// whole file has to remember.
	win *gtk.Window

	// content is the widget tree, independent of which surface holds it. It is
	// what made hosting in the backend possible at all: the tree used to be
	// built straight into m.win.SetChild, so "the full window" and "a toplevel"
	// were the same object.
	content *gtk.Box

	stack   *gtk.Stack
	tabs    []mainwin.Tab
	tabBtns map[string]*gtk.Button

	// errView is this surface's error strip. The drawer's is hidden whenever
	// this window is up, so a failure reported to it alone would be invisible.
	errView *errBarView

	// The hosted views, built with the window rather than lazily: there are two
	// of them, the tab bar has to be able to switch to either immediately, and
	// a page that appears only after its first visit is a flash the drawer's
	// lazy views get away with only because they are behind a navigation step.
	dashboard *dashboardView
	custom    *customView
}

// newMainWindow builds the full window. It is not shown; show() does that.
func newMainWindow(w *Window) *mainWindow {
	m := &mainWindow{w: w, tabs: mainwin.Resolve(w.device), tabBtns: map[string]*gtk.Button{}}

	// A toplevel only where one composites. Under gamescope the backend hosts
	// the same content inside the surface it already owns, so building a window
	// here would create one nothing ever draws — and its Escape controller
	// would be attached to it rather than to anything on screen.
	if m.w.fullHost() == nil {
		m.win = gtk.NewWindow()
		m.win.SetTitle("Voltaire")
		m.win.AddCSSClass("voltaire-main-window")
		mw, mh := mainwin.Fit(w.screenSize())
		m.win.SetDefaultSize(mw, mh)
		m.win.SetSizeRequest(mainwin.MinWidth, mainwin.MinHeight)

		// Closing the window hides it rather than destroying it: the views hold
		// daemon state and their own refresh loops, and rebuilding all of it on
		// every open would also lose the tab the user was last on. Returning
		// true stops GTK's default handler, which would dispose the toplevel.
		m.win.ConnectCloseRequest(func() bool {
			m.hide()
			return true
		})

		// Escape closes, matching the drawer. A window with no titlebar
		// affordance under some compositors would otherwise need the mouse.
		// The hosted case needs no equivalent: the drawer's own capture-phase
		// Escape handler is on the surface holding this content, and it calls
		// hide() through the same path.
		esc := gtk.NewEventControllerKey()
		esc.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
			if keyval == gdk.KEY_Escape {
				m.hide()
				return true
			}
			return false
		})
		m.win.AddController(esc)
	}

	outer := gtk.NewBox(gtk.OrientationVertical, 0)
	// .drawer carries every widget rule in the theme; .main-window flattens the
	// rounded border that suits a floating panel and not a window's interior.
	outer.AddCSSClass("drawer")
	outer.AddCSSClass("main-window")

	outer.Append(m.buildTabBar())

	m.stack = gtk.NewStack()
	m.stack.SetVExpand(true)
	outer.Append(m.stack)

	// Chrome below the pages, as in the drawer: one bar, every tab.
	m.errView = newErrorBar(w, m.isVisible)
	outer.Append(m.errView.bar)

	m.buildPages()
	m.content = outer
	if m.win != nil {
		m.win.SetChild(m.content)
	} else {
		// Hosted in the backend's surface. Installed now rather than on show so
		// the first open is a page switch and not a widget build — the same
		// reason the tab bar builds both pages up front.
		m.w.fullHost().SetFullChild(m.content)
	}

	if len(m.tabs) > 0 {
		// Selected, not synced. syncPage starts the dashboard's poll and asks
		// the daemon for a history window, and this window is not on screen
		// yet — the first show would then skip its own refresh as "already in
		// flight" and draw the chart from a reply fetched before the user
		// asked for it. show() syncs whatever page is selected.
		m.selectTab(m.tabs[0].ID)
	}
	return m
}

// buildTabBar creates the row of page buttons. It is the window's only
// navigation, which is why the hosted views build no back button of their own.
func (m *mainWindow) buildTabBar() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("btn-group")
	bar.AddCSSClass("main-tabs")
	for _, t := range m.tabs {
		t := t
		btn := gtk.NewButtonWithLabel(t.Title)
		btn.ConnectClicked(func() { m.setTab(t.ID) })
		m.tabBtns[t.ID] = btn
		bar.Append(btn)
	}
	return bar
}

// buildPages instantiates one view per resolved tab. A tab with no view behind
// it is a bug in this switch rather than in the tab list, so it is logged
// rather than silently skipped — mainwin.Resolve only ever returns IDs this
// package knows.
func (m *mainWindow) buildPages() {
	for _, t := range m.tabs {
		switch t.ID {
		case mainwin.TabDashboard:
			m.dashboard = newDashboardView(m.w, m.host(t.ID))
			m.stack.AddNamed(m.dashboard.root, t.ID)
		case mainwin.TabProfiles:
			m.custom = newCustomView(m.w, m.host(t.ID))
			m.stack.AddNamed(m.custom.root, t.ID)
		default:
			slog.Warn("full window: no view for tab", "tab", t.ID)
		}
	}
}

// host is the viewHost a view gets when built into this window: no back
// affordance (the tab bar is the navigation), and "current" means this window
// is open on that page.
func (m *mainWindow) host(tab string) viewHost {
	return viewHost{
		errBar: m.errView,
		prefix: "full:",
		current: func() bool {
			return m.isVisible() && m.stack != nil && m.stack.VisibleChildName() == tab
		},
	}
}

// isVisible reports whether the window is on screen. Read from the gamepad
// reader's goroutine through Window.anyVisible, which is why the flag it
// consults is an atomic on Window rather than a plain field here.
func (m *mainWindow) isVisible() bool { return m.w.fullVisible.Load() }

// selectTab switches pages and moves the highlight, without waking anything.
func (m *mainWindow) selectTab(id string) {
	if m.stack == nil {
		return
	}
	m.stack.SetVisibleChildName(id)
	setActiveButton(m.tabBtns, id)
}

// setTab switches pages and brings the new one up to date. What a tab button
// does.
func (m *mainWindow) setTab(id string) {
	m.selectTab(id)
	m.syncPage(id)
}

// syncPage brings the named page up to date and hands it the focus list. Also
// called on show, so a window reopened on the tab it was left on is not showing
// the values from last time.
func (m *mainWindow) syncPage(id string) {
	switch id {
	case mainwin.TabDashboard:
		if m.dashboard == nil {
			return
		}
		m.w.swapFocusList(m.dashboard.focusItems)
		m.dashboard.refresh()
		m.dashboard.startPolling()
	case mainwin.TabProfiles:
		if m.custom == nil {
			return
		}
		if m.dashboard != nil {
			m.dashboard.stopPolling()
		}
		m.w.swapFocusList(m.custom.focusItems)
		m.custom.sync()
	}
}

// show opens the window. The drawer is hidden by the caller, not here: the
// escalation is a decision about both surfaces and belongs where both are in
// view (Window.openFull).
func (m *mainWindow) show() {
	m.w.fullVisible.Store(true)
	m.w.setGamepadGrabbed(true)
	if m.win != nil {
		m.win.SetVisible(true)
		m.win.Present()
	} else {
		// Hosted: the surface is already up and already fullscreen, so this is
		// a page switch. The caller has *not* hidden the drawer in this case —
		// hiding it would take the whole surface down, the content included.
		m.w.fullHost().ShowFull(true)
	}
	m.errView.clear() // never greet an open with the last session's failure
	if id := m.stack.VisibleChildName(); id != "" {
		m.syncPage(id)
	}
	// The get-state poll keeps the hosted profile editor's readouts live. It
	// gates on anyVisible, so it runs for this window exactly as it does for
	// the drawer.
	m.w.startTelemetryPolling()
	m.w.refreshState()
}

// hide closes the window and stops everything that was running for it.
func (m *mainWindow) hide() {
	m.w.fullVisible.Store(false)
	if m.dashboard != nil {
		m.dashboard.stopPolling()
	}
	m.w.setGamepadGrabbed(false)
	m.w.hideGamepadFocus()
	if m.win != nil {
		m.win.SetVisible(false)
		m.w.telemetryGen++ // stop the get-state poll; nothing is looking at it
		return
	}
	// Hosted: swap back to the quickbar layout, then dismiss the surface
	// itself. Leaving the quickbar on screen would make the button's dismiss
	// gesture reveal a drawer instead of putting things away, which is the
	// behaviour the toplevel path deliberately does not have.
	m.w.fullHost().ShowFull(false)
	if m.w.visible.Load() {
		m.w.hide() // clears the poll and the focus list on its own path
		return
	}
	m.w.telemetryGen++
}

// openFull is the consumer for api.EventGUIOpenFull: the daemon saw a double
// press. The quickbar the first press opened is hidden and the full window
// takes its place — the brief flash is the accepted trade for never adding the
// double-press window's latency to a single press.
//
// Must be called from the GTK main thread.
func (w *Window) openFull() {
	hosted := w.fullHost() != nil
	if hosted && !w.visible.Load() {
		// The surface itself has to be up before a page inside it can be shown.
		// On the toplevel path this is the opposite of what happens — there the
		// drawer is hidden precisely because a separate window replaces it.
		w.show()
	}
	if w.mainWin == nil {
		w.mainWin = newMainWindow(w)
	}
	if !hosted && w.visible.Load() {
		w.hide()
	}
	slog.Info("gui-open-full", "action", "show full window", "hosted", hosted)
	w.mainWin.show()
}

// anyVisible reports whether either surface is on screen.
//
// It is what the gamepad reader gates on and what the get-state poll runs
// against: both serve whichever surface the user is looking at, and gating
// either on the drawer alone left the full window with a dead controller and
// frozen readouts. Safe from any goroutine — both flags are atomics.
func (w *Window) anyVisible() bool {
	return w.visible.Load() || w.fullVisible.Load()
}

// setGamepadGrabbed takes or releases the controller for a surface that is
// opening or closing, ordering the requests with grabGen exactly as show/hide
// do. Incrementing on the GTK thread is what makes the order the intended one;
// see gamepad.Reader.SetGrabbed.
func (w *Window) setGamepadGrabbed(on bool) {
	w.grabGen++
	if w.gamepadReader == nil {
		return
	}
	gen := w.grabGen
	go w.gamepadReader.SetGrabbed(gen, on)
}

// screenSize returns the pixel size of the monitor the drawer is on, or 0,0
// when it cannot be determined — which mainwin.Fit reads as "use the preferred
// size". Asked of the drawer's surface because it is realized by the time the
// full window is first built, and an unrealized window has no surface to ask
// a monitor about.
func (w *Window) screenSize() (width, height int) {
	display := gdk.DisplayGetDefault()
	if display == nil || w.gtkWin == nil {
		return 0, 0
	}
	surface := w.gtkWin.Surface()
	if surface == nil {
		return 0, 0
	}
	monitor := display.MonitorAtSurface(surface)
	if monitor == nil {
		return 0, 0
	}
	geo := monitor.Geometry()
	return geo.Width(), geo.Height()
}
