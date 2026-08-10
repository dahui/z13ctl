// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// controls.go — builds the entire drawer widget tree, theme picker view,
// and HSL color picker view. All views live in a gtk.Stack (both KDE and
// gamescope modes) for consistent gamepad navigation.

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/dahui/voltaire/v2/internal/controls"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
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

// buildLightingSection appends the whole RGB block: zone tabs, effect modes,
// both colour rows, speed and brightness. It is one control because its parts
// are not independently meaningful — syncModeVis already decides which of them
// are visible from the selected effect.
func (w *Window) buildLightingSection(inner *gtk.Box) {
	inner.Append(w.buildTabRow())
	inner.Append(w.buildModeSection())

	// Initialize color inputs here so syncModeVis can reference them.
	w.color1 = w.newColorInput("FF0000", "color1-swatch", "COLOR 1")
	w.color2 = w.newColorInput("000000", "color2-swatch", "COLOR 2")
	w.updateSwatches()

	w.color1Box = colorSubBox("COLOR 1", w.color1.row)
	w.color2Box = colorSubBox("COLOR 2", w.color2.row)
	inner.Append(w.color1Box)
	inner.Append(w.color2Box)

	w.speedBox = w.buildSpeedBox()
	inner.Append(w.speedBox)
	w.brightBox = w.buildBrightnessBox()
	inner.Append(w.brightBox)
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
	content.Append(w.buildErrorBar())

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

// buildThemeView builds the theme picker as a full scrollable view.
func (w *Window) buildThemeView() *gtk.Box {
	view := gtk.NewBox(gtk.OrientationVertical, 0)

	w.themeBackBtn = gtk.NewButton()
	w.themeBackBtn.SetIconName("go-previous-symbolic")
	w.themeBackBtn.AddCSSClass("view-back-btn")
	w.themeBackBtn.ConnectClicked(func() { w.showMainView() })

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.SetMarginTop(10)
	header.SetMarginBottom(6)
	header.SetMarginStart(14)
	header.Append(w.themeBackBtn)
	lbl := gtk.NewLabel("Theme")
	lbl.SetHAlign(gtk.AlignStart)
	lbl.AddCSSClass("drawer-title")
	header.Append(lbl)
	view.Append(header)

	content := gtk.NewBox(gtk.OrientationVertical, 2)
	content.SetMarginTop(4)
	content.SetMarginBottom(12)
	content.SetMarginStart(12)
	content.SetMarginEnd(12)
	w.appendThemeChoices(content)

	scroll := newDrawerScroll(content)
	w.themeScroll = scroll
	view.Append(scroll)
	return view
}

// appendThemeChoices appends the theme radio buttons and accent dots to box.
// Also collects widget references in w.themeRadios and w.themeDots for
// building the theme focus list.
func (w *Window) appendThemeChoices(box *gtk.Box) {
	w.themeRadios = nil
	w.themeDots = nil

	activeCfg := theme.LoadAppConfig()
	var first *gtk.CheckButton
	for _, t := range theme.Builtins {
		id := t.ID
		btn := gtk.NewCheckButtonWithLabel(t.Name)
		if first == nil {
			first = btn
		} else {
			btn.SetGroup(first)
		}
		if id == activeCfg.Theme {
			btn.SetActive(true)
		}
		btn.ConnectToggled(func() {
			if btn.Active() {
				// Selecting the theme itself means its default accent, so no dot is
				// the active one. A dot click re-marks itself afterwards.
				w.setActiveAccentDot(nil)
				w.applyTheme(id, "")
			}
		})
		if w.gamescope {
			addTouchActivate(btn, func() { btn.SetActive(true) })
		}
		w.themeRadios = append(w.themeRadios, btn)
		box.Append(btn)

		dots := w.appendAccentDots(box, t.Accents,
			func(ac theme.Accent) bool { return id == activeCfg.Theme && ac.ID == activeCfg.Accent },
			func(ac theme.Accent) { btn.SetActive(true); w.applyTheme(id, ac.ID) },
		)
		w.themeDots = append(w.themeDots, dots)
	}

	// Custom theme entry — shown when theme.toml is active.
	if w.isCustomTheme {
		customBtn := gtk.NewCheckButtonWithLabel("Custom")
		if first != nil {
			customBtn.SetGroup(first)
		}
		customBtn.SetActive(true)
		customBtn.ConnectToggled(func() {
			if customBtn.Active() {
				w.setActiveAccentDot(nil)
				w.applyCustomAccent("")
			}
		})
		if w.gamescope {
			addTouchActivate(customBtn, func() { customBtn.SetActive(true) })
		}
		w.themeRadios = append(w.themeRadios, customBtn)
		box.Append(customBtn)

		dots := w.appendAccentDots(box, w.customAccents,
			func(ac theme.Accent) bool { return ac.ID == activeCfg.Accent },
			func(ac theme.Accent) { w.applyCustomAccent(ac.ID) },
		)
		w.themeDots = append(w.themeDots, dots)
	}
}

// setActiveAccentDot moves the .accent-dot-active marker to active, clearing it
// from every other dot across every theme. Pass nil to clear it entirely.
//
// The marker used to be applied once, while the theme view was being built, from
// the config file's saved accent — and the view is built lazily exactly once and
// then kept. So picking a different accent left the marker where it was, and
// switching theme left the previous theme's dot marked. Nothing showed which
// accent was actually in force.
func (w *Window) setActiveAccentDot(active *gtk.Button) {
	for _, row := range w.themeDots {
		for _, dot := range row {
			if dot != nil && dot == active {
				dot.AddCSSClass("accent-dot-active")
			} else if dot != nil {
				dot.RemoveCSSClass("accent-dot-active")
			}
		}
	}
}

// appendAccentDots builds the "Accent Color" label and dot button grid for the
// given accent list and appends both to box. Returns the dot buttons for use
// in the focus list. isActive reports whether a dot should be marked active;
// onClick is called when a dot is clicked.
func (w *Window) appendAccentDots(box *gtk.Box, accents []theme.Accent, isActive func(theme.Accent) bool, onClick func(theme.Accent)) []*gtk.Button {
	if len(accents) == 0 {
		return nil
	}
	accentLabel := gtk.NewLabel("Accent Color")
	accentLabel.SetXAlign(0)
	accentLabel.AddCSSClass("accent-label")
	accentLabel.SetMarginStart(12)
	accentLabel.SetMarginTop(2)
	box.Append(accentLabel)

	dotsGrid := gtk.NewBox(gtk.OrientationVertical, 4)
	dotsGrid.SetMarginStart(12)
	dotsGrid.SetMarginBottom(4)

	var dots []*gtk.Button
	var row *gtk.Box
	for i, ac := range accents {
		ac := ac
		if i%dotsPerRow == 0 {
			row = gtk.NewBox(gtk.OrientationHorizontal, 4)
			dotsGrid.Append(row)
		}
		dot := gtk.NewButton()
		dot.AddCSSClass("color-preset")
		dot.SetHExpand(true)
		if isActive(ac) {
			dot.AddCSSClass("accent-dot-active")
		}
		provider := gtk.NewCSSProvider()
		provider.LoadFromString("button.color-preset { background: " + ac.Hex + "; }")
		dot.StyleContext().AddProvider(provider, gtk.STYLE_PROVIDER_PRIORITY_USER+20) //nolint:staticcheck // per-widget dynamic color; no style-class alternative for unique hex backgrounds
		w.setHint(dot, ac.Name)
		dot.ConnectClicked(func() {
			// onClick first: it may activate this theme's radio button, whose
			// toggled handler clears every dot. Marking afterwards survives that.
			onClick(ac)
			w.setActiveAccentDot(dot)
		})
		dots = append(dots, dot)
		row.Append(dot)
	}
	box.Append(dotsGrid)
	return dots
}

// buildColorPickerView builds the HSL color picker view.
// Contains preset buttons, hue/saturation/lightness sliders, and a preview swatch.
func (w *Window) buildColorPickerView() *gtk.Box {
	view := gtk.NewBox(gtk.OrientationVertical, 8)
	view.SetMarginStart(12)
	view.SetMarginEnd(12)

	// Header: back button + dynamic title.
	w.colorViewTitle = gtk.NewLabel("COLOR")
	w.colorBackBtn = gtk.NewButton()
	w.colorBackBtn.SetIconName("go-previous-symbolic")
	w.colorBackBtn.AddCSSClass("view-back-btn")
	w.colorBackBtn.ConnectClicked(func() { w.showMainView() })

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.SetMarginTop(10)
	header.SetMarginBottom(6)
	header.Append(w.colorBackBtn)
	w.colorViewTitle.SetHAlign(gtk.AlignStart)
	w.colorViewTitle.AddCSSClass("drawer-title")
	header.Append(w.colorViewTitle)
	view.Append(header)

	// 8 preset buttons.
	w.colorPickerPresets = nil
	presetsRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	for _, hex := range presetColors {
		h := hex
		btn := gtk.NewButton()
		btn.AddCSSClass("color-preset")
		btn.SetHExpand(true)
		p := gtk.NewCSSProvider()
		p.LoadFromString(fmt.Sprintf("button.color-preset { background: #%s; }", h))
		btn.StyleContext().AddProvider(p, gtk.STYLE_PROVIDER_PRIORITY_USER+5) //nolint:staticcheck // per-widget dynamic color
		btn.ConnectClicked(func() { w.colorPickerPresetClicked(h) })
		w.colorPickerPresets = append(w.colorPickerPresets, btn)
		presetsRow.Append(btn)
	}
	view.Append(presetsRow)

	// HSL sliders.
	w.colorHue = w.buildHSLScale("HUE", 0, 360)
	w.colorSat = w.buildHSLScale("SATURATION", 0, 100)
	w.colorLit = w.buildHSLScale("LIGHTNESS", 0, 100)

	view.Append(hslScaleBox("HUE", w.colorHue))
	view.Append(hslScaleBox("SATURATION", w.colorSat))
	view.Append(hslScaleBox("LIGHTNESS", w.colorLit))

	// Preview swatch + hex label.
	w.colorSwatchProv = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), w.colorSwatchProv,
		gtk.STYLE_PROVIDER_PRIORITY_USER+10,
	)

	w.colorPreview = gtk.NewBox(gtk.OrientationHorizontal, 0)
	w.colorPreview.AddCSSClass("color-swatch")
	w.colorPreview.SetName("color-picker-preview")

	w.colorHexLabel = gtk.NewLabel("#FF0000")
	w.colorHexLabel.AddCSSClass("section-label")

	previewRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	previewRow.SetMarginTop(4)
	previewRow.Append(w.colorPreview)
	previewRow.Append(w.colorHexLabel)
	view.Append(previewRow)

	return view
}

// buildHSLScale creates a Scale for an HSL component.
func (w *Window) buildHSLScale(_ string, lo, hi float64) *gtk.Scale {
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, lo, hi, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(true)
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() { w.onHSLChanged() })
	return sc
}

// hslScaleBox wraps a section label + scale into a box.
func hslScaleBox(label string, sc *gtk.Scale) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 2)
	box.Append(sectionLabel(label))
	box.Append(sc)
	return box
}

// showMainView switches the view stack to the main drawer view.
// Every show*View closes any open popup first: a popup is anchored to a
// widget in the view it was opened from, and a view switch underneath it
// would leave the scrim and list floating over the wrong view.
func (w *Window) showMainView() {
	w.closePopup()
	if w.viewStack != nil {
		w.viewStack.SetVisibleChildName("main")
		w.swapFocusList(w.mainFocusItems)
	}
}

// showCustomView switches the view stack to the custom profile view, opening
// on the running custom profile when there is one and "custom" otherwise.
// Lazy-builds the view on first access; a second tap returns to the main view.
func (w *Window) showCustomView() {
	if w.viewStack == nil {
		return
	}
	w.closePopup()
	if w.viewStack.VisibleChildName() == "custom" {
		w.showMainView()
		return
	}
	w.editProfile = profileui.DefaultEditTarget(w.state)
	if w.customScroll == nil {
		w.viewStack.AddNamed(w.buildCustomView(), "custom")
		w.buildCustomFocusList()
	}
	w.disarmDelete()
	w.syncCustomView()
	w.viewStack.SetVisibleChildName("custom")
	w.swapFocusList(w.customFocusItems)
	w.startTelemetryPolling()
}

// showThemeView switches the view stack to the theme picker view.
// The theme view is lazy-built on first access to keep the initial widget tree small.
func (w *Window) showThemeView() {
	if w.viewStack == nil {
		return
	}
	w.closePopup()
	if w.viewStack.VisibleChildName() == "theme" {
		w.showMainView()
		return
	}
	if w.themeScroll == nil {
		w.viewStack.AddNamed(w.buildThemeView(), "theme")
		w.buildThemeFocusList()
	}
	w.viewStack.SetVisibleChildName("theme")
	w.swapFocusList(w.themeFocusItems)
}

// buildTabRow creates the Keyboard / Lightbar tab radio buttons.
func (w *Window) buildTabRow() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)

	kb := gtk.NewCheckButtonWithLabel("Keyboard")
	kb.SetActive(true)
	lb := gtk.NewCheckButtonWithLabel("Lightbar")
	lb.SetGroup(kb)

	kb.AddCSSClass("tab-btn")
	lb.AddCSSClass("tab-btn")
	kb.SetHExpand(true)
	lb.SetHExpand(true)

	kb.ConnectToggled(func() {
		if kb.Active() {
			w.tab = "keyboard"
			w.syncLightingSection()
		}
	})
	lb.ConnectToggled(func() {
		if lb.Active() {
			w.tab = "lightbar"
			w.syncLightingSection()
		}
	})

	w.tabKB = kb
	w.tabLB = lb

	if w.gamescope {
		addTouchActivate(kb, func() { kb.SetActive(true) })
		addTouchActivate(lb, func() { lb.SetActive(true) })
	}

	row.Append(kb)
	row.Append(lb)
	return row
}

// dotsPerRow is the number of accent color dots per row in the theme picker.
const dotsPerRow = 7

// modeOrder defines the display order for lighting mode buttons.
var modeOrder = []string{
	"static", "breathe", "cycle",
	"rainbow", "strobe", "off",
}

// buildModeSection creates the 3x2 grid of lighting mode buttons.
func (w *Window) buildModeSection() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("MODE"))

	grid := gtk.NewGrid()
	grid.SetColumnSpacing(4)
	grid.SetRowSpacing(4)
	grid.AddCSSClass("mode-grid")
	grid.AddCSSClass("btn-group")
	grid.SetColumnHomogeneous(true)

	for i, m := range modeOrder {
		mode := m
		btn := gtk.NewButtonWithLabel(strings.Title(mode)) //nolint:staticcheck // strings.Title is fine for ASCII-only mode/speed/profile labels
		btn.ConnectClicked(func() {
			setActiveButton(w.modeButtons, mode)
			w.syncModeVis()
			w.sendApply()
		})
		w.modeButtons[mode] = btn
		grid.Attach(btn, i%3, i/3, 1, 1)
	}

	box.Append(grid)
	return box
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

// buildSpeedBox creates the slow/normal/fast button row.
func (w *Window) buildSpeedBox() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("SPEED"))
	box.Append(w.buildButtonGroup(gtk.OrientationHorizontal, speeds, w.speedBtns, func(_ string) {
		w.sendApply()
	}))
	setActiveButton(w.speedBtns, "normal")
	return box
}

// buildBrightnessBox creates the brightness scale (0–3).
func (w *Window) buildBrightnessBox() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("BRIGHTNESS"))

	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, 0, 3, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(true)
	sc.SetValue(3)
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() {
		w.queueApply()
	})
	w.brightScale = sc
	box.Append(sc)
	return box
}

// speeds lists the available lighting animation speeds.
var speeds = []string{"slow", "normal", "fast"}

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

	items = append(items, w.errBarFocusItem())
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
func (w *Window) focusProfileSection(b *focusgrid.Builder, items *[]focusItem) {
	b.Section("profile")
	stock := profileui.StockRows(nil)
	for i, c := range b.Line(len(stock)) {
		btn := w.profileBtns[stock[i].Name]
		*items = append(*items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.Activate() },
		})
	}
	c := b.One()
	*items = append(*items, focusItem{
		widget: w.customBtn, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { w.customBtn.Activate() },
	})
}

// focusAutoswitchSection: enable switch, then a dropdown per power source. The
// two target rows only exist while autoswitch is enabled, so they carry the
// container's visibility.
func (w *Window) focusAutoswitchSection(b *focusgrid.Builder, items *[]focusItem) {
	if w.autoswitchSwitch == nil {
		return
	}
	b.Section("autoswitch")
	sw := w.autoswitchSwitch
	c := b.One()
	*items = append(*items, focusItem{
		widget: sw, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { sw.SetActive(!sw.Active()) },
	})
	targetsVis := boxVisible(w.autoswitchTargets)
	c = b.One()
	*items = append(*items, focusItem{
		widget: w.autoswitchACDD.btn, row: c.Row, col: c.Col, section: c.Section,
		isVisible:  targetsVis,
		onActivate: func() { w.autoswitchACDD.btn.Activate() },
	})
	c = b.One()
	*items = append(*items, focusItem{
		widget: w.autoswitchBattDD.btn, row: c.Row, col: c.Col, section: c.Section,
		isVisible:  targetsVis,
		onActivate: func() { w.autoswitchBattDD.btn.Activate() },
	})
}

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

// focusLightingSection: zone tabs, the mode grid, both colour rows, speed and
// brightness — the focus half of buildLightingSection, in the same order.
func (w *Window) focusLightingSection(b *focusgrid.Builder, items *[]focusItem) {
	// Device tabs — horizontal row.
	b.Section("tabs")
	tabs := []*gtk.CheckButton{w.tabKB, w.tabLB}
	for i, tc := range b.Line(len(tabs)) {
		btn := tabs[i]
		*items = append(*items, focusItem{
			widget: btn, row: tc.Row, col: tc.Col, section: tc.Section,
			onActivate: func() { btn.SetActive(true) },
		})
	}

	// Mode buttons — a grid three wide. Grid owns how many rows that is, so
	// adding a seventh mode no longer collides with the section below.
	b.Section("mode")
	for i, mc := range b.Grid(len(modeOrder), 3) {
		btn := w.modeButtons[modeOrder[i]]
		*items = append(*items, focusItem{
			widget: btn, row: mc.Row, col: mc.Col, section: mc.Section,
			onActivate: func() { btn.Activate() },
		})
	}

	// Colour presets: a row of swatches, then the custom button below them.
	addColor := func(section string, ci *colorInput, box *gtk.Box) {
		if ci == nil {
			return
		}
		vis := boxVisible(box)
		b.Section(section)
		for i, pc := range b.Line(len(ci.presetBtns)) {
			btn := ci.presetBtns[i]
			*items = append(*items, focusItem{
				widget: btn, row: pc.Row, col: pc.Col, section: pc.Section,
				isVisible:  vis,
				onActivate: func() { btn.Activate() },
			})
		}
		cc := b.One()
		*items = append(*items, focusItem{
			widget: ci.customBtn, row: cc.Row, col: cc.Col, section: cc.Section,
			isVisible:  vis,
			onActivate: func() { w.showColorView(ci) },
		})
	}
	addColor("color1", w.color1, w.color1Box)
	addColor("color2", w.color2, w.color2Box)

	// Speed buttons — horizontal row.
	b.Section("speed")
	for i, sc := range b.Line(len(speeds)) {
		btn := w.speedBtns[speeds[i]]
		*items = append(*items, focusItem{
			widget: btn, row: sc.Row, col: sc.Col, section: sc.Section,
			isVisible:  boxVisible(w.speedBox),
			onActivate: func() { btn.Activate() },
		})
	}

	// Brightness slider.
	left, right, get, set := scaleAdjust(w.brightScale, 1)
	c := b.Section("brightness").One()
	*items = append(*items, focusItem{
		widget: w.brightScale, row: c.Row, col: c.Col, section: c.Section,
		isVisible: boxVisible(w.brightBox),
		editable:  true,
		onLeft:    left, onRight: right,
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

// buildThemeFocusList builds the 2D focus grid for the theme picker view.
// Must be called after appendThemeChoices has populated w.themeRadios/w.themeDots.
func (w *Window) buildThemeFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	// Back button.
	if w.themeBackBtn != nil {
		c := b.Section("nav").One()
		items = append(items, focusItem{
			widget: w.themeBackBtn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { w.showMainView() },
		})
	}

	// Each theme is a radio on its own line, followed by that theme's accent
	// dots wrapped at dotsPerRow. Grid owns the wrap arithmetic, so a theme
	// with a number of accents that does not divide evenly cannot overlap the
	// next theme's radio.
	b.Section("theme")
	for i, btn := range w.themeRadios {
		btn := btn
		c := b.One()
		items = append(items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.SetActive(true) },
		})
		if i >= len(w.themeDots) {
			continue
		}
		dots := w.themeDots[i]
		for j, dc := range b.Grid(len(dots), dotsPerRow) {
			dot := dots[j]
			items = append(items, focusItem{
				widget: dot, row: dc.Row, col: dc.Col, section: dc.Section,
				onActivate: func() { dot.Activate() },
			})
		}
	}

	items = append(items, w.errBarFocusItem())
	logFocusList("theme", items)
	w.themeFocusItems = items
}

// buildColorFocusList builds the 2D focus grid for the HSL color picker view.
func (w *Window) buildColorFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	// Back button.
	if w.colorBackBtn != nil {
		c := b.Section("nav").One()
		items = append(items, focusItem{
			widget: w.colorBackBtn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { w.showMainView() },
		})
	}

	// Colour presets.
	b.Section("presets")
	for i, pc := range b.Line(len(w.colorPickerPresets)) {
		btn := w.colorPickerPresets[i]
		items = append(items, focusItem{
			widget: btn, row: pc.Row, col: pc.Col, section: pc.Section,
			onActivate: func() { btn.Activate() },
		})
	}

	// HSL sliders (editable), one per line.
	b.Section("sliders")
	for _, sc := range []*gtk.Scale{w.colorHue, w.colorSat, w.colorLit} {
		oL, oR, gV, sV := scaleAdjust(sc, 5)
		c := b.One()
		items = append(items, focusItem{
			widget: sc, row: c.Row, col: c.Col, section: c.Section,
			editable: true,
			onLeft:   oL, onRight: oR,
			getValue: gV, setValue: sV,
		})
	}

	items = append(items, w.errBarFocusItem())
	logFocusList("color", items)
	w.colorFocusItems = items
}
