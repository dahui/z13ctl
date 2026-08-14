// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// controls.go — builds the entire drawer widget tree, theme picker view,
// and HSL color picker view. All views live in a gtk.Stack (both KDE and
// gamescope modes) for consistent gamepad navigation.

import (
	"log/slog"
	"strings"

	"github.com/dahui/voltaire/v2/internal/controls"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// addTouchActivate works around a GTK4 X11 issue where CheckButton and Switch
// widgets don't receive touch events properly in gamescope/XWayland (their
// internal GestureClick uses BUBBLE phase, which fails for touch). Adding a
// CAPTURE-phase, touch-only gesture ensures touch taps activate these widgets.
// Mouse input is unaffected (SetTouchOnly).
func addTouchActivate(widget gtk.Widgetter, onTap func()) {
	gesture := gtk.NewGestureClick()
	gesture.SetTouchOnly(true)
	gesture.SetPropagationPhase(gtk.PhaseCapture)
	gesture.ConnectReleased(func(_ int, _, _ float64) {
		onTap()
	})
	gtk.BaseWidget(widget).AddController(gesture)
}

// scrollMinContentHeight is the floor every view's scroller reports. It matches
// .fan-curve-area's min-height so the custom view's chart is never the thing
// being crushed when the drawer is short.
const scrollMinContentHeight = 240

// newDrawerScroll returns the vertical scroller every view uses.
//
// SetMinContentHeight is the load-bearing call. A GtkScrolledWindow reports a
// minimum height of 0, and SetVExpand only distributes surplus height rather
// than requesting any, so with nothing imposing a height from outside the
// toplevel's natural height collapses to the title row plus the bottom bar —
// which is the ~320px box in issue #16. Layer-shell hid that for two years by
// anchoring top and bottom, and gamescope hid it by going fullscreen; neither
// is a height the widget tree ever asked for.
//
// Deliberately not SetPropagateNaturalHeight(true): that would make the custom
// view (eight fan-curve points, three TDP sliders, telemetry) request a natural
// height taller than the screen, trading no default size for a bad one.
//
// The three views shared five identical lines before this existed, which is the
// shape of bug where a fix lands on two call sites out of three.
func newDrawerScroll(child gtk.Widgetter) *gtk.ScrolledWindow {
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	scroll.SetMinContentHeight(scrollMinContentHeight)
	scroll.SetChild(child)
	return scroll
}

// controlBuilder is the two halves of one control: the widgets it appends to
// the scrolling box, and the focus items it contributes to the gamepad grid.
//
// They live in one struct, in one map, because they used to be two separate
// literal sequences — an Append run in buildContent and a matching run in
// buildMainFocusList — that nothing forced to agree. A control added to one and
// not the other is either invisible or unreachable by controller, and the
// second is the failure nobody notices with a mouse in their hand.
type controlBuilder struct {
	build func(inner *gtk.Box)
	focus func(b *focusgrid.Builder, items *[]focusItem)
}

// controlBuilders maps a registry ID to the code that builds it. This is the
// only place internal/gui knows what a control *is*; internal/controls decides
// whether and where it appears.
func (w *Window) controlBuilders() map[string]controlBuilder {
	return map[string]controlBuilder{
		"profile": {
			build: func(inner *gtk.Box) { inner.Append(w.buildProfileSection()) },
			focus: w.focusProfileSection,
		},
		"autoswitch": {
			build: func(inner *gtk.Box) { inner.Append(w.buildAutoswitchSection()) },
			focus: w.focusAutoswitchSection,
		},
		"battery": {
			build: func(inner *gtk.Box) { inner.Append(w.buildBatterySection()) },
			focus: w.focusBatterySection,
		},
		"lighting": {
			build: w.buildLightingSection,
			focus: w.focusLightingSection,
		},
	}
}

// buildContent builds the scrolled content box and returns it as the window child.
// Content, theme view, and color picker view are in a gtk.Stack so views can be
// swapped for gamepad navigation (and in gamescope where popovers don't work).
func (w *Window) buildContent() gtk.Widgetter {
	outer := gtk.NewBox(gtk.OrientationVertical, 0)
	outer.AddCSSClass("drawer")

	// Fixed title row — sits above the scroll area, always visible.
	titleRow := gtk.NewBox(gtk.OrientationHorizontal, 0)
	titleRow.SetMarginTop(10)
	titleRow.SetMarginBottom(6)
	titleRow.SetMarginStart(14)
	titleRow.SetMarginEnd(14)

	titleLabel := gtk.NewLabel("Voltaire")
	titleLabel.SetHAlign(gtk.AlignStart)
	titleLabel.AddCSSClass("drawer-title")
	titleRow.Append(titleLabel)

	w.headerTelemetry = gtk.NewLabel("")
	w.headerTelemetry.SetHAlign(gtk.AlignEnd)
	w.headerTelemetry.SetHExpand(true)
	w.headerTelemetry.AddCSSClass("header-telemetry")
	titleRow.Append(w.headerTelemetry)

	inner := gtk.NewBox(gtk.OrientationVertical, 8)
	inner.SetMarginTop(4)
	inner.SetMarginBottom(12)
	inner.SetMarginStart(12)
	inner.SetMarginEnd(12)

	// The sections, their group headings and the separators between them all
	// come from the resolved control list rather than from a literal sequence
	// of Appends. With no gui.toml this produces exactly the sequence that used
	// to be written out here — controls.TestLayoutReproducesTheShippedChrome
	// pins that — and a user who reorders or hides a section gets the headings
	// following their choice instead of stranded above the wrong content.
	builders := w.controlBuilders()
	for _, row := range controls.Layout(w.controls) {
		if row.Separator {
			inner.Append(separator())
		}
		if row.Heading != "" {
			inner.Append(groupLabel(row.Heading))
		}
		cb, ok := builders[row.Control.ID]
		if !ok {
			// The registry names a control this build has no builder for. It
			// cannot happen from a config file (Resolve drops unknown IDs), so
			// it means the two lists have drifted — log rather than panic, and
			// leave the rest of the drawer usable.
			slog.Warn("no builder for control; skipping", "id", row.Control.ID)
			continue
		}
		cb.build(inner)
	}

	// Set initial visibility based on default mode (static). Safe when the
	// lighting section was not built: every widget it touches is nil-guarded.
	w.syncModeVis()

	scroll := newDrawerScroll(inner)
	w.mainScroll = scroll

	// Stack with main, theme, and color views — used in both modes.
	w.viewStack = gtk.NewStack()
	w.viewStack.SetTransitionType(gtk.StackTransitionTypeNone)
	w.viewStack.SetVExpand(true)

	mainPage := gtk.NewBox(gtk.OrientationVertical, 0)
	mainPage.Append(titleRow)
	mainPage.Append(scroll)
	w.viewStack.AddNamed(mainPage, "main")
	// Theme and color views are lazy-loaded on first navigation
	// to keep the initial widget tree small for fast animation.
	w.viewStack.SetVisibleChildName("main")

	// The view stack, error bar and bottom bar go into one content box, which
	// becomes the popup layer's main child: popups (dropdown lists, anchored
	// hints) are overlay children placed over all of it, in panel-relative
	// coordinates that are the same in every backend.
	content := gtk.NewBox(gtk.OrientationVertical, 0)
	content.Append(w.viewStack)

	// Error bar sits outside the stack so a failure raised in any view stays
	// visible, including after a view switch.
	w.errView = newErrorBar(w, w.visible.Load)
	content.Append(w.errView.bar)

	content.Append(w.buildBottomBar())

	outer.Append(w.buildPopupLayer(content))

	w.buildMainFocusList()
	w.focusItems = w.mainFocusItems

	return outer
}

// buildBottomBar returns the fixed bottom bar containing the theme picker button
// and system toggles (panel overdrive, boot sound). It sits below the scroll
// area and is always visible (not scrolled).
func (w *Window) buildBottomBar() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.AddCSSClass("bottom-bar")
	bar.SetMarginTop(4)
	bar.SetMarginBottom(8)
	bar.SetMarginStart(10)

	w.paletteBtn = gtk.NewButton()
	w.paletteBtn.SetIconName("preferences-desktop-color-symbolic")
	w.setHint(w.paletteBtn, "Choose theme")
	w.paletteBtn.ConnectClicked(func() { w.showThemeView() })
	bar.Append(w.paletteBtn)

	// There is deliberately no telemetry button here. The drawer is the quick
	// controls a user wants close at hand; the dashboard is a reading surface
	// that belongs to the full window, and a chart is the one thing 320px
	// cannot show at a size worth looking at. The view is still reachable under
	// gamescope, where there is no second toplevel to put it on — see
	// Window.openFull.

	// Spacer pushes toggles to the right.
	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	bar.Append(spacer)

	bar.Append(w.buildToggle("Panel Overdrive", "Enable panel overdrive for faster pixel response (may cause ghosting)", &w.overdriveSwitch, func(active bool) {
		v := 0
		if active {
			v = 1
		}
		w.sendOverdriveSet(v)
	}))
	bar.Append(w.buildToggle("Boot Sound", "Play startup sound when the laptop powers on", &w.bootSoundSwitch, func(active bool) {
		v := 0
		if active {
			v = 1
		}
		w.sendBootSoundSet(v)
	}))

	return bar
}

// buildToggle creates a compact label + switch pair for the bottom bar.
func (w *Window) buildToggle(label, hint string, sw **gtk.Switch, onChange func(bool)) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 4)
	w.setHint(box, hint)
	lbl := gtk.NewLabel(label)
	lbl.AddCSSClass("toggle-label")
	s := gtk.NewSwitch()
	// Registered on the switch as well as the box: the gamepad focus item is
	// the switch, and the focus path looks hints up by the focused widget.
	w.setHint(s, hint)
	s.ConnectStateSet(func(state bool) bool {
		if !w.syncing {
			onChange(state)
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(s, func() { s.SetActive(!s.Active()) })
	}
	*sw = s
	box.Append(lbl)
	box.Append(s)
	return box
}

// showMainView switches the view stack to the main drawer view.
// Every show*View closes any open popup first: a popup is anchored to a
// widget in the view it was opened from, and a view switch underneath it
// would leave the scrim and list floating over the wrong view.
func (w *Window) showMainView() {
	w.closePopup()
	w.stopDashboardPolling()
	if w.viewStack != nil {
		w.viewStack.SetVisibleChildName("main")
		w.swapFocusList(w.mainFocusItems)
	}
}

// stopDashboardPolling ends the dashboard's history refresh. Every path that
// leaves the dashboard calls it: the tick's own visible-child guard would stop
// it within a second anyway, but that is a second of a few-hundred-sample round
// trip nobody is looking at, and hide() has no view switch to be caught by.
func (w *Window) stopDashboardPolling() {
	if w.dashboard != nil {
		w.dashboard.stopPolling()
	}
}

// setActiveButton removes .active from all buttons in the map and adds it
// to the button matching the given key. Used for button groups that replaced
// radio buttons (profiles, modes, speeds).
func setActiveButton(btns map[string]*gtk.Button, active string) {
	for k, b := range btns {
		if k == active {
			b.AddCSSClass("active")
		} else {
			b.RemoveCSSClass("active")
		}
	}
}

// buildButtonGroup creates a row of regular buttons for the given options,
// stores each in dst[option], and calls onChange when a button is clicked.
func (w *Window) buildButtonGroup(
	orientation gtk.Orientation,
	options []string,
	dst map[string]*gtk.Button,
	onChange func(string),
) *gtk.Box {
	row := gtk.NewBox(orientation, 4)
	row.AddCSSClass("btn-group")
	for _, opt := range options {
		opt := opt
		btn := gtk.NewButtonWithLabel(strings.Title(opt)) //nolint:staticcheck // strings.Title is fine for ASCII-only mode/speed/profile labels
		btn.ConnectClicked(func() {
			setActiveButton(dst, opt)
			onChange(opt)
		})
		dst[opt] = btn
		row.Append(btn)
	}
	return row
}

// buildBatterySection creates the battery charge limit scale (40–100%).
func (w *Window) buildBatterySection() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("BATTERY LIMIT"))

	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, 40, 100, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(true)
	sc.SetValue(80)
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	w.battScale = sc
	w.initBatteryDebounce(sc)

	box.Append(sc)
	return box
}

// colorSubBox wraps a section label + content widget into a single Box,
// making it easy to show/hide the whole subsection at once.
func colorSubBox(label string, content gtk.Widgetter) *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVertical, 4)
	b.Append(sectionLabel(label))
	b.Append(content)
	return b
}

// blockNote creates a hidden label for the refusal reason beneath a control.
//
// Refusal reasons live in these notes, never in tooltips: a tooltip is a
// separate popup surface gamescope never shows, touch has no hover to raise
// one, and the gamepad focus grid skips insensitive widgets — so a tooltip on
// a desensitized control is unreadable in exactly the situations where the
// user is staring at a dead button. The note is in the flow of the view,
// visible whenever the block reason is non-empty, the same pattern as
// editorNote and tdpWarningLabel.
func blockNote() *gtk.Label {
	l := gtk.NewLabel("")
	l.AddCSSClass("block-note")
	l.SetWrap(true)
	l.SetXAlign(0)
	l.SetVisible(false)
	return l
}

// setBlockNote shows reason in the note, or hides the note when reason is "".
func setBlockNote(l *gtk.Label, reason string) {
	if l == nil {
		return
	}
	if reason == "" {
		l.SetVisible(false)
		return
	}
	l.SetText(reason)
	l.SetVisible(true)
}

// sectionLabel creates a small-caps section label (e.g. "MODE", "SPEED").
func sectionLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetHAlign(gtk.AlignStart)
	l.AddCSSClass("section-label")
	return l
}

// groupLabel creates a section group heading (e.g. "TDP AND POWER", "RGB").
func groupLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetHAlign(gtk.AlignStart)
	l.AddCSSClass("section-group")
	return l
}

// separator creates a horizontal separator line.
func separator() *gtk.Separator {
	return gtk.NewSeparator(gtk.OrientationHorizontal)
}

// buildMainFocusList builds the 2D focus grid for the main drawer view.
// Items are arranged by visual row/col matching the drawer layout.
//
// Row numbers are assigned with a running counter rather than literals: the
// profile list holds one row per custom-family profile, so everything below it
// shifts as profiles are created and deleted. syncProfiles rebuilds this list
// whenever the profile row set changes.
func (w *Window) buildMainFocusList() {
	var items []focusItem

	// Coordinates come from focusgrid.Builder, not a hand-incremented row: the
	// declaration order below IS the visual order, and the arithmetic that used
	// to be spread through this function (a grid's height written once in the
	// loop and again in the follow-up assignment) lives in one tested place.
	// internal/focusgrid/builder.go has the two defects that shape invited.
	// A section the caller skips consumes no line, so an absent optional
	// control still leaves everything below it where it was.
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	// Walk the same resolved list buildContent built from, so the gamepad's
	// order is the visual order by construction rather than by two literal
	// sequences that have to be kept in step by hand.
	builders := w.controlBuilders()
	for _, c := range w.controls {
		if cb, ok := builders[c.ID]; ok {
			cb.focus(b, &items)
		}
	}

	w.focusFooter(b, &items)

	items = append(items, w.errView.focusItem())
	logFocusList("main", items)
	w.mainFocusItems = items
}

// boxVisible reports a container's visibility, for focus items whose widgets
// are shown and hidden as a group.
func boxVisible(box *gtk.Box) func() bool {
	return func() bool { return box.IsVisible() }
}

// focusProfileSection: the three firmware buttons share a row, and the Custom
// button that opens the custom view sits below them. The custom profiles
// themselves are navigated in that view, not here.

// focusAutoswitchSection: enable switch, then a dropdown per power source. The
// two target rows only exist while autoswitch is enabled, so they carry the
// container's visibility.

// focusBatterySection: the charge-limit slider.
func (w *Window) focusBatterySection(b *focusgrid.Builder, items *[]focusItem) {
	left, right, get, set := scaleAdjust(w.battScale, 5)
	c := b.Section("battery").One()
	*items = append(*items, focusItem{
		widget: w.battScale, row: c.Row, col: c.Col, section: c.Section,
		editable: true,
		onLeft:   left, onRight: right,
		getValue: get, setValue: set,
	})
}

// focusFooter: the theme button, then whichever firmware toggles this device
// has. They share one line, so the count is known only after the nil checks.
//
// The footer is fixed chrome outside the scroll area, so it is not a registry
// control and always comes last regardless of how the sections are ordered.
func (w *Window) focusFooter(b *focusgrid.Builder, items *[]focusItem) {
	b.Section("footer")
	footer := []struct {
		widget   gtk.Widgetter
		activate func()
	}{{w.paletteBtn, func() { w.showThemeView() }}}
	if sw := w.overdriveSwitch; sw != nil {
		footer = append(footer, struct {
			widget   gtk.Widgetter
			activate func()
		}{sw, func() { sw.SetActive(!sw.Active()) }})
	}
	if sw := w.bootSoundSwitch; sw != nil {
		footer = append(footer, struct {
			widget   gtk.Widgetter
			activate func()
		}{sw, func() { sw.SetActive(!sw.Active()) }})
	}
	for i, fc := range b.Line(len(footer)) {
		f := footer[i]
		*items = append(*items, focusItem{
			widget: f.widget, row: fc.Row, col: fc.Col, section: fc.Section,
			onActivate: f.activate,
		})
	}
}
