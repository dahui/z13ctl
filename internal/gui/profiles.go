// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// profiles.go — the custom view's profile selector: which profile the editor
// is pointed at, the create/copy/activate affordances, and the inline name
// entry. It is part of customView (customview.go), split out because it is a
// self-contained block with its own rules.
//
// Every rule here — which rows exist, which affordances each carries, how a
// name is validated — lives in internal/profileui where it is unit tested;
// this file only builds widgets and applies the answers. The main view's own
// profile buttons are a different thing and live in mainprofile.go.

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	nameModeCreate = "create"
	nameModeSaveAs = "save-as"
)

// buildProfileSelector creates the custom view's profile selector: a dropdown
// naming the profile being edited, and the create/copy/activate affordances.
//
// The dropdown's list opens in the in-surface popup layer (popup.go) — never
// a GtkDropDown, whose popover is a separate window gamescope does not
// composite. The list is built from profileui.CustomRows on every open, so a
// profile created or deleted by another client is simply present or absent
// the next time the list opens, with no rebuild machinery in between.
func (c *customView) buildProfileSelector() *gtk.Box {
	w := c.w
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("PROFILE"))

	selRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	selRow.AddCSSClass("btn-group")
	c.selDD = w.newDropdown(dropdownConfig{
		options: func() []dropdownOption {
			rows := profileui.CustomRows(w.state)
			opts := make([]dropdownOption, len(rows))
			for i, r := range rows {
				opts[i] = dropdownOption{
					value: r.Name,
					label: r.Label,
					// Two distinct marks: selected is the edit target, the
					// running dot is the active profile. The old in-flow
					// selector collapsed both onto one .active class.
					selected: r.Name == c.editProfile,
					running:  r.Active,
				}
			}
			return opts
		},
		onSelect: func(name string) { c.setEditTarget(name) },
	})
	w.setHint(c.selDD.btn, "Choose which custom profile to edit")
	selRow.Append(c.selDD.btn)
	box.Append(selRow)

	actions := gtk.NewBox(gtk.OrientationHorizontal, 4)
	actions.AddCSSClass("btn-group")
	c.activateBtn = gtk.NewButtonWithLabel("Activate")
	c.activateBtn.AddCSSClass("save-btn")
	c.activateBtn.SetHExpand(true)
	w.setHint(c.activateBtn, "Apply this profile to the machine")
	c.activateBtn.ConnectClicked(func() { w.sendProfileSet(c.editProfile) })
	actions.Append(c.activateBtn)
	c.newProfileBtn = gtk.NewButtonWithLabel("+ New")
	c.newProfileBtn.SetHExpand(true)
	w.setHint(c.newProfileBtn, "Create an empty named profile")
	c.newProfileBtn.ConnectClicked(func() { c.showNameEntry(nameModeCreate) })
	actions.Append(c.newProfileBtn)
	c.saveAsBtn = gtk.NewButtonWithLabel("Save As")
	c.saveAsBtn.SetHExpand(true)
	w.setHint(c.saveAsBtn, "Copy the active profile's settings under a new name")
	c.saveAsBtn.ConnectClicked(func() { c.showNameEntry(nameModeSaveAs) })
	actions.Append(c.saveAsBtn)
	c.symmetricRow(actions)
	// Kept on the struct so the window can add Delete to this row — a
	// profile operation belongs with the profile operations.
	c.profileActions = actions
	box.Append(actions)

	// One note serves both refusals — they are almost always blocked together
	// (a fresh profile blocks Activate and Save As at once).
	c.actionsNote = blockNote()
	box.Append(c.actionsNote)

	// Inline name entry, in place of a dialog, for the same reason the
	// selector expands in place: it appears only while a name is being chosen.
	c.nameRow = gtk.NewBox(gtk.OrientationHorizontal, 4)
	c.nameEntry = gtk.NewEntry()
	c.nameEntry.SetHExpand(true)
	c.nameEntry.SetMaxLength(api.MaxProfileNameLen)
	c.nameEntry.ConnectActivate(func() { c.confirmNameEntry() })
	c.nameRow.Append(c.nameEntry)
	c.nameOKBtn = gtk.NewButtonWithLabel("OK")
	c.nameOKBtn.ConnectClicked(func() { c.confirmNameEntry() })
	c.nameRow.Append(c.nameOKBtn)
	c.nameCancelBtn = gtk.NewButton()
	c.nameCancelBtn.SetIconName("window-close-symbolic")
	w.setHint(c.nameCancelBtn, "Cancel")
	c.nameCancelBtn.ConnectClicked(func() { c.nameRow.SetVisible(false) })
	c.nameRow.Append(c.nameCancelBtn)
	c.nameRow.SetVisible(false)
	box.Append(c.nameRow)

	c.selDD.setLabel(profileui.Label(c.editProfile))
	return box
}

// setEditTarget points the custom view at another profile and re-syncs every
// widget to it.
func (c *customView) setEditTarget(name string) {
	c.editProfile = name
	c.disarmDelete()
	c.sync()
}

// syncProfileSelector updates the selector's trigger label and the action
// buttons' sensitivity from daemon state. There is nothing else to sync: the
// dropdown list is built fresh on every open, so a profile created or deleted
// by another client needs no rebuild here — and nothing below the selector
// shifts when the profile set changes, which is why the custom focus list is
// built exactly once.
func (c *customView) syncProfileSelector() {
	if c.selDD == nil {
		return
	}
	c.selDD.setLabel(profileui.Label(c.editProfile))

	// Refusal reasons go to the shared block note, in the flow of the view —
	// never to tooltips, which are invisible in gamescope and unreachable on
	// a controller (the focus grid skips insensitive widgets).
	blocks := make([]string, 0, 2)
	if c.activateBtn != nil {
		block := profileui.ActivateBlock(c.w.state, c.editProfile)
		c.activateBtn.SetSensitive(block == "")
		if block != "" {
			blocks = append(blocks, "Activate: "+block)
		}
	}
	if c.saveAsBtn != nil {
		block := profileui.SaveAsBlock(c.w.state)
		c.saveAsBtn.SetSensitive(block == "")
		if block != "" {
			blocks = append(blocks, "Save As: "+block)
		}
	}
	setBlockNote(c.actionsNote, strings.Join(blocks, " · "))
}

// showNameEntry opens the inline name row prefilled with a suggested name.
// The prefill is not a convenience: a gamepad user cannot type into the entry
// at all, so the suggestion (always valid and free) is the name they get.
func (c *customView) showNameEntry(mode string) {
	c.nameMode = mode
	c.nameEntry.SetText(profileui.SuggestName(c.w.state))
	c.nameRow.SetVisible(true)
	c.nameEntry.GrabFocus()
}

// confirmNameEntry validates the typed name and sends the create or save-as.
// A bad name keeps the row open for correction; the daemon's own refusal is
// still the final word and lands in the error bar like any other failure.
func (c *customView) confirmNameEntry() {
	w := c.w
	name := strings.TrimSpace(c.nameEntry.Text())
	mode := c.nameMode
	var problem string
	if mode == nameModeSaveAs {
		problem = profileui.SaveAsNameProblem(name)
	} else {
		problem = profileui.CreateNameProblem(w.state, name)
	}
	if problem != "" {
		w.reportError(nameOpLabel(mode), errors.New(problem))
		return
	}
	c.nameRow.SetVisible(false)
	go func() {
		var handled bool
		var err error
		if mode == nameModeSaveAs {
			handled, err = api.SendProfileSave(name)
		} else {
			handled, err = api.SendProfileCreate(name)
		}
		if e := apiresult.Err(handled, err); e != nil {
			w.reportError(nameOpLabel(mode), e)
			return
		}
		w.clearErrorAsync()
		slog.Info("profile "+mode, "profile", name)
		// Edit what was just created: creating a profile and then having to
		// find it in the selector is a step with no purpose.
		glib.IdleAdd(func() bool {
			c.setEditTarget(name)
			return false
		})
		w.refreshState()
	}()
}

func nameOpLabel(mode string) string {
	if mode == nameModeSaveAs {
		return "Save profile as"
	}
	return "Create profile"
}
