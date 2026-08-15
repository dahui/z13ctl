// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package gui implements the GTK4 overlay drawer for voltaire-gui.
// It provides the main Window type that handles daemon state synchronization,
// GTK widget construction, and theming. Display-mode-specific concerns
// (layer-shell, the gamescope X11 overlay, or the fullscreen click-through
// overlay used where layer-shell is unavailable) are delegated to Backend
// implementations.
package gui

import (
	_ "embed"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/buttonpref"
	"github.com/dahui/voltaire/v2/internal/controls"
	"github.com/dahui/voltaire/v2/internal/display"
	"github.com/dahui/voltaire/v2/internal/gui/fonts"
	"github.com/dahui/voltaire/v2/internal/gui/gamepad"
	"github.com/dahui/voltaire/v2/internal/gui/gamescope"
	"github.com/dahui/voltaire/v2/internal/gui/layershell"
	"github.com/dahui/voltaire/v2/internal/gui/overlay"
	"github.com/dahui/voltaire/v2/internal/limits"
	"github.com/dahui/voltaire/v2/internal/mainwin"
	"github.com/dahui/voltaire/v2/internal/panelgeom"
	"github.com/dahui/voltaire/v2/internal/startup"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/dahui/voltaire/v2/internal/togglegate"
	"github.com/diamondburned/gotk4-layer-shell/pkg/gtk4layershell"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

//go:embed layout.css
var layoutCSS string

//go:embed theme-default.css
var defaultThemeCSS string

//go:embed theme-default.toml
var defaultThemeTOML string

const (
	drawerWidth = 320 // drawer panel width in pixels

	// daemonToggleDebounce suppresses hardware-duplicated gui-toggle events: some
	// firmware revisions report a single Armoury Crate press twice within the same
	// evdev instant. It is deliberately NOT an animation rate limiter — deliberate
	// rapid presses must all register.
	//
	// Sizing: measured human tapping on a Z13 bottoms out around 129ms between
	// presses, so anything at or above ~120ms starts discarding real input (a 250ms
	// window swallowed 38% of presses in a 96-event sample). 50ms leaves ~2.5x
	// headroom below the human floor while still catching same-instant duplicates.
	daemonToggleDebounce = 50 * time.Millisecond
)

// Window is the overlay drawer. All methods must be called from the GTK main
// thread except subscribeLoop, which runs in a background goroutine.
type Window struct {
	win       *gtk.ApplicationWindow
	gtkWin    *gtk.Window // alias for backend calls
	backend   Backend     // display backend (layer-shell or gamescope)
	gamescope bool        // true when running under gamescope (X11 overlay mode)
	state     *api.State  // latest daemon state; nil until first successful fetch

	// visible is true when the drawer is on-screen or animating in. Atomic
	// because it is the one piece of Window state read off the GTK thread: the
	// gamepad reader's goroutine gates every event on it. A plain bool there is a
	// data race, and internal/gui is not covered by `go test -race`, so nothing
	// would ever report it. Written only from show/hide on the GTK thread.
	visible atomic.Bool

	// grabGen orders the gamepad grab/release requests show and hide issue from
	// their own goroutines. Incremented on the GTK thread, which is the only place
	// that knows the intended order. See gamepad.Reader.SetGrabbed.
	grabGen uint64

	themeProvider *gtk.CSSProvider // current theme; replaced on applyTheme()

	// colors is the active palette, kept alongside the CSS built from it because
	// the fan curve chart is painted with Cairo rather than styled by CSS and so
	// cannot read the @z13-* tokens. Without this the chart was the one part of
	// the drawer the theme did not reach.
	colors theme.Colors

	// errView is the drawer's single error surface, outside the view stack so
	// one instance serves every view. See errbar.go.
	errView *errBarView

	// limits is the device's power/thermal envelope, driving every TDP and fan
	// curve bound in the custom view. Defaulted to the Z13's values; when the daemon
	// grows an API for serving per-device limits this is the one place that
	// changes — fetch once at startup, Sanitized, falling back to the defaults.
	limits limits.Limits

	// edge is the screen edge the drawer anchors to and slides from, resolved
	// once at startup from gui.toml. All three backends take it; nothing reads
	// it after Configure.
	edge panelgeom.Edge

	// controls is which sections this drawer builds and in what order,
	// resolved once at startup from gui.toml and the device document. Both
	// buildContent and buildMainFocusList walk it, which is what keeps the
	// visual order and the gamepad order the same list rather than two.
	controls []controls.Control

	// device is the capability document, kept for the readers that need more
	// than the bounds limits.FromDevice narrows it to — the dashboard asks it
	// how much history the daemon retains. nil when the daemon did not answer,
	// which means "unknown", never "the machine has no capabilities".
	device *api.DeviceInfo

	// lighting is the RGB section of the main view; nil on a device with no
	// lighting capability, which controls.Resolve drops the section for.
	// See lightingview.go.
	lighting *lightingView

	// Widget references for syncState.
	headerTelemetry *gtk.Label // "45°C · 3200 RPM" in the header, on every view

	// Drawer main-view sections the control registry can drop independently.
	// Each is one *instance* of a block the dashboard rail also builds, so
	// nothing here is the only copy — the Window-level syncs walk both. See
	// mainprofile.go, battery.go and lightingview.go.
	profiles   *profileSection
	autoswitch *autoswitchSection
	battery    *batterySection

	// telemetryGen and telemetryBusy drive the get-state poll that keeps the
	// header live. Window-level rather than per-view: it runs for as long as
	// the drawer is visible, whichever view is showing. See
	// startTelemetryPolling.
	telemetryGen  int
	telemetryBusy bool // a poll request is in flight; skip ticks until it lands

	syncing bool // true while syncState is updating widgets; suppresses sendApply

	// View switching (main/theme/color views).
	mainScroll *gtk.ScrolledWindow // scrollable area in main drawer view
	viewStack  *gtk.Stack          // switches between the drawer's views
	paletteBtn *gtk.Button         // theme button in bottom bar

	// fullVisible is true when the full window is on screen. Atomic for the
	// same reason `visible` is: the gamepad reader's goroutine reads both
	// through anyVisible. Written only from mainWindow.show/hide on the GTK
	// thread.
	fullVisible atomic.Bool

	// mainWin is the full window (mainwindow.go); nil until a double press
	// asks for it. It hosts its own instances of the views the drawer also
	// shows, so nothing here is shared but Window itself.
	mainWin *mainWindow

	// Lazily-built stack views. Each owns its widgets, and a nil pointer is
	// also the built-yet test every show*View uses.
	//
	// The custom profile editor is deliberately not among them: the drawer
	// switches profiles from a picker and does not edit them, so the only
	// instance lives on the full window's Profiles tab (mainWindow.custom).
	themeView *themeView // theme picker (themeview.go)

	// colorPopup is the HSL picker (colorpopup.go). Not a stack view and not
	// per-surface: it is a popup body the active layer draws, so one serves the
	// drawer and the full window both.
	colorPopup *colorPopup

	// press is which surface a single press of the hardware button opens; the
	// double press opens the other. Read and written on the GTK thread only —
	// the subscribe goroutine dispatches through IdleAdd and the decision is
	// made inside that closure, so this needs no atomic. See internal/buttonpref.
	press buttonpref.Surface

	// refreshPrefs is the screen refresh rate to select on each power source,
	// and onAC/sourceKnown are the latch that makes the switch edge-triggered.
	// They live on Window rather than on the DISPLAY card because the switch
	// runs whether or not the full window was ever opened; the card is a view
	// of the pair. All main-thread-owned — refreshState's idle closure is the
	// only writer. See displayview.go.
	refreshPrefs display.Prefs
	onAC         bool
	sourceKnown  bool

	// Custom theme state (set when theme.toml exists).
	isCustomTheme bool
	customColors  theme.Colors
	customAccents []theme.Accent

	// Steam input suppression (gamescope only).
	steamBlocker gamepad.SteamInputBlocker
	steamPID     int

	// Gamepad focus navigation.
	gamepadReader     *gamepad.Reader
	focusItems        []focusItem  // active view's navigable widgets (points to one of the lists below)
	focusIdx          int          // current position in focusItems
	gamepadActive     bool         // true when gamepad focus indicator is shown
	focusEditing      bool         // true when a slider is in edit mode
	editOriginalValue float64      // saved value for cancel on B
	mainFocusItems    []focusItem  // focus grid for main drawer view
	focusStack        []focusFrame // suspended focus lists while a popup is open

	// In-surface popup layer (popup.go) and anchored hints (hint.go).
	popup   *popupLayer
	hints   map[uintptr]string // hint text keyed by widget Native(); entries are never removed
	hintGen int                // invalidates pending hint dwell timers
}

// layerShellUsable reports whether this session can actually use the layer-shell
// protocol.
//
// The GDK backend is checked before asking gtk4-layer-shell, because
// gtk_layer_is_supported() runs a g_return_val_if_fail on the display being a
// GdkWaylandDisplay. On X11 that assertion fires and GLib logs it at
// G_LOG_LEVEL_CRITICAL — so simply calling it would put
// "assertion 'GDK_IS_WAYLAND_DISPLAY(gdk_display)' failed" in the journal of
// every X11 session, immediately before z13gui went on to do the right thing.
// Diagnosing issue #16 was hard enough without the fix adding its own scary
// line to the logs.
func layerShellUsable() bool {
	gdkDisplay := gdk.DisplayGetDefault()
	if gdkDisplay == nil {
		slog.Warn("no GDK display available; assuming layer-shell is unusable")
		return false
	}
	// e.g. "GdkWaylandDisplay", "GdkX11Display".
	if backend := gdkDisplay.TypeFromInstance().Name(); !strings.Contains(backend, "Wayland") {
		slog.Debug("GDK is not using the Wayland backend, so layer-shell cannot apply",
			"gdkDisplay", backend)
		return false
	}
	return gtk4layershell.IsSupported()
}

// deviceLimits asks the daemon what this machine's power and thermal envelope
// actually is, so the drawer stops presenting one laptop's numbers on every
// machine. Any failure falls back to the built-in Z13 values — a drawer with
// slightly wrong bounds is worth having, and the daemon validates every write
// regardless, so the worst case is an option that gets refused rather than a
// setting that gets through.
//
// Fetched once, before any widget exists, because the TDP scales and the fan
// curve editor bind their ranges at construction: applying new limits later
// would mean rebuilding them, and there is no second device yet to prove such
// a path works. The document is static for the daemon's lifetime anyway
// (api/device.go), so the only case this misses is the drawer starting while
// the daemon is down — rare, since voltaire.socket is socket-activated and up
// before graphical-session.target, and harmless on the one device shipping
// today, where FromDevice and DefaultLimits agree by test.
// deviceDocument fetches the capability document once, or nil when the daemon
// cannot be reached. Two things read it — the widgets' limits and the control
// list — and they must see the same answer: fetching twice would let a daemon
// that started between the calls give the drawer bounds for a device whose
// controls it had already decided without.
func deviceDocument() *api.DeviceInfo {
	handled, info, err := api.SendDeviceGet()
	switch {
	case err != nil:
		slog.Warn("device-get failed, using built-in defaults", "err", err)
		return nil
	case !handled:
		slog.Info("daemon not running, using built-in defaults")
		return nil
	}
	return info
}

// deviceLimits derives the widgets' bounds from the document, falling back to
// the built-in Z13 values on any failure — a drawer with slightly wrong bounds
// is worth having, and the daemon validates every write anyway.
func deviceLimits(info *api.DeviceInfo) limits.Limits {
	if info == nil {
		return limits.DefaultLimits()
	}
	l := limits.FromDevice(info)
	slog.Info("device limits", "model", l.Model,
		"tdpMax", l.TDPMaxForced, "tdpSafe", l.TDPMaxSafe, "fanFloor", l.HighTDPMinPWM)
	return l
}

// guiConfig reads gui.toml once. A malformed file is logged and treated as
// absent: the drawer is the only way some users reach these settings, so a typo
// in an optional file must never be what stops it opening.
func guiConfig() controls.Config {
	cfg, err := controls.Load(theme.XDGConfigHome() + "/voltaire")
	if err != nil {
		slog.Warn("using the default drawer layout", "err", err)
	}
	return cfg
}

// resolveControls picks which sections the drawer builds and in what order:
// the user's list if they have one, filtered by what the device can actually
// do. A nil document means the daemon did not answer, which is not evidence
// that the machine lacks capabilities, so nothing is filtered out there.
func resolveControls(cfg controls.Config, info *api.DeviceInfo) []controls.Control {
	resolved, complaints := controls.Resolve(cfg, info)
	for _, c := range complaints {
		slog.Warn(c)
	}
	slog.Info("controls", "ids", controls.IDsOf(resolved))
	return resolved
}

// resolveEdge picks the screen edge the drawer lives on. Every parse failure
// still yields a usable edge, so an unrecognised value costs a warning and the
// default rather than a drawer that will not open.
func resolveEdge(cfg controls.Config) panelgeom.Edge {
	edge, err := panelgeom.ParseEdge(cfg.Quickbar.Edge)
	if err != nil {
		slog.Warn("drawer edge", "err", err)
	}
	slog.Info("drawer edge", "edge", edge)
	return edge
}

// resolveButtonPress reads which surface a single press of the hardware button
// opens. Like the edge, an unrecognised value costs a warning and the default
// rather than a button that does nothing — and unlike the edge this one is
// written by a control in Settings, so a value here that the parser rejects
// would be one the UI itself had produced.
func resolveButtonPress() buttonpref.Surface {
	raw := theme.LoadAppConfig().ButtonPress
	s, ok := buttonpref.Parse(raw)
	if !ok && raw != "" {
		slog.Warn("button preference not recognised, using the default",
			"value", raw, "using", s)
	}
	slog.Info("button preference", "single_press_opens", s)
	return s
}

// New creates the overlay window and attaches it to app. Called from the
// GTK Activate signal.
func New(app *gtk.Application) *Window {
	// One document, two readers: the widgets' bounds and the control list.
	doc := deviceDocument()
	cfg := guiConfig()
	w := &Window{
		device:       doc,
		limits:       deviceLimits(doc),
		controls:     resolveControls(cfg, doc),
		edge:         resolveEdge(cfg),
		press:        resolveButtonPress(),
		refreshPrefs: resolveRefreshPrefs(),
		colors:       theme.DefaultColors,
		gamescope:    os.Getenv("GAMESCOPE_WAYLAND_DISPLAY") != "",
	}

	w.win = gtk.NewApplicationWindow(app)
	w.win.AddCSSClass("z13-drawer-window")
	w.gtkWin = &w.win.Window

	// Select display backend.
	//
	// Layer-shell is not something every Wayland compositor has: zwlr_layer_shell_v1
	// is a wlroots extension, not part of wayland-protocols, and GNOME's Mutter has
	// never implemented it. Calling into gtk4-layer-shell anyway does not fail
	// loudly — gtk_layer_init_for_window logs one G_LOG_LEVEL_WARNING and every
	// later anchor/margin call quietly no-ops — so the drawer came up as an
	// unanchored, unsized box in the middle of the screen (issue #16). Ask first.
	switch {
	case w.gamescope:
		w.backend = gamescope.New(w.win, w.gtkWin, drawerWidth, w.edge)
	case layerShellUsable():
		w.backend = layershell.New(w.win, w.gtkWin, drawerWidth, w.edge)
	default:
		slog.Info("layer-shell unavailable, using the overlay backend "+
			"(the drawer is drawn in a transparent click-through window instead of "+
			"anchored to the screen edge)",
			"desktop", os.Getenv("XDG_CURRENT_DESKTOP"), "session", os.Getenv("XDG_SESSION_TYPE"))
		w.backend = overlay.New(w.win, w.gtkWin, drawerWidth, w.edge)
	}

	w.backend.Configure(w.visible.Load, w.hide)

	if w.gamescope {
		w.steamBlocker = gamepad.NewSteamInputBlocker()
	}

	// Register embedded Inter font before loading CSS so font-family resolves.
	fonts.Register()

	// Load CSS (layout + theme).
	w.loadCSS()

	// Build content and let the backend wrap it if needed.
	w.syncing = true
	w.win.SetChild(w.backend.WrapContent(w.buildContent()))
	w.syncing = false

	go w.subscribeLoop()

	// Gamepad input (disabled with VOLTAIRE_GUI_NO_GAMEPAD=1; the pre-rename
	// Z13GUI_NO_GAMEPAD is honoured through 2.x).
	if startup.GUIEnv("NO_GAMEPAD") == "" {
		w.gamepadReader = gamepad.New(
			w.handleGamepadAction,
			w.anyVisible,
			func(f func()) { glib.IdleAdd(f) },
		)
		go w.gamepadReader.Run()
	}

	// Hide gamepad focus indicator on mouse movement.
	// Hide the gamepad focus indicator when the user reaches for the mouse.
	//
	// The same-position guard is load-bearing: GTK synthesizes a motion event
	// at the *current* pointer position whenever the widget under it changes,
	// and gamepad navigation changes the layout on every press (the anchored
	// hint appears, ensureVisible scrolls). Without the guard each press was
	// immediately followed by a synthetic motion that cleared gamepad mode, so
	// the next press started over at the first item and focus could never move
	// past it — D-pad navigation stopped working entirely. Real pointer motion
	// always carries new coordinates.
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
		// The hint prune backstop moved onto each popup layer's own motion
		// controller (newPopupLayer), where the coordinates and the anchor
		// share a widget tree on every surface.
	})
	w.gtkWin.AddController(motion)

	// Block arrow keys from reaching child widgets. GTK4 uses arrow keys
	// to navigate radio groups (auto-activating them) and adjust scales.
	// The overlay uses mouse/touch/gamepad — not keyboard navigation.
	//
	// Escape-closes-popup lives in this same controller rather than a new
	// one: all three backends attach their own Escape (drawer dismiss)
	// handlers in the bubble phase on this window, and capture reliably
	// precedes bubble, whereas two controllers in the same phase have no
	// ordering contract. Returning false when no popup is open leaves the
	// backends' dismiss behaviour exactly as it was.
	keyBlock := gtk.NewEventControllerKey()
	keyBlock.SetPropagationPhase(gtk.PhaseCapture)
	keyBlock.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		switch keyval {
		case gdk.KEY_Up, gdk.KEY_Down, gdk.KEY_Left, gdk.KEY_Right:
			return true
		case gdk.KEY_Escape:
			if w.popupOpen() {
				w.closePopup()
				return true
			}
		}
		return false
	})
	w.gtkWin.AddController(keyBlock)

	if startup.GUIEnv("DUMP_FOCUS") != "" {
		w.dumpAllFocusLists()
	}
	// VOLTAIRE_GUI_OPEN_FULL=1 opens the full window at startup, without the
	// hardware double press. It is the same kind of instrument as DUMP_FOCUS
	// and exists for the same reason: the full window is otherwise reachable
	// only by pressing a key on one laptop, so nothing about it could be
	// checked while developing it. A tab id as the value ("profiles") opens
	// on that page — a page past the first is otherwise reachable only with
	// a pointer, which a screenshot run does not have.
	//
	// "color" opens the HSL picker over the window's dashboard. It is not a
	// tab — it is a popup two pointer clicks in from the RGB card — so it is
	// the thing a screenshot run is least able to reach.
	if openFull := startup.GUIEnv("OPEN_FULL"); openFull != "" {
		glib.IdleAdd(func() bool {
			w.openFull()
			m := w.mainWin
			if m == nil {
				return false
			}
			if _, ok := mainwin.Lookup(openFull); ok {
				m.setTab(openFull)
				return false
			}
			if openFull == "color" && m.dashboard != nil && len(m.dashboard.lightings) > 0 {
				w.openColorPopup(m.dashboard.lightings[0].color1)
			}
			return false
		})
	}

	slog.Info("drawer initialized")
	return w
}

// dumpAllFocusLists eagerly builds every lazy view and logs its focus grid.
//
// It exists because the focus lists are the one part of the drawer with a
// checkable fingerprint and no test that can reach them: internal/gui needs
// GTK4 headers, so `make test` cannot compile it, and every view but "main" is
// built on first navigation — which needs a person to tap a button. That left
// the custom, theme and colour lists verified only by the parity tests in
// internal/focusgrid, with no evidence the widgets they describe are the
// widgets actually built. This closes that: `VOLTAIRE_GUI_DUMP_FOCUS=1 -d`
// prints all five, so a refactor can be diffed against a baseline rather than
// reasoned about.
//
// It calls the build halves of the show*View functions, not the functions
// themselves — showCustomView resolves an edit target and starts the telemetry
// poll, neither of which a dump should do. Views already built are left alone,
// so this is idempotent and only ever runs before the drawer is shown.
func (w *Window) dumpAllFocusLists() {
	if w.viewStack == nil {
		return
	}
	if w.themeView == nil {
		w.viewStack.AddNamed(w.buildThemeView(), "theme")
		w.buildThemeFocusList()
	}
	// The HSL picker is a popup body rather than a view, so it has no surface
	// to be built into and one instance serves both — but its grid is still the
	// one a person reaches only by clicking Custom, which is precisely what this
	// dump exists to cover. Building it logs its list.
	w.ensureColorPopup()
	w.viewStack.SetVisibleChildName("main")

	// The full window is a second surface hosting its own instances of two of
	// those views, so the fingerprint has to cover it too — a change that left
	// the drawer's grids untouched and broke the window's would otherwise pass
	// the diff. Its pages are named full:<tab> in the log, since the view names
	// alone would collide with the drawer's.
	if w.mainWin == nil {
		w.mainWin = newMainWindow(w)
	}
}

// Toggle shows or hides the drawer. Must be called from the GTK main thread.
func (w *Window) Toggle() {
	slog.Debug("toggle entered", "visible", w.visible.Load(), "full", w.fullVisible.Load())
	// The full window wins, and dismissing it is all this press does.
	//
	// Without this the button reads w.visible — which openFull set to false on
	// its way to showing the window — and so *opens the quickbar behind a window
	// the user was looking at*, leaving the only way out the title bar's close
	// button. The press has to mean the same thing on both surfaces: put away
	// what is in front of me. It deliberately does not fall through to showing
	// the drawer, matching Escape, which the window has always handled this way.
	//
	// Gamescope reaches none of this: openFull there shows the drawer's own
	// dashboard, so fullVisible stays false and the ordinary drawer toggle below
	// is already the right behaviour.
	if w.fullVisible.Load() && w.mainWin != nil {
		slog.Info("toggle", "action", "hide full window")
		w.mainWin.hide()
		return
	}
	if w.visible.Load() {
		slog.Info("toggle", "action", "hide")
		w.hide()
		return
	}
	// Nothing is up, so this press opens the primary surface — which the user
	// chooses (internal/buttonpref). Everything above is unconditional: putting
	// away what is in front means the same thing whichever surface that is.
	if w.press == buttonpref.Window {
		slog.Info("toggle", "action", "show full window")
		w.openFull()
		return
	}
	slog.Info("toggle", "action", "show")
	w.openQuickbar()
}

// openQuickbar shows the drawer and fetches state into it.
//
// The fetch is here rather than in show() because show() is also the animation
// entry point the backends and the full window's hosted path call; this is the
// "a press asked for the quickbar" path, and it is the one that owes a refresh.
func (w *Window) openQuickbar() {
	w.show()
	fetchStart := time.Now()
	go func() {
		ok, state, rawErr := api.SendGetState()
		slog.Debug("SendGetState returned", "ok", ok, "err", rawErr, "elapsed", time.Since(fetchStart))
		// A missing daemon arrives as ok=false with a nil error, so testing err
		// alone opened the drawer on stale defaults with nothing to say. That is
		// the worst moment to stay quiet: every control is about to lie.
		if err := apiresult.Err(ok, rawErr); err != nil {
			w.reportError("Read daemon state", err)
			return
		}
		if state == nil {
			return
		}
		glib.IdleAdd(func() {
			slog.Debug("syncState dispatched", "totalElapsed", time.Since(fetchStart))
			w.state = state
			w.syncState()
		})
	}()
}

// pressDouble is the consumer for api.EventGUIOpenFull: the daemon saw a second
// press inside the double-press window. It opens whichever surface a *single*
// press does not — so the event's name describes the historical default rather
// than what it now means, and it stays that name because it is a published
// protocol string every existing subscriber keys on.
func (w *Window) pressDouble() {
	if w.press == buttonpref.Window {
		w.openQuickbarOverFull()
		return
	}
	w.openFull()
}

// openQuickbarOverFull is the escalation when a single press opens the full
// window: the second press replaces it with the quickbar.
//
// It does not assume the single press's Toggle already ran, even though it
// always has — both handlers fire for the same press, in order, so by the time
// this runs the window is usually already down. Written to be idempotent
// instead, because the alternative is a behaviour that depends on the *other*
// handler's internals staying what they are.
func (w *Window) openQuickbarOverFull() {
	if w.mainWin != nil && w.fullVisible.Load() {
		w.mainWin.hide()
	}
	if !w.visible.Load() {
		w.openQuickbar()
	}
}

// setButtonPress records a new button preference and persists it.
func (w *Window) setButtonPress(s buttonpref.Surface) {
	if s == w.press {
		return
	}
	w.press = s
	theme.UpdateAppConfig(func(cfg *theme.AppConfig) { cfg.ButtonPress = string(s) })
	slog.Info("button preference changed", "single_press_opens", s)
}

// show delegates to the display backend.
func (w *Window) show() {
	slog.Debug("show called")
	w.visible.Store(true)
	w.grabGen++
	if w.gamepadReader != nil {
		gen := w.grabGen
		go w.gamepadReader.SetGrabbed(gen, true)
	}
	// BlockSteam walks /proc twice (every comm, then every status) to find Steam
	// and its children, which is far too much synchronous I/O to do before the
	// animation starts. It only has to take effect before the user's first input,
	// so it runs alongside the slide instead of in front of it.
	if w.steamBlocker != nil {
		blocker := w.steamBlocker
		go func() {
			pid := blocker.BlockSteam()
			glib.IdleAdd(func() {
				// A hide may have landed while /proc was being walked. Undo
				// immediately rather than recording a PID nothing will release.
				if !w.visible.Load() {
					blocker.UnblockSteam(pid)
					return
				}
				w.steamPID = pid
			})
		}()
	}
	w.backend.Show()
	w.startTelemetryPolling()
}

// hide delegates to the display backend. Resets to main view so the drawer
// always opens to the home screen.
func (w *Window) hide() {
	slog.Debug("hide called", "wasVisible", w.visible.Load())
	w.visible.Store(false)
	w.grabGen++
	gen := w.grabGen
	// Delay unblock + ungrab so the dismiss button release is consumed before
	// Steam resumes input processing. gen makes that delay safe: a show landing
	// inside the 200ms window supersedes this release, which would otherwise hand
	// the game the D-pad presses navigating the re-opened drawer.
	// Gated on the blocker, not on steamPID: BlockSteam runs off-thread, so a hide
	// arriving before it lands would otherwise see steamPID == 0, take the
	// immediate path, and skip the delay that lets the dismiss button's release be
	// consumed first. UnblockSteam(0) is already a no-op, and show's own goroutine
	// releases a block that completes after the drawer has closed.
	if w.steamBlocker != nil {
		pid := w.steamPID
		w.steamPID = 0
		go func() {
			time.Sleep(200 * time.Millisecond)
			w.steamBlocker.UnblockSteam(pid)
			if w.gamepadReader != nil {
				w.gamepadReader.SetGrabbed(gen, false)
			}
		}()
	} else if w.gamepadReader != nil {
		go w.gamepadReader.SetGrabbed(gen, false)
	}
	w.hideGamepadFocus()
	// Close any popup before the view reset. In gamescope a backdrop tap with
	// a popup open dismisses the whole drawer through this path, and leaving
	// the popup "open" would greet the next show with a stale list over the
	// main view.
	w.closePopup()
	w.clearError()   // don't greet the next open with a stale failure
	w.telemetryGen++ // stop any running telemetry poll
	if w.viewStack != nil {
		w.viewStack.SetVisibleChildName("main")
		w.swapFocusList(w.mainFocusItems)
	}
	w.backend.Hide()
}

// handleGamepadAction processes a gamepad action on the GTK main thread.
// Navigation: D-pad moves between items. A activates buttons/switches or
// enters edit mode for sliders. In edit mode, left/right adjusts the value,
// A commits, B cancels.
func (w *Window) handleGamepadAction(action gamepad.Action) {
	if !w.gamepadActive {
		w.showGamepadFocus()
	}
	switch action {
	case gamepad.ActionUp:
		if w.focusEditing {
			w.exitEditMode(true)
		}
		w.moveVertical(-1)
	case gamepad.ActionDown:
		if w.focusEditing {
			w.exitEditMode(true)
		}
		w.moveVertical(1)
	case gamepad.ActionLeft:
		if w.focusEditing {
			w.adjustFocus(-1)
		} else {
			w.moveHorizontal(-1)
		}
	case gamepad.ActionRight:
		if w.focusEditing {
			w.adjustFocus(1)
		} else {
			w.moveHorizontal(1)
		}
	case gamepad.ActionAccept:
		if w.focusEditing {
			w.exitEditMode(true)
		} else {
			w.activateOrEdit()
		}
	case gamepad.ActionBack:
		switch {
		case w.focusEditing:
			w.exitEditMode(false)
		// Above the window and view-stack cases: B must close the popup, not
		// the surface underneath it — closing the surface first would strand
		// the popup's focus frame over a vanished list.
		case w.popupOpen():
			w.closePopup()
		// Above the drawer cases: while the full window is up, the drawer is
		// hidden, so falling through to w.hide() dismissed nothing — B could
		// not close the window at all. Same order as Escape and Toggle.
		case w.fullVisible.Load():
			if w.mainWin != nil {
				w.mainWin.hide()
			}
		case w.viewStack != nil && w.viewStack.VisibleChildName() != "main":
			w.showMainView()
		default:
			w.hide()
		}
	case gamepad.ActionBumpL:
		if w.focusEditing {
			w.exitEditMode(true)
		}
		// The bumpers switch tabs while the full window is up — the
		// console-universal gesture — and jump sections in the drawer. The
		// window's pages keep D-pad navigation for their few sections.
		if w.fullVisible.Load() && w.mainWin != nil {
			w.mainWin.cycleTab(-1)
			return
		}
		w.jumpSection(-1)
	case gamepad.ActionBumpR:
		if w.focusEditing {
			w.exitEditMode(true)
		}
		if w.fullVisible.Load() && w.mainWin != nil {
			w.mainWin.cycleTab(1)
			return
		}
		w.jumpSection(1)
	}
}

// subscribeLoop runs in a background goroutine. It subscribes to daemon events,
// dispatches gui-toggle to the GTK main thread via IdleAdd, and re-reads state
// on power-source and state-changed so the drawer never shows values another
// client (CLI, autoswitch, a resume) has already moved. Reconnects with
// exponential backoff.
func (w *Window) subscribeLoop() {
	backoff := time.Second
	// Debounce state for duplicate gui-toggle bursts. Deliberately a local, not a
	// Window field: every other piece of Window state is main-thread-owned, and
	// keeping this out of the struct makes it unreachable from the main thread.
	// Declared outside the reconnect loop so the window survives a reconnect.
	var lastToggle time.Time
	for {
		ch, cancel, err := api.Subscribe(api.AllEvents)
		if err != nil || ch == nil {
			slog.Info("daemon disconnected, retrying", "backoff", backoff)
			time.Sleep(backoff)
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		slog.Info("daemon connected")
		backoff = time.Second
		for event := range ch {
			switch event {
			case api.EventGUIToggle:
				receivedAt := time.Now()
				lastAccepted, ok := togglegate.Accept(lastToggle, receivedAt, daemonToggleDebounce)
				if !ok {
					slog.Debug("gui-toggle suppressed", "since", receivedAt.Sub(lastToggle))
					continue
				}
				lastToggle = lastAccepted
				slog.Debug("gui-toggle received, dispatching")
				glib.TimeoutAdd(0, func() bool {
					w.Toggle()
					return false
				})
				glib.MainContextDefault().Wakeup()
			case api.EventGUIOpenFull:
				// Additive to the gui-toggle the same press already emitted:
				// the quickbar is up by now, and this replaces it. Never
				// debounced against the toggle — they are one press reported
				// twice on purpose.
				slog.Debug("gui-open-full received, dispatching")
				glib.TimeoutAdd(0, func() bool {
					w.pressDouble()
					return false
				})
				glib.MainContextDefault().Wakeup()
			case api.EventPowerSource, api.EventStateChanged:
				// Events carry no payload by design; get-state answers with
				// current truth. Called inline rather than on a goroutine so a
				// burst of events refreshes sequentially instead of racing
				// stale snapshots into IdleAdd; refreshState itself marshals
				// every widget write to the main thread.
				slog.Debug("state refresh", "event", event)
				w.refreshState()
			}
		}
		cancel()
	}
}

// loadCSS loads the layout CSS (always) then the user theme or the default theme.
// Priority chain (first match wins):
//  1. ~/.config/voltaire/theme.toml — custom color config (overrides everything)
//  2. ~/.config/voltaire/theme.css  — full CSS override (power users)
//  3. config.toml theme = "id"    — built-in theme selection
//  4. embedded "rog-dark"         — compiled-in default
func (w *Window) loadCSS() {
	gdkDisplay := gdk.DisplayGetDefault()

	layout := gtk.NewCSSProvider()
	layout.LoadFromString(layoutCSS)
	gtk.StyleContextAddProviderForDisplay(gdkDisplay, layout, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)

	w.themeProvider = gtk.NewCSSProvider()
	base := theme.XDGConfigHome()
	tomlPath := filepath.Join(base, "voltaire", "theme.toml")
	cssPath := filepath.Join(base, "voltaire", "theme.css")

	var loaded bool
	switch {
	case fileExists(tomlPath):
		data, err := os.ReadFile(tomlPath)
		if err != nil {
			slog.Warn("failed to read custom theme TOML, using default", "path", tomlPath, "err", err)
		} else {
			colors, accents := theme.ParseThemeTOMLFull(data)
			w.isCustomTheme = true
			w.customColors = colors
			w.customAccents = accents
			// Restore the user's last accent selection from config.toml.
			if len(accents) > 0 {
				cfg := theme.LoadAppConfig()
				if cfg.Accent != "" {
					for _, a := range accents {
						if a.ID == cfg.Accent {
							colors.Accent = a.Hex
							break
						}
					}
				}
			}
			w.colors = colors
			w.themeProvider.LoadFromString(theme.BuildThemeCSS(colors, defaultThemeCSS))
			slog.Info("theme loaded", "source", "custom-toml", "path", tomlPath)
			loaded = true
		}
	case fileExists(cssPath):
		data, err := os.ReadFile(cssPath)
		if err != nil {
			slog.Warn("failed to read custom theme CSS, using default", "path", cssPath, "err", err)
		} else {
			// theme.css is loaded verbatim, so a token it references without
			// defining is simply undefined and GTK drops every rule using it —
			// silently, as a styling gap rather than an error. Name the tokens
			// instead of leaving the user to guess why part of the drawer is
			// unstyled. Not fatal: the rest of the sheet still applies.
			if missing := theme.UndefinedColorTokens(string(data)); len(missing) > 0 {
				slog.Warn("custom theme CSS references colors it does not define; "+
					"rules using them will be ignored — add @define-color lines or "+
					"start from `voltaire-gui --print-theme`",
					"path", cssPath, "undefined", missing)
			}
			w.themeProvider.LoadFromString(string(data))
			slog.Info("theme loaded", "source", "custom-css", "path", cssPath)
			loaded = true
		}
	}
	if !loaded {
		cfg := theme.LoadAppConfig()
		colors, ok := theme.BuiltinByID(cfg.Theme)
		if !ok {
			colors = theme.DefaultColors
		}
		if cfg.Accent != "" {
			if hex, ok := theme.BuiltinAccentHex(cfg.Theme, cfg.Accent); ok {
				colors.Accent = hex
			}
		}
		w.colors = colors
		w.themeProvider.LoadFromString(theme.BuildThemeCSS(colors, defaultThemeCSS))
		slog.Info("theme loaded", "source", "builtin", "theme", cfg.Theme, "accent", cfg.Accent)
	}

	gtk.StyleContextAddProviderForDisplay(gdkDisplay, w.themeProvider, gtk.STYLE_PROVIDER_PRIORITY_USER)
}

// applyTheme hot-swaps the theme CSS provider and persists the selection to
// config.toml. accentID may be "" to use the theme's default accent.
// Must be called from the GTK main thread.
func (w *Window) applyTheme(id, accentID string) {
	gdkDisplay := gdk.DisplayGetDefault()
	if w.themeProvider != nil {
		gtk.StyleContextRemoveProviderForDisplay(gdkDisplay, w.themeProvider)
	}
	w.themeProvider = gtk.NewCSSProvider()
	colors, ok := theme.BuiltinByID(id)
	if !ok {
		colors = theme.DefaultColors
		id = "rog-dark"
	}
	if accentID != "" {
		if hex, ok := theme.BuiltinAccentHex(id, accentID); ok {
			colors.Accent = hex
		}
	}
	w.colors = colors
	w.themeProvider.LoadFromString(theme.BuildThemeCSS(colors, defaultThemeCSS))
	gtk.StyleContextAddProviderForDisplay(gdkDisplay, w.themeProvider, gtk.STYLE_PROVIDER_PRIORITY_USER)
	// Read-modify-write, never a fresh value: SaveAppConfig writes the whole
	// file, so constructing one here would discard every preference this
	// function does not happen to know about.
	theme.UpdateAppConfig(func(cfg *theme.AppConfig) {
		cfg.Theme, cfg.Accent = id, accentID
	})
	w.redrawFanCurve()
	slog.Info("theme changed", "id", id, "accent", accentID)
}

// applyCustomAccent hot-swaps the accent color for a custom theme.toml theme.
// The accentID must match an entry in w.customAccents. The selection is saved
// to config.toml so it persists across restarts.
func (w *Window) applyCustomAccent(accentID string) {
	colors := w.customColors
	for _, a := range w.customAccents {
		if a.ID == accentID {
			colors.Accent = a.Hex
			break
		}
	}
	gdkDisplay := gdk.DisplayGetDefault()
	if w.themeProvider != nil {
		gtk.StyleContextRemoveProviderForDisplay(gdkDisplay, w.themeProvider)
	}
	w.themeProvider = gtk.NewCSSProvider()
	w.colors = colors
	w.themeProvider.LoadFromString(theme.BuildThemeCSS(colors, defaultThemeCSS))
	gtk.StyleContextAddProviderForDisplay(gdkDisplay, w.themeProvider, gtk.STYLE_PROVIDER_PRIORITY_USER)
	// Change only the accent. Saving AppConfig{Accent: …} wrote an empty theme
	// key, discarding the user's built-in theme choice — invisible while
	// theme.toml exists, since it wins on load, and a silent reset to rog-dark the
	// moment they remove it. UpdateAppConfig is that lesson made structural.
	theme.UpdateAppConfig(func(cfg *theme.AppConfig) { cfg.Accent = accentID })
	w.redrawFanCurve()
}

// fileExists returns true if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DefaultThemeTOML returns the embedded default theme.toml content.
// Used by --print-theme to let users bootstrap a custom theme.
func DefaultThemeTOML() string {
	return defaultThemeTOML
}
