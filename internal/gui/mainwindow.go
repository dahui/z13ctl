// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// mainwindow.go — the full window: a tab per page, opened by a double press
// of the hardware button (api.EventGUIOpenFull). A real toplevel on desktop;
// hosted inside the backend's own surface under gamescope (fullSurfaceHost).
//
// It is a different surface from the drawer, not a wider one — and since the
// desktop design pass it is a different *design*: mouse/keyboard-first
// (underline tabs, ~30px controls, a card-grid dashboard) where the drawer
// stays touch-first. The drawer is a 320px column reached in a hurry and it
// already shows every control; what it cannot show is a chart at a size worth
// reading, which is why the telemetry tab leads. Both surfaces host the same
// view implementations — a second *instance*, never a second copy of the
// code, which is what the per-view structs and the viewHost seam exist for.
//
// Which tabs exist, in what order, and how large the window may open are in
// internal/mainwin, where `make test` can reach them. What is here is the GTK.
//
// # Gamescope hosts this content instead of a second toplevel
//
// What does not work under gamescope is a second *toplevel*: only one window
// carries the STEAM_OVERLAY atom, and gamescope's GetPossibleFocusWindows()
// skips windows flagged isOverlay. That is a fact about second windows, not
// about screen space — the gamescope backend's window is *already
// fullscreen*, so the full window there is a different **layout of the
// surface we already own**. fullSurfaceHost (backend.go) is the seam:
// SetFullChild installs this file's content as a sibling page of the wrapper
// stack, ShowFull swaps between it and the quickbar layout. HHD is the same
// shape — its sidebar and its larger settings view are one Electron surface
// re-laying-out its contents.
//
// The drawer's *own* view stack was never the answer: it lives inside the
// 320px panel the backend sizes, so a page added to it would be a 320px
// "full window". The hosted stack sits at the wrapper level, above the panel.

import (
	"log/slog"
	"math"

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

	// popup is this surface's popup layer, for the same reason as errView:
	// placement translates the anchor into the layer's own widget tree, so
	// the drawer's layer cannot serve a dropdown opened here — it would show
	// its scrim on the hidden drawer with nothing visible in this window.
	popup *popupLayer

	// The hosted views, built with the window rather than lazily: there are two
	// of them, the tab bar has to be able to switch to either immediately, and
	// a page that appears only after its first visit is a flash the drawer's
	// lazy views get away with only because they are behind a navigation step.
	dashboard *dashboardView
	custom    *customView
	settings  *settingsView
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

		// Reaching for the mouse dismisses the gamepad focus ring, as it does
		// in the drawer. The same-coordinate guard is the drawer's: touch
		// events arrive with a synthetic motion that would otherwise clear
		// gamepad mode on every press. The hosted path needs no equivalent —
		// the drawer's own controller sits on the surface holding the content.
		motion := gtk.NewEventControllerMotion()
		lastX, lastY := math.NaN(), math.NaN()
		motion.ConnectMotion(func(x, y float64) {
			if x == lastX && y == lastY {
				return
			}
			lastX, lastY = x, y
			if w.gamepadActive {
				w.hideGamepadFocus()
			}
		})
		m.win.AddController(motion)
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
	// The surface's own popup layer wraps the content; the overlay is what
	// gets installed. Built once, before the single SetFullChild call, so the
	// hosted path's install-once contract holds.
	m.popup = w.newPopupLayer(outer)
	if m.win != nil {
		m.win.SetChild(m.popup.overlay)
	} else {
		// Hosted in the backend's surface. Installed now rather than on show so
		// the first open is a page switch and not a widget build — the same
		// reason the tab bar builds both pages up front.
		m.w.fullHost().SetFullChild(m.popup.overlay)
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
// Zero spacing: underline tabs read as one strip, not a group of buttons.
// .btn-group stays on the bar deliberately — the underline styling wins by
// specificity in theme-default.css, and a verbatim theme.css written before
// those rules existed then falls back to its own filled-button look instead
// of stock GTK colours (see the CSS architecture entry in CLAUDE.md).
func (m *mainWindow) buildTabBar() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 0)
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
		case mainwin.TabSettings:
			m.settings = newSettingsView(m.w, m.host(t.ID))
			m.stack.AddNamed(m.settings.root, t.ID)
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

// cycleTab steps to the neighbouring tab — the gamepad bumpers' gesture while
// this window is up. Clamped rather than wrapping, matching jumpSection's
// edge behaviour: a held bumper settles on the last tab instead of spinning.
func (m *mainWindow) cycleTab(dir int) {
	if m.stack == nil || len(m.tabs) == 0 {
		return
	}
	cur := m.stack.VisibleChildName()
	idx := 0
	for i, t := range m.tabs {
		if t.ID == cur {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.tabs) {
		idx = len(m.tabs) - 1
	}
	if m.tabs[idx].ID != cur {
		m.setTab(m.tabs[idx].ID)
	}
}

// activeScroll is the scroller of the page on screen, for D-pad navigation
// and the wheel — the window-side answer to the drawer's Window.activeScroll.
func (m *mainWindow) activeScroll() *gtk.ScrolledWindow {
	if m.stack == nil {
		return nil
	}
	switch m.stack.VisibleChildName() {
	case mainwin.TabDashboard:
		if m.dashboard != nil {
			return m.dashboard.scroll
		}
	case mainwin.TabProfiles:
		if m.custom != nil {
			return m.custom.scroll
		}
	case mainwin.TabSettings:
		if m.settings != nil {
			return m.settings.scroll
		}
	}
	return nil
}

// setTab switches pages and brings the new one up to date. What a tab button
// does.
func (m *mainWindow) setTab(id string) {
	// A view switch closes any open popup — the drawer's rule: a dropdown
	// left open over a page change strands its focus frame over a vanished
	// anchor.
	m.w.closePopup()
	m.selectTab(id)
	m.syncPage(id)
}

// syncPage brings the named page up to date and hands it the focus list. Also
// called on show, so a window reopened on the tab it was left on is not showing
// the values from last time.
func (m *mainWindow) syncPage(id string) {
	// Leaving the dashboard stops its history poll, whichever page we land on.
	// This was written inside the Profiles case while that was the only other
	// page — behind a third it would have left a few-hundred-sample round trip
	// per second running for a page with no chart on it.
	if id != mainwin.TabDashboard && m.dashboard != nil {
		m.dashboard.stopPolling()
	}
	switch id {
	case mainwin.TabDashboard:
		if m.dashboard == nil {
			return
		}
		m.w.swapFocusList(m.dashboard.focusItems)
		m.dashboard.syncControls()
		m.dashboard.refresh()
		m.dashboard.startPolling()
	case mainwin.TabProfiles:
		if m.custom == nil {
			return
		}
		m.w.swapFocusList(m.custom.focusItems)
		m.custom.sync()
	case mainwin.TabSettings:
		if m.settings == nil {
			return
		}
		m.w.swapFocusList(m.settings.focusItems)
		m.settings.sync()
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
	// Before fullVisible flips: closePopup sweeps every layer regardless, but
	// the pop of the suspended focus list must happen while this surface's
	// popup still reads as open.
	m.w.closePopup()
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
