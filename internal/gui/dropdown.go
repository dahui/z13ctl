// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// dropdown.go — the drawer's dropdown control: a trigger button plus an
// in-surface popup list (popup.go). There is deliberately no GtkDropDown or
// GtkMenuButton anywhere in the drawer: their popovers are separate surfaces
// gamescope will not composite as input-receiving windows. The trigger is an
// ordinary gtk.Button and the list is ordinary buttons in the popup surface,
// which is what lets the existing focus grid, touch handling and theming
// apply unchanged.

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// dropdownOption is one row of a dropdown list.
type dropdownOption struct {
	value string
	label string
	// selected marks the row the dropdown currently stands on.
	selected bool
	// running marks the running profile's row (profile selector only). It is
	// distinct from selected — the editor's target and the machine's active
	// profile are different facts, and the old in-flow selector marked both
	// with one CSS class.
	running bool
}

// dropdownConfig configures one dropdown.
type dropdownConfig struct {
	// options returns the rows to offer, evaluated on every open so a list
	// changed by another client (a profile created over the CLI) is fresh
	// without any rebuild machinery.
	options func() []dropdownOption
	// onSelect runs after the popup has closed with the chosen value. It is
	// not called when the popup is dismissed without a choice.
	onSelect func(value string)
}

// dropdown is a trigger button that opens an in-surface popup list.
type dropdown struct {
	w   *Window
	cfg dropdownConfig
	btn *gtk.Button
	lbl *gtk.Label // trigger text; ellipsized, user-typed names can be long
}

// newDropdown builds the trigger button. The list itself is built per open.
func (w *Window) newDropdown(cfg dropdownConfig) *dropdown {
	d := &dropdown{w: w, cfg: cfg}

	d.btn = gtk.NewButton()
	d.btn.AddCSSClass("dropdown-trigger")
	d.btn.SetHExpand(true)

	d.lbl = gtk.NewLabel("")
	d.lbl.SetEllipsize(pango.EllipsizeEnd)
	d.lbl.SetXAlign(0)
	d.lbl.SetHExpand(true)
	arrow := gtk.NewLabel("▾")
	inner := gtk.NewBox(gtk.OrientationHorizontal, 4)
	inner.Append(d.lbl)
	inner.Append(arrow)
	d.btn.SetChild(inner)

	d.btn.ConnectClicked(func() { d.openList() })
	return d
}

// setLabel sets the trigger's collapsed text.
func (d *dropdown) setLabel(text string) { d.lbl.SetText(text) }

// openList builds the option list from cfg.options and opens it in the popup
// layer, anchored to the trigger. A second click on the trigger while the
// list is open lands on the scrim, which closes it — the toggle comes free.
func (d *dropdown) openList() {
	opts := d.cfg.options()

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	// .btn-group is what styles the rows: `.drawer .btn-group button` is
	// already themed and already scaled under gamescope.
	box.AddCSSClass("btn-group")

	items := make([]focusItem, 0, len(opts))
	for i, opt := range opts {
		opt := opt
		btn := gtk.NewButton()
		btn.SetHExpand(true)

		// A custom label child rather than NewButtonWithLabel: option text
		// includes user-typed profile names up to api.MaxProfileNameLen, and
		// an unbreakable long one would otherwise drive the popup wider than
		// the panel — the same failure the error bar solved with
		// MaxWidthChars, reached by another route.
		lbl := gtk.NewLabel(opt.label)
		lbl.SetEllipsize(pango.EllipsizeEnd)
		lbl.SetXAlign(0)
		lbl.SetHExpand(true)
		row := gtk.NewBox(gtk.OrientationHorizontal, 4)
		row.Append(lbl)
		if opt.running {
			dot := gtk.NewLabel("●")
			dot.AddCSSClass("running-dot")
			row.Append(dot)
		}
		btn.SetChild(row)

		if opt.selected {
			btn.AddCSSClass("active")
		}
		// Ordinary click is enough: GtkButton's own gesture runs in the
		// capture phase, so touch works under gamescope's XWayland with no
		// addTouchActivate. Close first, then select — onSelect may re-sync
		// views, and syncs are suppressed while a popup is open.
		btn.ConnectClicked(func() {
			d.w.closePopup()
			if d.cfg.onSelect != nil {
				d.cfg.onSelect(opt.value)
			}
		})

		items = append(items, focusItem{
			widget: btn, row: i, col: 0,
			section:    "popup",
			onActivate: func() { btn.Activate() },
		})
		box.Append(btn)
	}

	d.w.openPopup(d.btn, box, items, nil)
}
