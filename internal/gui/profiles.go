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
	"time"

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

// buildProfileSection creates the main view's PROFILE section: the three
// firmware profiles on one row, and a single Custom button beneath them.
//
// The custom profiles are deliberately not listed here. One row per saved
// profile pushed the RGB and battery controls off the bottom of a 320px
// drawer, so the whole family collapses to one button that opens the custom
// view; the button is labelled with the running custom profile, so the main
// view still says what is in force.
func (w *Window) buildProfileSection() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("PROFILE"))

	stockRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	stockRow.AddCSSClass("btn-group")
	stockRow.SetHomogeneous(true)
	for _, r := range profileui.StockRows(nil) {
		r := r
		btn := gtk.NewButtonWithLabel(r.Label)
		btn.SetHExpand(true)
		btn.ConnectClicked(func() {
			// Optimistic highlight, as the stock buttons always had;
			// sendProfileSet refreshes state itself once the daemon has
			// applied the profile.
			setActiveButton(w.profileBtns, r.Name)
			w.sendProfileSet(r.Name)
		})
		w.profileBtns[r.Name] = btn
		stockRow.Append(btn)
	}
	box.Append(stockRow)

	// In a .btn-group of its own: the .active style that marks the running
	// profile is scoped to that class, so a bare button would never highlight.
	customRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	customRow.AddCSSClass("btn-group")
	w.customBtn = gtk.NewButtonWithLabel(profileui.Custom(nil).Label)
	w.customBtn.SetHExpand(true)
	w.customBtn.SetTooltipText("Custom profiles: power limits, fan curve and undervolt")
	w.customBtn.ConnectClicked(func() { w.showCustomView() })
	customRow.Append(w.customBtn)
	box.Append(customRow)

	return box
}

// syncProfiles updates the main view's profile controls from daemon state.
// The section never changes shape, so this only moves highlights and the
// Custom button's label.
//
// Callers must hold w.syncing (as syncState does).
func (w *Window) syncProfiles() {
	if w.state != nil && w.state.Profile != "" {
		setActiveButton(w.profileBtns, w.state.Profile)
	}
	if w.customBtn == nil {
		return
	}
	cs := profileui.Custom(w.state)
	w.customBtn.SetLabel(cs.Label)
	if cs.Active {
		w.customBtn.AddCSSClass("active")
	} else {
		w.customBtn.RemoveCSSClass("active")
	}
}

// buildProfileSelector creates the custom view's profile selector: a
// collapsed row naming the profile being edited, an expandable list of the
// custom profiles, and the create/copy/activate affordances.
//
// It expands in the flow of the view rather than opening a popup. GtkDropDown
// and popovers create their own windows, which gamescope does not composite —
// the drawer would be selecting a profile from an invisible list in Steam
// Gaming Mode. Expanding in place also means the rows are ordinary widgets
// that the existing focus grid navigates, with no second mechanism for a
// popup's contents.
func (w *Window) buildProfileSelector() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("PROFILE"))

	selRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	selRow.AddCSSClass("btn-group")
	w.profileSelBtn = gtk.NewButtonWithLabel("")
	w.profileSelBtn.AddCSSClass("profile-select")
	w.profileSelBtn.SetHExpand(true)
	w.profileSelBtn.SetTooltipText("Choose which custom profile to edit")
	w.profileSelBtn.ConnectClicked(func() { w.toggleProfileList() })
	selRow.Append(w.profileSelBtn)
	box.Append(selRow)

	w.profileSelBox = gtk.NewBox(gtk.OrientationVertical, 4)
	w.profileSelBox.AddCSSClass("btn-group")
	w.profileSelBox.SetVisible(false)
	box.Append(w.profileSelBox)

	actions := gtk.NewBox(gtk.OrientationHorizontal, 4)
	actions.AddCSSClass("btn-group")
	w.activateBtn = gtk.NewButtonWithLabel("Activate")
	w.activateBtn.AddCSSClass("save-btn")
	w.activateBtn.SetHExpand(true)
	w.activateBtn.ConnectClicked(func() { w.sendProfileSet(w.editProfile) })
	actions.Append(w.activateBtn)
	w.newProfileBtn = gtk.NewButtonWithLabel("+ New")
	w.newProfileBtn.SetHExpand(true)
	w.newProfileBtn.SetTooltipText("Create an empty named profile")
	w.newProfileBtn.ConnectClicked(func() { w.showNameEntry(nameModeCreate) })
	actions.Append(w.newProfileBtn)
	w.saveAsBtn = gtk.NewButtonWithLabel("Save As")
	w.saveAsBtn.SetHExpand(true)
	w.saveAsBtn.ConnectClicked(func() { w.showNameEntry(nameModeSaveAs) })
	actions.Append(w.saveAsBtn)
	box.Append(actions)

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
	w.nameCancelBtn.SetTooltipText("Cancel")
	w.nameCancelBtn.ConnectClicked(func() { w.nameRow.SetVisible(false) })
	w.nameRow.Append(w.nameCancelBtn)
	w.nameRow.SetVisible(false)
	box.Append(w.nameRow)

	w.rebuildProfileSelector(profileui.CustomRows(nil))
	return box
}

// toggleProfileList expands or collapses the selector's list of profiles.
func (w *Window) toggleProfileList() {
	w.profileExpanded = !w.profileExpanded
	w.profileSelBox.SetVisible(w.profileExpanded)
	w.syncProfileSelector()
}

// rebuildProfileSelector replaces the expandable list's buttons with one per
// custom profile. Called only when the row set changes — see
// syncProfileSelector.
func (w *Window) rebuildProfileSelector(rows []profileui.Row) {
	for c := w.profileSelBox.FirstChild(); c != nil; c = w.profileSelBox.FirstChild() {
		w.profileSelBox.Remove(c)
	}
	w.profileSelBtns = make(map[string]*gtk.Button, len(rows))
	for _, r := range rows {
		r := r
		btn := gtk.NewButtonWithLabel(r.Label)
		btn.SetHExpand(true)
		btn.ConnectClicked(func() {
			w.setEditTarget(r.Name)
			w.toggleProfileList() // selecting collapses the list again
		})
		w.profileSelBtns[r.Name] = btn
		w.profileSelBox.Append(btn)
	}
	w.customRows = rows
	w.customSig = profileui.Signature(rows)
}

// setEditTarget points the custom view at another profile and re-syncs every
// widget to it.
func (w *Window) setEditTarget(name string) {
	w.editProfile = name
	w.disarmDelete()
	w.syncCustomView()
}

// syncProfileSelector updates the selector from daemon state: rebuilds the
// list when the profile set changed (a profile created or deleted, possibly
// by another client) and otherwise updates labels and sensitivity in place.
func (w *Window) syncProfileSelector() {
	if w.profileSelBtn == nil {
		return
	}
	rows := profileui.CustomRows(w.state)
	if profileui.Signature(rows) != w.customSig {
		w.rebuildProfileSelector(rows)
		// The rows below the selector all shifted, so the whole custom focus
		// grid is rebuilt; adopt it now only if it is the grid in use.
		w.buildCustomFocusList()
		if w.viewStack != nil && w.viewStack.VisibleChildName() == "custom" {
			w.swapFocusList(w.customFocusItems)
		}
	} else {
		w.customRows = rows
	}

	arrow := "▾"
	if w.profileExpanded {
		arrow = "▴"
	}
	w.profileSelBtn.SetLabel(profileui.Label(w.editProfile) + "  " + arrow)
	setActiveButton(w.profileSelBtns, w.editProfile)

	for _, r := range rows {
		if btn := w.profileSelBtns[r.Name]; btn != nil {
			if r.Active {
				btn.AddCSSClass("active")
			}
		}
	}

	if w.activateBtn != nil {
		block := profileui.ActivateBlock(w.state, w.editProfile)
		w.activateBtn.SetSensitive(block == "")
		if block == "" {
			w.activateBtn.SetTooltipText("Apply this profile to the machine")
		} else {
			w.activateBtn.SetTooltipText("Unavailable: " + block)
		}
	}
	if w.saveAsBtn != nil {
		block := profileui.SaveAsBlock(w.state)
		w.saveAsBtn.SetSensitive(block == "")
		if block == "" {
			w.saveAsBtn.SetTooltipText("Copy the active profile's settings under a new name")
		} else {
			w.saveAsBtn.SetTooltipText("Unavailable: " + block)
		}
	}
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

// buildAutoswitchSection creates the AUTOSWITCH section: the enable switch and
// a cycle button per power source. Cycle buttons instead of a dropdown for the
// usual reason — a dropdown's popup is a separate window gamescope does not
// composite — and they read fine with a handful of profiles.
func (w *Window) buildAutoswitchSection() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)

	labelRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	labelRow.Append(sectionLabel("AUTOSWITCH"))
	sw := gtk.NewSwitch()
	sw.SetHAlign(gtk.AlignEnd)
	sw.SetHExpand(true)
	sw.SetTooltipText("Switch profiles automatically when the charger is plugged or unplugged")
	sw.ConnectStateSet(func(on bool) bool {
		if !w.syncing {
			w.autoswitchEnabled = on
			w.queueAutoswitchSend()
			w.syncAutoswitchVis()
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(sw, func() { sw.SetActive(!sw.Active()) })
	}
	w.autoswitchSwitch = sw
	labelRow.Append(sw)
	box.Append(labelRow)

	// The two target rows are shown only while autoswitch is enabled: they
	// are meaningless when it is off, and this is the main view, where three
	// permanent rows for a feature most users leave alone is exactly the
	// crowding the profile list was moved out to avoid.
	w.autoswitchTargets = gtk.NewBox(gtk.OrientationVertical, 4)
	w.autoswitchTargets.Append(w.buildAutoswitchTargetRow("On AC", &w.autoswitchAC, &w.autoswitchACBtn))
	w.autoswitchTargets.Append(w.buildAutoswitchTargetRow("On battery", &w.autoswitchBatt, &w.autoswitchBattBtn))
	w.autoswitchTargets.SetVisible(false)
	box.Append(w.autoswitchTargets)
	return box
}

// buildAutoswitchTargetRow creates one "label + cycle button" row. target and
// btnDst point at the Window fields for this side; both are only ever touched
// on the GTK thread.
func (w *Window) buildAutoswitchTargetRow(label string, target *string, btnDst **gtk.Button) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	name := gtk.NewLabel(label)
	name.AddCSSClass("scale-name")
	name.SetHAlign(gtk.AlignStart)
	// A size group keeps the two cycle buttons aligned: without it "On AC"
	// and "On battery" are different widths and the buttons start at
	// different offsets.
	name.SetSizeRequest(72, -1)
	row.Append(name)

	btn := gtk.NewButtonWithLabel(profileui.TargetLabel(*target))
	btn.SetHExpand(true)
	btn.SetTooltipText("Profile to apply when this power source becomes active — tap to cycle")
	btn.ConnectClicked(func() {
		opts := profileui.TargetOptions(w.state)
		*target = profileui.CycleTarget(opts, *target, +1)
		btn.SetLabel(profileui.TargetLabel(*target))
		w.queueAutoswitchSend()
	})
	*btnDst = btn
	row.Append(btn)
	return row
}

// syncAutoswitchVis shows the target rows only while autoswitch is enabled.
func (w *Window) syncAutoswitchVis() {
	if w.autoswitchTargets != nil {
		w.autoswitchTargets.SetVisible(w.autoswitchEnabled)
	}
}

// queueAutoswitchSend debounces the autoswitch config send. The debounce is
// not only about chattiness: cycling a target is several clicks in a row, and
// concurrent per-click goroutines could land on the daemon out of order,
// leaving an intermediate choice as the stored one. One timer, one snapshot of
// the fields on the GTK thread, one send.
func (w *Window) queueAutoswitchSend() {
	if w.autoswitchTimer != nil {
		w.autoswitchTimer.Stop()
	}
	w.autoswitchTimer = time.AfterFunc(300*time.Millisecond, func() {
		glib.IdleAdd(func() bool {
			enabled, ac, batt := w.autoswitchEnabled, w.autoswitchAC, w.autoswitchBatt
			go func() {
				if err := apiresult.Err(api.SendAutoswitchSet(enabled, ac, batt)); err != nil {
					w.reportError("Configure autoswitch", err)
					return
				}
				w.clearErrorAsync()
				slog.Debug("autoswitch sent", "enabled", enabled, "ac", ac, "battery", batt)
				w.refreshState()
			}()
			return false
		})
	})
}

// syncAutoswitch updates the autoswitch widgets and their mirror fields from
// daemon state. Callers must hold w.syncing: SetActive on the switch fires its
// state-set handler, which would otherwise send the value straight back.
func (w *Window) syncAutoswitch() {
	if w.autoswitchSwitch == nil {
		return
	}
	a := profileui.Autoswitch(w.state)
	w.autoswitchEnabled, w.autoswitchAC, w.autoswitchBatt = a.Enabled, a.AC, a.Battery
	w.autoswitchSwitch.SetActive(a.Enabled)
	w.autoswitchACBtn.SetLabel(profileui.TargetLabel(a.AC))
	w.autoswitchBattBtn.SetLabel(profileui.TargetLabel(a.Battery))
	w.syncAutoswitchVis()
}
