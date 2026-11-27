package cmd

// setup_inputplumber.go — setup's opt-in InputPlumber override
// (--inputplumber-passthrough): what to do is internal/drivers/inputplumber's
// plan; this file decides whether the user wants it, prints the plan, and
// restarts InputPlumber when a file changed (docs/decisions/0178).

import (
	"fmt"
	"os"
	"strings"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
)

// Seams for the tests: where InputPlumber's configs are, and the identity
// it matches them against.
var (
	ipUpstreamDir = inputplumber.UpstreamDir
	ipReadDMI     = inputplumber.ReadDMI
)

const ipService = "inputplumber.service"

// lightingSources is the hidraw interfaces this machine's lighting uses: the
// sources an override passes through. Nothing else is ever unhidden.
func lightingSources(cfg device.Config) []inputplumber.HIDRawSource {
	if cfg.Lighting == nil {
		return nil
	}
	var out []inputplumber.HIDRawSource
	for _, z := range cfg.Lighting.Zones {
		v, p, err := z.USBIDs()
		if err != nil || z.Interface == nil {
			continue
		}
		out = append(out, inputplumber.HIDRawSource{Vendor: v, Product: p, Interface: *z.Interface})
	}
	return out
}

// wantPassthrough is the user's choice: the flag when given, otherwise
// whatever an earlier run chose, which the overrides it wrote record. A
// package upgrade runs a plain "voltaire setup", so the choice must survive
// one.
func wantPassthrough() bool {
	if ipPassthroughSet {
		return ipPassthroughFlag
	}
	return len(inputplumber.Overrides()) > 0
}

// syncPassthrough brings the overrides in line with want and reports whether
// a file changed. restart restarts InputPlumber after a change so it reads
// it; the boot pass runs before InputPlumber and must not (a restart queued
// behind a unit ordered after this one would wait on it).
func syncPassthrough(cfg device.Config, want, restart bool) bool {
	srcs := lightingSources(cfg)
	if !want && len(inputplumber.Overrides()) == 0 {
		if ipPassthroughSet || permsOnlyFlag || setupRemoveFlag {
			return false
		}
		// Not opted in: say so only when it would make a difference.
		if hint, err := inputplumber.PlanOverrides(ipUpstreamDir(), srcs, ipReadDMI(), true); err == nil &&
			hint.Wanted() && len(hint.Foreign) == 0 {
			fmt.Printf("\nnote: InputPlumber hides this machine's lighting interface (%s), so voltaire cannot drive the lighting.\n"+
				"To have InputPlumber leave it visible: sudo voltaire setup --inputplumber-passthrough\n"+
				"(what that changes: https://dahui.github.io/voltaire/inputplumber#ally-lighting)\n", sourceNames(srcs))
		}
		return false
	}

	p, err := inputplumber.PlanOverrides(ipUpstreamDir(), srcs, ipReadDMI(), want)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: InputPlumber override: %v\n", err)
		return false
	}
	for _, f := range p.Failed {
		fmt.Fprintf(os.Stderr, "note: cannot add passthrough to %s; InputPlumber keeps its own config\n", f)
	}
	for _, f := range p.Foreign {
		fmt.Fprintf(os.Stderr, "note: %s was not written by voltaire setup; leaving it in place (it decides whether InputPlumber hides the lighting)\n", f)
	}
	if want && ipPassthroughSet && !p.Wanted() && len(p.Failed) == 0 {
		fmt.Println("InputPlumber does not hide this machine's lighting interface; no override needed.")
	}
	if dryRunFlag {
		for _, f := range p.Write {
			fmt.Printf("Would write %s/%s (InputPlumber's %s with passthrough on %s)\n",
				inputplumber.OverrideDir, f.Name, f.From, sourceNames(srcs))
		}
		for _, f := range p.Remove {
			fmt.Printf("Would remove %s\n", f)
		}
		if p.Changed() && restart {
			fmt.Printf("Would run: systemctl try-restart %s\n", ipService)
		}
		return p.Changed()
	}
	if err := p.Apply(); err != nil {
		fmt.Fprintf(os.Stderr, "note: InputPlumber override: %v\n", err)
	}
	for _, f := range p.Write {
		fmt.Printf("Wrote %s/%s from %s (passthrough on %s)\n", inputplumber.OverrideDir, f.Name, f.From, sourceNames(srcs))
	}
	for _, f := range p.Remove {
		fmt.Printf("Removed %s\n", f)
	}
	if p.Changed() && restart && note("systemctl", "try-restart", ipService) {
		fmt.Println("Restarted InputPlumber so it reads the change (the controller pauses for a moment).")
		fmt.Println("Restart voltaire for the lighting change to show: systemctl --user restart voltaire.service")
	}
	return p.Changed()
}

func sourceNames(srcs []inputplumber.HIDRawSource) string {
	names := make([]string, len(srcs))
	for i, s := range srcs {
		names[i] = s.String()
	}
	return strings.Join(names, ", ")
}
