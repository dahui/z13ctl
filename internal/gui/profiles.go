// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// profiles.go — the main view's profile buttons, the custom view's profile
// selector (with its create/save-as/activate affordances), and the autoswitch
// section. Every rule here (which rows exist, which affordances each carries,
// which autoswitch targets are offered, how a name is validated) lives in
// internal/profileui where it is unit tested; this file only builds widgets
// and applies the answers.

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
func (w *Window) buildProfileSelector() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("PROFILE"))

	selRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	selRow.AddCSSClass("btn-group")
	w.profileSelDD = w.newDropdown(dropdownConfig{
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
					selected: r.Name == w.editProfile,
					running:  r.Active,
				}
			}
			return opts
		},
		onSelect: func(name string) { w.setEditTarget(name) },
	})
	w.setHint(w.profileSelDD.btn, "Choose which custom profile to edit")
	selRow.Append(w.profileSelDD.btn)
	box.Append(selRow)

	actions := gtk.NewBox(gtk.OrientationHorizontal, 4)
	actions.AddCSSClass("btn-group")
	w.activateBtn = gtk.NewButtonWithLabel("Activate")
	w.activateBtn.AddCSSClass("save-btn")
	w.activateBtn.SetHExpand(true)
	w.setHint(w.activateBtn, "Apply this profile to the machine")
	w.activateBtn.ConnectClicked(func() { w.sendProfileSet(w.editProfile) })
	actions.Append(w.activateBtn)
	w.newProfileBtn = gtk.NewButtonWithLabel("+ New")
	w.newProfileBtn.SetHExpand(true)
	w.setHint(w.newProfileBtn, "Create an empty named profile")
	w.newProfileBtn.ConnectClicked(func() { w.showNameEntry(nameModeCreate) })
	actions.Append(w.newProfileBtn)
	w.saveAsBtn = gtk.NewButtonWithLabel("Save As")
	w.saveAsBtn.SetHExpand(true)
	w.setHint(w.saveAsBtn, "Copy the active profile's settings under a new name")
	w.saveAsBtn.ConnectClicked(func() { w.showNameEntry(nameModeSaveAs) })
	actions.Append(w.saveAsBtn)
	box.Append(actions)

	// One note serves both refusals — they are almost always blocked together
	// (a fresh profile blocks Activate and Save As at once).
	w.actionsNote = blockNote()
	box.Append(w.actionsNote)

	// Inline name entry, in place of a dialog, for the same reason the
	// selector expands in place: it appears only while a name is being chosen.
	w.nameRow = gtk.NewBox(gtk.OrientationHorizontal, 4)
	w.nameEntry = gtk.NewEntry()
	w.nameEntry.SetHExpand(true)
	w.nameEntry.SetMaxLength(api.MaxProfileNameLen)
	w.nameEntry.ConnectActivate(func() { w.confirmNameEntry() })
	w.nameRow.Append(w.nameEntry)
	w.nameOKBtn = gtk.NewButtonWithLabel("OK")
	w.nameOKBtn.ConnectClicked(func() { w.confirmNameEntry() })
	w.nameRow.Append(w.nameOKBtn)
	w.nameCancelBtn = gtk.NewButton()
	w.nameCancelBtn.SetIconName("window-close-symbolic")
	w.setHint(w.nameCancelBtn, "Cancel")
	w.nameCancelBtn.ConnectClicked(func() { w.nameRow.SetVisible(false) })
	w.nameRow.Append(w.nameCancelBtn)
	w.nameRow.SetVisible(false)
	box.Append(w.nameRow)

	w.profileSelDD.setLabel(profileui.Label(w.editProfile))
	return box
}

// setEditTarget points the custom view at another profile and re-syncs every
// widget to it.
func (w *Window) setEditTarget(name string) {
	w.editProfile = name
	w.disarmDelete()
	w.syncCustomView()
}

// syncProfileSelector updates the selector's trigger label and the action
// buttons' sensitivity from daemon state. There is nothing else to sync: the
// dropdown list is built fresh on every open, so a profile created or deleted
// by another client needs no rebuild here — and nothing below the selector
// shifts when the profile set changes, which is why the custom focus list is
// built exactly once.
func (w *Window) syncProfileSelector() {
	if w.profileSelDD == nil {
		return
	}
	w.profileSelDD.setLabel(profileui.Label(w.editProfile))

	// Refusal reasons go to the shared block note, in the flow of the view —
	// never to tooltips, which are invisible in gamescope and unreachable on
	// a controller (the focus grid skips insensitive widgets).
	blocks := make([]string, 0, 2)
	if w.activateBtn != nil {
		block := profileui.ActivateBlock(w.state, w.editProfile)
		w.activateBtn.SetSensitive(block == "")
		if block != "" {
			blocks = append(blocks, "Activate: "+block)
		}
	}
	if w.saveAsBtn != nil {
		block := profileui.SaveAsBlock(w.state)
		w.saveAsBtn.SetSensitive(block == "")
		if block != "" {
			blocks = append(blocks, "Save As: "+block)
		}
	}
	setBlockNote(w.actionsNote, strings.Join(blocks, " · "))
}

// showNameEntry opens the inline name row prefilled with a suggested name.
// The prefill is not a convenience: a gamepad user cannot type into the entry
// at all, so the suggestion (always valid and free) is the name they get.
func (w *Window) showNameEntry(mode string) {
	w.nameMode = mode
	w.nameEntry.SetText(profileui.SuggestName(w.state))
	w.nameRow.SetVisible(true)
	w.nameEntry.GrabFocus()
}

// confirmNameEntry validates the typed name and sends the create or save-as.
// A bad name keeps the row open for correction; the daemon's own refusal is
// still the final word and lands in the error bar like any other failure.
func (w *Window) confirmNameEntry() {
	name := strings.TrimSpace(w.nameEntry.Text())
	mode := w.nameMode
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
	w.nameRow.SetVisible(false)
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
			w.setEditTarget(name)
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
