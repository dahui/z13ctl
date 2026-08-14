// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// themeview.go — the theme picker, one of the drawer's stack views.
//
// A radio per built-in theme, each followed by that theme's accent dots, plus a
// "Custom" entry when the user has a theme.toml. Radio buttons rather than a
// dropdown for the standing gamescope reason, and a full view rather than a
// popup because the list is long.
//
// The view owns its widgets and its focus list. The *palette* — Window.colors,
// themeProvider, isCustomTheme, customColors, customAccents — deliberately
// stays on Window: loadCSS resolves it at startup, long before this view is
// built, and applyTheme swaps a display-wide CSS provider that outlives any
// view. This file is the chooser, not the theme engine.

import (
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// dotsPerRow is the number of accent color dots per row in the theme picker.
const dotsPerRow = 7

// themeView is the theme picker page.
type themeView struct {
	w *Window

	scroll  *gtk.ScrolledWindow
	backBtn *gtk.Button

	// radios and dots are parallel: dots[i] holds the accent buttons belonging
	// to radios[i], and a theme with no accents contributes a nil row rather
	// than being skipped, so the two stay index-aligned.
	radios []*gtk.CheckButton
	dots   [][]*gtk.Button

	focusItems []focusItem
}

// buildThemeView builds the theme picker as a full scrollable view.
func (w *Window) buildThemeView() *gtk.Box {
	t := &themeView{w: w}
	w.themeView = t

	view := gtk.NewBox(gtk.OrientationVertical, 0)

	t.backBtn = gtk.NewButton()
	t.backBtn.SetIconName("go-previous-symbolic")
	t.backBtn.AddCSSClass("view-back-btn")
	t.backBtn.ConnectClicked(func() { w.showMainView() })

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.SetMarginTop(10)
	header.SetMarginBottom(6)
	header.SetMarginStart(14)
	header.Append(t.backBtn)
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
	t.appendChoices(content)

	t.scroll = newDrawerScroll(content)
	view.Append(t.scroll)
	return view
}

// appendChoices appends the theme radio buttons and accent dots to box,
// collecting the widget references the focus list needs.
func (t *themeView) appendChoices(box *gtk.Box) {
	w := t.w
	t.radios = nil
	t.dots = nil

	activeCfg := theme.LoadAppConfig()
	var first *gtk.CheckButton
	for _, th := range theme.Builtins {
		id := th.ID
		btn := gtk.NewCheckButtonWithLabel(th.Name)
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
				t.setActiveAccentDot(nil)
				w.applyTheme(id, "")
			}
		})
		if w.gamescope {
			addTouchActivate(btn, func() { btn.SetActive(true) })
		}
		t.radios = append(t.radios, btn)
		box.Append(btn)

		dots := t.appendAccentDots(box, th.Accents,
			func(ac theme.Accent) bool { return id == activeCfg.Theme && ac.ID == activeCfg.Accent },
			func(ac theme.Accent) { btn.SetActive(true); w.applyTheme(id, ac.ID) },
		)
		t.dots = append(t.dots, dots)
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
				t.setActiveAccentDot(nil)
				w.applyCustomAccent("")
			}
		})
		if w.gamescope {
			addTouchActivate(customBtn, func() { customBtn.SetActive(true) })
		}
		t.radios = append(t.radios, customBtn)
		box.Append(customBtn)

		dots := t.appendAccentDots(box, w.customAccents,
			func(ac theme.Accent) bool { return ac.ID == activeCfg.Accent },
			func(ac theme.Accent) { w.applyCustomAccent(ac.ID) },
		)
		t.dots = append(t.dots, dots)
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
func (t *themeView) setActiveAccentDot(active *gtk.Button) {
	for _, row := range t.dots {
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
func (t *themeView) appendAccentDots(box *gtk.Box, accents []theme.Accent, isActive func(theme.Accent) bool, onClick func(theme.Accent)) []*gtk.Button {
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
		t.w.setHint(dot, ac.Name)
		dot.ConnectClicked(func() {
			// onClick first: it may activate this theme's radio button, whose
			// toggled handler clears every dot. Marking afterwards survives that.
			onClick(ac)
			t.setActiveAccentDot(dot)
		})
		dots = append(dots, dot)
		row.Append(dot)
	}
	box.Append(dotsGrid)
	return dots
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
	if w.themeView == nil {
		w.viewStack.AddNamed(w.buildThemeView(), "theme")
		w.buildThemeFocusList()
	}
	w.viewStack.SetVisibleChildName("theme")
	w.swapFocusList(w.themeView.focusItems)
}

// buildThemeFocusList builds the 2D focus grid for the theme picker view.
// Must be called after appendChoices has populated the radios and dots.
func (w *Window) buildThemeFocusList() {
	t := w.themeView
	if t == nil {
		return
	}
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	// Back button.
	if t.backBtn != nil {
		c := b.Section("nav").One()
		items = append(items, focusItem{
			widget: t.backBtn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { w.showMainView() },
		})
	}

	// Each theme is a radio on its own line, followed by that theme's accent
	// dots wrapped at dotsPerRow. Grid owns the wrap arithmetic, so a theme
	// with a number of accents that does not divide evenly cannot overlap the
	// next theme's radio.
	b.Section("theme")
	for i, btn := range t.radios {
		btn := btn
		c := b.One()
		items = append(items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.SetActive(true) },
		})
		if i >= len(t.dots) {
			continue
		}
		dots := t.dots[i]
		for j, dc := range b.Grid(len(dots), dotsPerRow) {
			dot := dots[j]
			items = append(items, focusItem{
				widget: dot, row: dc.Row, col: dc.Col, section: dc.Section,
				onActivate: func() { dot.Activate() },
			})
		}
	}

	items = append(items, w.errView.focusItem())
	logFocusList("theme", items)
	t.focusItems = items
}
