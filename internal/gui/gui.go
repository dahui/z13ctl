// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package gui implements the GTK4 overlay drawer for z13gui.
// It provides the main Window type that handles daemon state synchronization,
// GTK widget construction, and theming. Display-mode-specific concerns
// (layer-shell, the gamescope X11 overlay, or the fullscreen click-through
// overlay used where layer-shell is unavailable) are delegated to Backend
// implementations.
package gui

import (
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13gui/internal/daemon"
	"github.com/dahui/z13gui/internal/gui/fonts"
	"github.com/dahui/z13gui/internal/gui/gamepad"
	"github.com/dahui/z13gui/internal/gui/gamescope"
	"github.com/dahui/z13gui/internal/gui/layershell"
	"github.com/dahui/z13gui/internal/gui/overlay"
	"github.com/dahui/z13gui/internal/power"
	"github.com/dahui/z13gui/internal/theme"
	"github.com/dahui/z13gui/internal/togglegate"
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
	tab       string      // active device tab: "keyboard" or "lightbar"

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

	swatchProvider *gtk.CSSProvider // dynamic swatch background colors
	themeProvider  *gtk.CSSProvider // current theme; replaced on applyTheme()

	// colors is the active palette, kept alongside the CSS built from it because
	// the fan curve chart is painted with Cairo rather than styled by CSS and so
	// cannot read the @z13-* tokens. Without this the chart was the one part of
	// the drawer the theme did not reach.
	colors theme.Colors

	errBar        *gtk.Box    // error surface; hidden unless an operation failed
	errLabel      *gtk.Label  // message shown in errBar
	errDismissBtn *gtk.Button // dismiss button; navigable in every view's focus grid

	// limits is the device's power/thermal envelope, driving every TDP and fan
	// curve bound in the custom view. Defaulted to the Z13's values; when z13ctl
	// grows an API for serving per-device limits this is the one place that
	// changes — fetch once at startup, Sanitized, falling back to the defaults.
	limits power.Limits

	// Widget references for syncState.
	tabKB           *gtk.CheckButton
	tabLB           *gtk.CheckButton
	modeButtons     map[string]*gtk.Button
	color1          *colorInput
	color2          *colorInput
	color1Box       *gtk.Box // COLOR 1 label + row — visibility toggled by syncModeVis
	color2Box       *gtk.Box // COLOR 2 label + row — visibility toggled by syncModeVis
	speedBox        *gtk.Box // SPEED label + row — visibility toggled by syncModeVis
	brightBox       *gtk.Box // BRIGHTNESS label + scale — hidden when mode is "off"
	speedBtns       map[string]*gtk.Button
	brightScale     *gtk.Scale
	profileBtns     map[string]*gtk.Button
	battScale       *gtk.Scale
	overdriveSwitch *gtk.Switch
	bootSoundSwitch *gtk.Switch

	// Custom profile view.
	customScroll       *gtk.ScrolledWindow
	customBackBtn      *gtk.Button
	tdpBasicScale      *gtk.Scale
	tdpBasicLabel      *gtk.Label
	tdpAdvancedCheck   *gtk.CheckButton
	tdpAdvancedBox     *gtk.Box
	tdpPL1Scale        *gtk.Scale
	tdpPL2Scale        *gtk.Scale
	tdpPL3Scale        *gtk.Scale
	tdpPL1Label        *gtk.Label
	tdpPL2Label        *gtk.Label
	tdpPL3Label        *gtk.Label
	tdpWarningLabel    *gtk.Label
	fanCurve           *fanCurveEditor
	saveTdpBtn         *gtk.Button
	saveFanBtn         *gtk.Button
	saveBothBtn        *gtk.Button
	resetTdpBtn        *gtk.Button
	resetFanBtn        *gtk.Button
	uvBox              *gtk.Box // undervolt container, hidden when unavailable
	uvCpuScale         *gtk.Scale
	uvCpuLabel         *gtk.Label
	saveUvBtn          *gtk.Button
	resetUvBtn         *gtk.Button
	headerTelemetry    *gtk.Label // "45°C · 3200 RPM" in main header
	telemetryTempLabel *gtk.Label
	telemetryFanLabel  *gtk.Label
	telemetryGen       int
	telemetryBusy      bool // a poll request is in flight; skip ticks until it lands
	customFocusItems   []focusItem

	syncing    bool        // true while syncState is updating widgets; suppresses sendApply
	applyTimer *time.Timer // debounce for continuous inputs (brightness, color wheel)

	// View switching (main/theme/color views).
	mainScroll         *gtk.ScrolledWindow // scrollable area in main drawer view
	themeScroll        *gtk.ScrolledWindow // scrollable area in theme picker view
	viewStack          *gtk.Stack          // switches between main/theme/color views
	editingColor       *colorInput         // which color the color-picker view is editing
	colorViewTitle     *gtk.Label          // "COLOR 1" or "COLOR 2" in color view header
	colorHue           *gtk.Scale          // H: 0-360
	colorSat           *gtk.Scale          // S: 0-100
	colorLit           *gtk.Scale          // L: 0-100
	colorPreview       *gtk.Box            // swatch preview in color view
	colorHexLabel      *gtk.Label          // hex display in color view
	colorSwatchProv    *gtk.CSSProvider    // color picker preview swatch CSS
	paletteBtn         *gtk.Button         // theme button in bottom bar
	themeBackBtn       *gtk.Button         // back button in theme view
	colorBackBtn       *gtk.Button         // back button in color picker view
	colorPickerPresets []*gtk.Button       // preset buttons in color picker view
	themeRadios        []*gtk.CheckButton  // collected during appendThemeChoices
	themeDots          [][]*gtk.Button     // accent dot buttons per theme

	// Custom theme state (set when theme.toml exists).
	isCustomTheme bool
	customColors  theme.Colors
	customAccents []theme.Accent

	// Steam input suppression (gamescope only).
	steamBlocker gamepad.SteamInputBlocker
	steamPID     int

	// Gamepad focus navigation.
	gamepadReader     *gamepad.Reader
	focusItems        []focusItem // active view's navigable widgets (points to one of the lists below)
	focusIdx          int         // current position in focusItems
	gamepadActive     bool        // true when gamepad focus indicator is shown
	focusEditing      bool        // true when a slider is in edit mode
	editOriginalValue float64     // saved value for cancel on B
	mainFocusItems    []focusItem // focus grid for main drawer view
	themeFocusItems   []focusItem // focus grid for theme picker view
	colorFocusItems   []focusItem // focus grid for HSL color picker view
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
	display := gdk.DisplayGetDefault()
	if display == nil {
		slog.Warn("no GDK display available; assuming layer-shell is unusable")
		return false
	}
	// e.g. "GdkWaylandDisplay", "GdkX11Display".
	if backend := display.TypeFromInstance().Name(); !strings.Contains(backend, "Wayland") {
		slog.Debug("GDK is not using the Wayland backend, so layer-shell cannot apply",
			"gdkDisplay", backend)
		return false
	}
	return gtk4layershell.IsSupported()
}

// New creates the overlay window and attaches it to app. Called from the
// GTK Activate signal.
func New(app *gtk.Application) *Window {
	w := &Window{
		tab:         "keyboard",
		limits:      power.DefaultLimits(),
		colors:      theme.DefaultColors,
		gamescope:   os.Getenv("GAMESCOPE_WAYLAND_DISPLAY") != "",
		modeButtons: make(map[string]*gtk.Button),
		speedBtns:   make(map[string]*gtk.Button),
		profileBtns: make(map[string]*gtk.Button),
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
		w.backend = gamescope.New(w.win, w.gtkWin, drawerWidth)
	case layerShellUsable():
		w.backend = layershell.New(w.win, w.gtkWin, drawerWidth)
	default:
		slog.Info("layer-shell unavailable, using the overlay backend "+
			"(the drawer is drawn in a transparent click-through window instead of "+
			"anchored to the screen edge)",
			"desktop", os.Getenv("XDG_CURRENT_DESKTOP"), "session", os.Getenv("XDG_SESSION_TYPE"))
		w.backend = overlay.New(w.win, w.gtkWin, drawerWidth)
	}

	w.backend.Configure(w.visible.Load, w.hide)

	if w.gamescope {
		w.steamBlocker = gamepad.NewSteamInputBlocker()
	}

	// Register embedded Inter font before loading CSS so font-family resolves.
	fonts.Register()

	// Load CSS (layout + theme).
	w.loadCSS()

	// Swatch provider: separate from theme so we can update it per-color at runtime.
	w.swatchProvider = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), w.swatchProvider, gtk.STYLE_PROVIDER_PRIORITY_USER+10)

	// Build content and let the backend wrap it if needed.
	w.syncing = true
	w.win.SetChild(w.backend.WrapContent(w.buildContent()))
	w.syncing = false

	go w.subscribeLoop()

	// Gamepad input (disabled with Z13GUI_NO_GAMEPAD=1).
	if os.Getenv("Z13GUI_NO_GAMEPAD") == "" {
		w.gamepadReader = gamepad.New(
			w.handleGamepadAction,
			w.visible.Load,
			func(f func()) { glib.IdleAdd(f) },
		)
		go w.gamepadReader.Run()
	}

	// Hide gamepad focus indicator on mouse movement.
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(_, _ float64) {
		if w.gamepadActive {
			w.hideGamepadFocus()
		}
	})
	w.gtkWin.AddController(motion)

	// Block arrow keys from reaching child widgets. GTK4 uses arrow keys
	// to navigate radio groups (auto-activating them) and adjust scales.
	// The overlay uses mouse/touch/gamepad — not keyboard navigation.
	keyBlock := gtk.NewEventControllerKey()
	keyBlock.SetPropagationPhase(gtk.PhaseCapture)
	keyBlock.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		switch keyval {
		case gdk.KEY_Up, gdk.KEY_Down, gdk.KEY_Left, gdk.KEY_Right:
			return true
		}
		return false
	})
	w.gtkWin.AddController(keyBlock)

	slog.Info("drawer initialized")
	return w
}

// Toggle shows or hides the drawer. Must be called from the GTK main thread.
func (w *Window) Toggle() {
	slog.Debug("toggle entered", "visible", w.visible.Load())
	if w.visible.Load() {
		slog.Info("toggle", "action", "hide")
		w.hide()
	} else {
		slog.Info("toggle", "action", "show")
		w.show()
		fetchStart := time.Now()
		go func() {
			ok, state, rawErr := api.SendGetState()
			slog.Debug("SendGetState returned", "ok", ok, "err", rawErr, "elapsed", time.Since(fetchStart))
			// A missing daemon arrives as ok=false with a nil error, so testing err
			// alone opened the drawer on stale defaults with nothing to say. That is
			// the worst moment to stay quiet: every control is about to lie.
			if err := daemon.Err(ok, rawErr); err != nil {
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
		case w.viewStack != nil && w.viewStack.VisibleChildName() != "main":
			w.showMainView()
		default:
			w.hide()
		}
	case gamepad.ActionBumpL:
		if w.focusEditing {
			w.exitEditMode(true)
		}
		w.jumpSection(-1)
	case gamepad.ActionBumpR:
		if w.focusEditing {
			w.exitEditMode(true)
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
//  1. ~/.config/z13gui/theme.toml — custom color config (overrides everything)
//  2. ~/.config/z13gui/theme.css  — full CSS override (power users)
//  3. config.toml theme = "id"    — built-in theme selection
//  4. embedded "rog-dark"         — compiled-in default
func (w *Window) loadCSS() {
	display := gdk.DisplayGetDefault()

	layout := gtk.NewCSSProvider()
	layout.LoadFromString(layoutCSS)
	gtk.StyleContextAddProviderForDisplay(display, layout, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)

	w.themeProvider = gtk.NewCSSProvider()
	base := theme.XDGConfigHome()
	tomlPath := filepath.Join(base, "z13gui", "theme.toml")
	cssPath := filepath.Join(base, "z13gui", "theme.css")

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
					"start from `z13gui --print-theme`",
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

	gtk.StyleContextAddProviderForDisplay(display, w.themeProvider, gtk.STYLE_PROVIDER_PRIORITY_USER)
}

// applyTheme hot-swaps the theme CSS provider and persists the selection to
// config.toml. accentID may be "" to use the theme's default accent.
// Must be called from the GTK main thread.
func (w *Window) applyTheme(id, accentID string) {
	display := gdk.DisplayGetDefault()
	if w.themeProvider != nil {
		gtk.StyleContextRemoveProviderForDisplay(display, w.themeProvider)
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
	gtk.StyleContextAddProviderForDisplay(display, w.themeProvider, gtk.STYLE_PROVIDER_PRIORITY_USER)
	theme.SaveAppConfig(theme.AppConfig{Theme: id, Accent: accentID})
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
	display := gdk.DisplayGetDefault()
	if w.themeProvider != nil {
		gtk.StyleContextRemoveProviderForDisplay(display, w.themeProvider)
	}
	w.themeProvider = gtk.NewCSSProvider()
	w.colors = colors
	w.themeProvider.LoadFromString(theme.BuildThemeCSS(colors, defaultThemeCSS))
	gtk.StyleContextAddProviderForDisplay(display, w.themeProvider, gtk.STYLE_PROVIDER_PRIORITY_USER)
	// Change only the accent. Saving AppConfig{Accent: …} wrote an empty theme
	// key, discarding the user's built-in theme choice — invisible while
	// theme.toml exists, since it wins on load, and a silent reset to rog-dark the
	// moment they remove it.
	cfg := theme.LoadAppConfig()
	cfg.Accent = accentID
	theme.SaveAppConfig(cfg)
	w.redrawFanCurve()
}

// redrawFanCurve repaints the fan curve chart after a theme change. It is drawn
// with Cairo from w.colors rather than styled by CSS, so swapping the CSS
// provider alone leaves it in the previous theme's colours.
func (w *Window) redrawFanCurve() {
	if w.fanCurve != nil {
		w.fanCurve.area.QueueDraw()
	}
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
