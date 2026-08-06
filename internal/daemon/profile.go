package daemon

// profile.go — applying a profile, and the CRUD around named custom profiles.
//
// A profile is either one of the three firmware profiles written to
// platform_profile, or a custom profile: a named set of fan curve, TDP and
// Curve Optimizer settings that z13ctl applies itself and that never touches
// platform_profile. state.CustomProfiles is the source of truth for the latter;
// api.State's FanCurve/TDP/Undervolt fields are only a projection of the active
// one, filled in at the serialization boundaries (see withLegacyProjection).
//
// The firmware profile names are reserved end to end — validation refuses them,
// loadState drops a map entry carrying one, api.State.IsCustomProfile answers
// false for one, and applyProfileLocked tests for a stock name before it ever
// consults the map. Selecting "balanced" therefore always reaches the firmware
// profile, with no way for a custom profile to shadow it.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

// applyProfileLocked applies a profile — firmware or custom — to hardware and
// updates daemon state to match. It is the single implementation behind both
// the "profile" socket command and the AC/battery autoswitch watcher.
//
// The caller must hold d.hwMu and must NOT hold d.mu. This function takes d.mu
// itself, and the lock order in this package is hwMu then d.mu, always. For the
// same reason nothing reached from here may call d.effectiveProfile(), which
// takes d.mu.
func (d *Daemon) applyProfileLocked(profile string) error {
	// Stock first, before the map is consulted at all. This is the last of the
	// four layers that keep a custom profile from shadowing a firmware one, and
	// the only one that still holds if a hand-edited state file gets past the
	// other three.
	if cli.IsStockProfile(profile) {
		return d.applyStockHW(profile)
	}

	d.mu.Lock()
	if !d.state.IsCustomProfile(profile) {
		d.mu.Unlock()
		return fmt.Errorf("unknown profile %q: not a firmware profile and not saved", profile)
	}
	p := d.state.CustomProfiles[profile]
	p.Name = profile
	if p.Empty() {
		d.mu.Unlock()
		return fmt.Errorf("profile %q has no settings saved; set a fan curve, TDP, or undervolt first", profile)
	}
	// Set the active profile before the hardware work, not after. That is what
	// makes the reconcile watcher start defending the fans *during* the apply,
	// and it lets a half-failed apply self-heal on the next reconcile tick
	// rather than leaving the daemon claiming a stock profile while a custom
	// curve is live. Deciding custom-ness and snapshotting in one critical
	// section also closes the window where a concurrent profile-delete could
	// remove the entry between the two.
	d.state.Profile = profile
	p = cloneCustomProfile(p)
	d.mu.Unlock()

	d.applyCustomHW(p)
	return nil
}

// applyCustomHW puts hardware into the state a custom profile describes.
//
// A subsystem the profile leaves nil is *cleared*, not left alone. That is what
// makes switching between two custom profiles predictable: without it, going
// from a profile with a 90 W limit and a -25 offset to one that sets neither
// leaves both in force while the daemon reports the second profile. Selecting
// A then B then A again would not give the same machine as selecting A.
//
// Ordering is the fail-closed part and is not free to rearrange:
//
//   - The profile's own curve goes on *before* its TDP, so ApplyTDPSafely's
//     high-TDP floor is written last and wins if the two disagree.
//   - Clearing the TDP hands the limits back to the firmware profile underneath,
//     which lowers power before the fans are touched.
//   - The fans are released only when no high sustained limit is in force. A
//     profile with a high TDP and no curve of its own keeps the floor
//     ApplyTDPSafely just wrote.
//
// Individual failures are logged rather than returned: a profile that only
// partly applies is still the profile the user asked for, and the reconcile
// watcher keeps trying on the parts it owns.
func (d *Daemon) applyCustomHW(p api.CustomProfile) {
	hasCurve := p.FanCurve != nil && p.FanCurve.Mode == 1 && len(p.FanCurve.Points) == 8
	if hasCurve {
		if err := cli.SetBothFanCurves(p.FanCurve.Points); err != nil {
			slog.Warn("failed to apply fan curve", "profile", p.Name, "err", err)
		}
	}

	highTDP := false
	if t := p.TDP; t != nil {
		if err := cli.ApplyTDPSafely(*t); err != nil {
			slog.Warn("failed to apply TDP", "profile", p.Name, "err", err)
		} else {
			highTDP = t.PL1SPL > cli.TDPMaxSafe
		}
	} else {
		// Not controlled here, so hand the limits back to the firmware profile
		// underneath rather than leaving the previous profile's watts in force.
		restoreStockPPT(readProfileFromSysfs())
	}

	if !hasCurve && !highTDP {
		if err := cli.ResetAllFanCurves(); err != nil {
			slog.Warn("failed to release fans to firmware auto", "profile", p.Name, "err", err)
		}
	}

	uvActive := false
	if cli.SMUProbeUndervolt() {
		if uv := p.Undervolt; uv != nil {
			if err := cli.SetCurveOptimizer(uv.CPUCO); err != nil {
				slog.Warn("failed to apply undervolt", "profile", p.Name, "err", err)
			} else {
				uvActive = true
			}
		} else if err := cli.ResetCurveOptimizer(); err != nil {
			slog.Warn("failed to reset undervolt", "profile", p.Name, "err", err)
		}
	}

	d.mu.Lock()
	// Active describes hardware, so it is set here and nowhere else — and only
	// when the SMU write actually succeeded.
	setUndervoltActive(d.state, false)
	if uvActive {
		if cur, ok := d.state.CustomProfiles[p.Name]; ok && cur.Undervolt != nil {
			cur.Undervolt.Active = true
		}
	}
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
}

// applyStockHW switches to a firmware profile.
//
// The order below is deliberate and load-bearing. Reset the undervolt first
// (every route to a stock profile must clear it, or a custom setting leaks into
// a stock profile). Write platform_profile next; a failure there aborts with
// state untouched. Restore that profile's stock PPT — the firmware does not
// re-apply per-profile limits on a profile change, so without this a custom TDP
// persists across the switch. Release the fans to firmware auto *last*, so they
// are never dropped to auto while a high custom TDP is still in force.
func (d *Daemon) applyStockHW(profile string) error {
	if cli.SMUProbeUndervolt() {
		if err := cli.ResetCurveOptimizer(); err != nil {
			slog.Warn("failed to reset undervolt", "err", err)
		}
	}
	if err := cli.SetProfile(profile); err != nil {
		return err
	}
	restoreStockPPT(profile)
	if err := cli.ResetAllFanCurves(); err != nil {
		slog.Warn("failed to reset fan curves to auto", "err", err)
	}

	d.mu.Lock()
	d.state.Profile = profile
	setUndervoltActive(d.state, false)
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
	return nil
}

// setUndervoltActive stamps the Active flag across every saved profile. Curve
// Optimizer is global hardware state, so at most one profile's offset can be
// applied; clearing all of them is both correct and immune to the active
// profile having changed underneath. Caller must hold d.mu.
func setUndervoltActive(s api.State, active bool) {
	for _, p := range s.CustomProfiles {
		if p.Undervolt != nil {
			p.Undervolt.Active = active
		}
	}
}

// editTarget names the custom profile a fancurve/tdp/undervolt command edits,
// and says whether that profile is the active one — which is what decides
// whether the command also writes hardware.
type editTarget struct {
	Name    string
	Live    bool
	Profile api.CustomProfile // the profile as it stands before the edit
}

// resolveEditTarget works out which profile a setting command edits.
//
// An empty name means the active profile, creating and activating the default
// "custom" profile when a firmware profile is active — which is exactly how
// z13ctl behaved before named profiles existed, so no existing invocation
// changes meaning. A name selects that profile without activating it, which is
// what makes it possible to build the battery profile while running on AC.
//
// Caller must hold d.mu.
func (d *Daemon) resolveEditTargetLocked(name string) (editTarget, error) {
	// A lookup, so fold case: profile names are always stored lowercase.
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		// The profile currently running. When a firmware profile is active that
		// is the default custom profile, created and activated by this very
		// edit, so the target is live either way.
		name = d.state.Profile
		if !d.state.IsCustomProfile(name) {
			name = api.DefaultCustomProfile
		}
		return d.editTargetLocked(name, true), nil
	}
	if cli.IsStockProfile(name) {
		return editTarget{}, fmt.Errorf("%q is a firmware profile and has no custom settings to edit", name)
	}
	if name != api.DefaultCustomProfile && !d.state.IsCustomProfile(name) {
		return editTarget{}, fmt.Errorf("unknown profile %q; create it with 'z13ctl profile --create %s'", name, name)
	}
	return d.editTargetLocked(name, name == d.state.Profile), nil
}

// editTargetLocked builds the target for a known profile name. Caller must hold
// d.mu.
func (d *Daemon) editTargetLocked(name string, live bool) editTarget {
	p := d.state.CustomProfiles[name]
	p.Name = name
	return editTarget{Name: name, Live: live, Profile: cloneCustomProfile(p)}
}

// commitEditLocked writes an edited profile back and, when it is the live one,
// makes it the active profile. Caller must hold d.mu; returns the snapshot to
// hand to saveState after unlocking.
func (d *Daemon) commitEditLocked(t editTarget, p api.CustomProfile) api.State {
	p.Name = t.Name
	if d.state.CustomProfiles == nil {
		d.state.CustomProfiles = make(map[string]api.CustomProfile, 1)
	}
	d.state.CustomProfiles[t.Name] = p
	if t.Live {
		d.state.Profile = t.Name
	}
	return cloneState(d.state)
}

// activeTDPFor returns the sustained limit a curve edit must clear.
//
// For the live profile that is whatever hardware reports, which is ground truth
// and can disagree with saved state. For any other profile hardware says
// nothing, so the limit to check against is the one stored in that same
// profile — which is what stops a profile being saved in a state that would be
// unsafe the moment it is activated.
func (d *Daemon) floorLimitFor(t editTarget) int {
	if t.Live {
		tdp, err := cli.ReadEffectivePPT(t.Name)
		if err != nil {
			// A PPT read failure is deliberately not a refusal: the guard is
			// best-effort and must not make fan control unavailable.
			return 0
		}
		return tdp.PL1SPL
	}
	if t.Profile.TDP == nil {
		return 0
	}
	return t.Profile.TDP.PL1SPL
}

func (d *Daemon) handleProfile(req request) response {
	if req.Set == "" {
		return response{OK: false, Error: "profile requires a set field"}
	}
	profile := strings.ToLower(req.Set)

	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	if err := d.applyProfileLocked(profile); err != nil {
		return response{OK: false, Error: "profile: " + err.Error()}
	}
	slog.Info("profile", "set", profile)
	return response{OK: true}
}

// handleProfileCreate adds an empty custom profile without activating it.
//
// The name is validated as typed, not case-folded first: silently creating
// "gaming" from "Gaming" leaves the user looking for a profile under a name
// that is not there. Lookups are lenient in the other direction — selecting or
// editing a profile does fold case.
func (d *Daemon) handleProfileCreate(req request) response {
	name := strings.TrimSpace(req.Set)
	if err := cli.ValidateProfileName(name); err != nil {
		return response{OK: false, Error: "profile-create: " + err.Error()}
	}
	// hwMu even though this writes no hardware: the fancurve/tdp/undervolt
	// handlers resolve a target under d.mu, release it for the hardware write,
	// and commit the profile back under d.mu again. Mutating the profile map
	// inside that window would be silently undone by the commit — or, for a
	// delete, undone by resurrecting the profile. hwMu is what makes the whole
	// resolve-write-commit span exclusive.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	if _, exists := d.state.CustomProfiles[name]; exists {
		d.mu.Unlock()
		return response{OK: false, Error: "profile-create: profile " + name + " already exists"}
	}
	if d.state.CustomProfiles == nil {
		d.state.CustomProfiles = make(map[string]api.CustomProfile, 1)
	}
	d.state.CustomProfiles[name] = api.CustomProfile{Name: name}
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
	slog.Info("profile-create", "profile", name)
	return response{OK: true}
}

// handleProfileSave copies the active custom profile under a new name. It does
// not activate the copy: saving is a bookmark, not a switch.
func (d *Daemon) handleProfileSave(req request) response {
	name := strings.TrimSpace(req.Set)
	if err := cli.ValidateProfileName(name); err != nil {
		return response{OK: false, Error: "profile-save: " + err.Error()}
	}
	// hwMu for the same reason as handleProfileCreate.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	src, ok := d.state.ActiveCustomProfile()
	if !ok {
		d.mu.Unlock()
		return response{OK: false, Error: "profile-save: no custom profile is active; there is nothing to copy"}
	}
	if src.Empty() {
		d.mu.Unlock()
		return response{OK: false, Error: "profile-save: the active profile has no settings to copy"}
	}
	p := cloneCustomProfile(src)
	p.Name = name
	if p.Undervolt != nil {
		// Active describes hardware, and the copy is not the profile running.
		p.Undervolt.Active = false
	}
	if d.state.CustomProfiles == nil {
		d.state.CustomProfiles = make(map[string]api.CustomProfile, 1)
	}
	d.state.CustomProfiles[name] = p
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
	slog.Info("profile-save", "profile", name, "from", src.Name)
	return response{OK: true}
}

// handleProfileDelete removes a saved custom profile.
//
// It refuses to delete the active profile or one referenced by autoswitch.
// Deleting the active one would leave state.Profile naming a profile that no
// longer exists, which makes IsCustomProfile false and silently stops the
// reconcile watcher defending a live custom fan curve — a thermal regression
// with no log line. Both refusals are trivially reversible: switch away, or
// reconfigure autoswitch, then delete.
func (d *Daemon) handleProfileDelete(req request) response {
	name := strings.ToLower(strings.TrimSpace(req.Set))
	if name == "" {
		return response{OK: false, Error: "profile-delete requires a set field"}
	}
	// hwMu for the same reason as handleProfileCreate. It also keeps a delete
	// from landing part-way through applyProfileLocked, which would leave
	// state.Profile naming a profile that no longer exists — and so stop the
	// reconcile watcher defending a fan curve that is live in hardware.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	if refusal := d.deleteRefusalLocked(name); refusal != "" {
		d.mu.Unlock()
		return response{OK: false, Error: "profile-delete: " + refusal}
	}
	delete(d.state.CustomProfiles, name)
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
	slog.Info("profile-delete", "profile", name)
	return response{OK: true}
}

// deleteRefusalLocked returns the reason a profile may not be deleted, or "".
// Caller must hold d.mu.
func (d *Daemon) deleteRefusalLocked(name string) string {
	if _, ok := d.state.CustomProfiles[name]; !ok {
		return "no saved profile named " + name
	}
	if d.state.Profile == name {
		return name + " is the active profile; switch to another profile first"
	}
	if a := d.state.Autoswitch; a != nil {
		if a.AC == name {
			return name + " is the autoswitch AC profile; change it first"
		}
		if a.Battery == name {
			return name + " is the autoswitch battery profile; change it first"
		}
	}
	return ""
}

// profileSummary is one row of profile-list. It reports which profile is active
// so a client does not have to correlate against get-state.
type profileSummary struct {
	api.CustomProfile
	Active bool `json:"active"`
}

func (d *Daemon) handleProfileList() response {
	d.mu.Lock()
	s := cloneState(d.state)
	d.mu.Unlock()

	names := make([]string, 0, len(s.CustomProfiles)+1)
	for name := range s.CustomProfiles {
		names = append(names, name)
	}
	// "custom" is always addressable even before it has ever been populated,
	// so list it whether or not it has an entry.
	if _, ok := s.CustomProfiles[api.DefaultCustomProfile]; !ok {
		names = append(names, api.DefaultCustomProfile)
	}
	slices.Sort(names)

	out := make([]profileSummary, 0, len(names))
	for _, name := range names {
		p := s.CustomProfiles[name]
		p.Name = name
		out = append(out, profileSummary{CustomProfile: p, Active: s.Profile == name})
	}
	data, _ := json.Marshal(out)
	return response{OK: true, Value: string(data)}
}
