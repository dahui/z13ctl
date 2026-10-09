package cmd

// tdp.go — "tdp" subcommand: read or set TDP power limits via the Linux
// kernel PPT attributes (asus-armoury, or asus-nb-wmi as a fallback). No HID
// access required.

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
	"github.com/dahui/z13ctl/internal/daemon"

	"github.com/spf13/cobra"
)

var (
	tdpGetFlag     bool
	tdpSetFlag     string
	tdpResetFlag   bool
	tdpPL1Flag     string
	tdpPL2Flag     string
	tdpPL3Flag     string
	tdpForceFlag   bool
	tdpProfileFlag string
)

var tdpCmd = &cobra.Command{
	Use:   "tdp",
	Short: "Get or set TDP power limits (PPT)",
	Long: `Get or set TDP power limits through the kernel's asus-armoury
firmware-attributes, or the older asus-nb-wmi PPT attributes on kernels that do
not expose them there.

With --get, prints the current PPT (Package Power Tracking) values and the
range the kernel accepts for each.

With --set, writes power limits in watts. By default, all PPT values are set to
the same value. Use --pl1, --pl2, --pl3 to override individual limits.

Ranges come from the kernel. On the ROG Flow Z13 (GZ302EA) asus-armoury accepts
PL1 28–80W, PL2 32–92W and PL3 45–93W; a PL2 or PL3 below its minimum is raised
to it (so --set 30 writes 30/32/45W), and a PL1 outside its range is refused.
The older interface accepts 5–93W.

Safety: The sustained power limit (PL1) is capped at 75W by default. Use --force
to allow PL1 up to the kernel's maximum. When PL1 exceeds 75W, both fans are
held to a curve with a 50% PWM floor that reaches 100% at 80°C, written before
the power limit; if that write fails, or the kernel does not honour it, the TDP
is not applied at all. Burst limits (PL2/PL3) need no --force since short
bursts are thermally safe.

Run the daemon to keep a custom TDP in force. Any system power profile change
(GNOME power modes, power-profiles-daemon on plugging or unplugging the charger,
Fn+F5) makes the firmware re-apply that profile's own limits and releases custom
fan curves; the daemon notices and restores both. Without it, the limit and the
fan floor are lost until the next 'tdp --set'.

With --reset, switches to the balanced profile, writes balanced's stock PPT
values (bringing a high custom limit down first), and then resets fan curves to
auto mode, which puts the firmware's own balanced limits back in force.

PPT attributes:
  PL1/SPL          — Sustained Power Limit: the continuous power budget the APU
                     can draw indefinitely. This is your effective base TDP.
  PL2/sPPT         — Short-term boost: the APU can draw this much power for
                     several seconds before throttling back to PL1.
  PL3/fPPT         — Fast boost: the maximum instantaneous power the APU can
                     draw for millisecond-scale spikes (e.g. launching an app).
  APU sPPT         — APU-specific short-term limit (automatically set to PL2;
                     not exposed by asus-armoury on the GZ302EA).
  Platform sPPT    — Platform-level short-term limit (as APU sPPT).

When using --set, all three limits are set to the same value by default. Use
--pl1, --pl2, and --pl3 to set them independently — for example, --set 45
--pl2 55 --pl3 65 allows short bursts up to 65W while sustaining 45W.

Setting a TDP edits the custom profile you are running, creating and
activating "custom" if a firmware profile is active. Switching back to a
firmware profile hands the power limits back to the firmware while keeping
every custom profile saved, so they stay re-selectable.

Use --profile <name> to store limits in a profile you are NOT running: nothing
is written to hardware, which is how you build the profile 'z13ctl autoswitch'
selects on battery.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !tdpGetFlag && tdpSetFlag == "" && !tdpResetFlag {
			return cmd.Help()
		}

		if tdpSetFlag != "" {
			return runTdpSet()
		}
		if tdpResetFlag {
			return runTdpReset()
		}
		return runTdpGet()
	},
}

func runTdpGet() error {
	// The daemon when it is running: it refuses the read while the EC is not
	// answering (on asus-armoury each limit read is a live ACPI call), where a
	// read from here would go straight into the stalled EC.
	var tdp api.TDPState
	handled, value, err := api.SendTdpGet()
	switch {
	case handled && err != nil:
		return err
	case handled:
		if err = json.Unmarshal([]byte(value), &tdp); err != nil {
			return fmt.Errorf("reading TDP: %w", err)
		}
	default:
		if tdp, err = cli.ReadEffectivePPT(effectiveProfileForTDP()); err != nil {
			return fmt.Errorf("reading TDP: %w", err)
		}
	}

	fmt.Println("TDP Power Limits (watts):")
	fmt.Printf("  PL1 (SPL):          %d\n", tdp.PL1SPL)
	fmt.Printf("  PL2 (sPPT):         %d\n", tdp.PL2SPPT)
	fmt.Printf("  PL3 (fPPT):         %d\n", tdp.FPPT)
	// 0 means the interface does not expose it (asus-armoury on the GZ302EA).
	if tdp.APUSPPT != 0 || tdp.PlatformSPPT != 0 {
		fmt.Printf("  APU sPPT:           %d\n", tdp.APUSPPT)
		fmt.Printf("  Platform sPPT:      %d\n", tdp.PlatformSPPT)
	}
	if lim, ok := pptLimitsFor(); ok {
		fmt.Printf("  Interface:          %s (PL1 %d–%dW, PL2 %d–%dW, PL3 %d–%dW)\n", lim.Backend,
			lim.PL1.Min, lim.PL1.Max, lim.PL2.Min, lim.PL2.Max, lim.PL3.Min, lim.PL3.Max)
	}
	return nil
}

func readCurrentProfile() string {
	data, err := os.ReadFile(cli.FindProfilePath())
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// effectiveProfileForTDP returns the profile name to use when interpreting PPT
// values. It prefers the daemon's own profile because "custom" is a virtual
// profile that is deliberately never written to platform_profile — so sysfs
// alone cannot tell a legitimate 5W custom TDP from the kernel's stale 5W cache,
// and cli.ReadEffectivePPT would substitute the stock table for real values.
// Falls back to platform_profile when the daemon is not running.
func effectiveProfileForTDP() string {
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil && st.Profile != "" {
		return st.Profile
	}
	return readCurrentProfile()
}

func runTdpSet() error {
	watts, err := strconv.Atoi(tdpSetFlag)
	if err != nil {
		return fmt.Errorf("invalid TDP value %q: must be an integer", tdpSetFlag)
	}

	pl1, pl2, pl3, err := parsePLOverrides(watts)
	if err != nil {
		return err
	}

	// The kernel's limits, not constants: asus-armoury reports its own (PL1
	// 28–80 W on the GZ302EA). PL1 above the safe maximum needs --force; PL2/PL3
	// below their minimum are raised to it, which notes reports. The daemon runs
	// the same check on what it receives.
	lim, ok := pptLimitsFor()
	if !ok {
		lim = cli.LegacyPPTLimits()
	}
	tdp, notes, err := cli.ResolveTDPWith(lim, watts, pl1, pl2, pl3, tdpForceFlag)
	if err != nil {
		return err
	}

	if dryRunFlag {
		if tdpProfileFlag != "" {
			cli.DryRunProfileEdit(tdpProfileFlag, "power limits")
			return nil
		}
		cli.DryRunTdp(tdp.PL1SPL, tdp.PL1SPL, tdp.PL2SPPT, tdp.FPPT, tdpForceFlag, cli.LiveFanCurve(),
			cli.PlanTDPWrites(tdp))
		printNotes(notes)
		return nil
	}

	// The daemon applies the fan floor itself (cli.ApplyTDPSafely), so hand the
	// whole operation over before touching hardware here.
	if err := ensureProfileTargetSupported(tdpProfileFlag); err != nil {
		return err
	}

	// Sampled *before* the send. Asking after it is meaningless: the daemon has
	// already written the clamped curve, so the live curve then satisfies the floor
	// by construction and FloorAdjustsCurve is always false — except when the read
	// fails and nil makes it spuriously true, which is exactly backwards.
	preCurve := cli.LiveFanCurve()

	if handled, err := api.SendTdpSetFor(tdpProfileFlag, tdpSetFlag, tdpPL1Flag, tdpPL2Flag, tdpPL3Flag, tdpForceFlag); handled {
		if err != nil {
			return err
		}
		if tdpProfileFlag != "" {
			fmt.Print(profileEditMessage(tdpProfileFlag, ""))
			printNotes(notes)
			return nil
		}
		printFloorNotice(pl1, preCurve, true)
		fmt.Printf("TDP set to %dW\n", watts)
		printNotes(notes)
		return nil
	}

	if err := requireDaemonForProfile(tdpProfileFlag); err != nil {
		return err
	}
	// Direct path: same helper, so the no-daemon path enforces the fan floor on
	// the same terms — fans first, and no TDP at all if that write fails.
	//
	// LiveFanCurve is what this path has instead of profile state. Without it the
	// floor would replace a curve the user set moments earlier even when that curve
	// is well above it. preCurve was sampled before the socket attempt, which never
	// wrote anything on this branch, so it is still current.
	want := preCurve
	if err := cli.ApplyTDPSafely(tdp, want); err != nil {
		return fmt.Errorf("setting TDP: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	printFloorNotice(pl1, want, false)
	// Not only above the safe maximum: any profile change discards a custom
	// limit of any size (issue #22), and with no daemon nothing puts it back.
	fmt.Println("  Note: a system power profile change (GNOME power modes, power-profiles-daemon")
	fmt.Println("  on plugging or unplugging the charger, Fn+F5) makes the firmware re-apply its")
	fmt.Println("  own limits and releases custom fan curves, and the z13ctl daemon is not")
	fmt.Println("  running to restore them. Start it (see 'z13ctl daemon') to keep this limit.")
	fmt.Printf("TDP set to %dW\n", watts)
	printNotes(notes)
	return nil
}

// pptLimitsFor returns the limits the kernel accepts. With a daemon running
// they come from it (get-state's tdp_limits): on asus-armoury a read of the
// bounds is a live ACPI call, and the daemon leaves them out rather than make
// one while the EC is not answering — the CLI reading them itself would walk
// into the stalled EC the daemon is avoiding. ok is false when they are not to
// be had: the daemon withheld them, or no interface exists. With no daemon
// there is no latch to honour, and the kernel is asked.
func pptLimitsFor() (api.TDPLimits, bool) {
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil {
		if st.TDPLimits == nil {
			return api.TDPLimits{}, false
		}
		return *st.TDPLimits, true
	}
	lim, err := cli.PPTLimits()
	return lim, err == nil
}

// printNotes prints what ResolveTDP changed about the request, one per line.
func printNotes(notes []string) {
	for _, n := range notes {
		fmt.Printf("  %s\n", n)
	}
}

// printFloorNotice explains what the high-TDP floor did to the fan curve, if
// anything. want is the curve that was in force before the change.
//
// The three outcomes are genuinely different and were previously collapsed into
// two: with no custom curve at all the whole built-in floor curve is written, and
// saying "points below 127 PWM were raised; every other point is unchanged" there
// described points the user never set. DryRunTdp already distinguished the case.
func printFloorNotice(pl1 int, want []api.FanCurvePoint, viaDaemon bool) {
	if pl1 <= cli.TDPMaxSafe {
		return
	}
	switch {
	case len(want) == 0:
		fmt.Printf("Fans set to the built-in high-TDP curve: a %d PWM (50%%) floor rising to 100%% at 80°C\n",
			cli.HighTDPMinPWM)
		fmt.Println("  (no custom fan curve was in force to keep)")
	case cli.FloorAdjustsCurve(pl1, want):
		fmt.Println("Fan curve points below the built-in high-TDP curve were raised to it; every")
		fmt.Printf("  other point is unchanged. The floor rises with temperature — %d PWM (50%%) when\n", cli.HighTDPMinPWM)
		fmt.Println("  cool, 255 (100%) at 80°C — so a point can be raised even well above 50%")
	default:
		fmt.Println("Your fan curve already clears the high-TDP floor and was kept exactly as drawn")
	}
	if viaDaemon {
		fmt.Println("  (the daemon keeps the floor in force if a power profile change releases it)")
	}
}

func runTdpReset() error {
	if dryRunFlag {
		if tdpProfileFlag != "" {
			cli.DryRunProfileEdit(tdpProfileFlag, "cleared power limits")
			return nil
		}
		cli.DryRunTdpReset()
		return nil
	}

	if err := ensureProfileTargetSupported(tdpProfileFlag); err != nil {
		return err
	}
	if handled, err := api.SendTdpResetFor(tdpProfileFlag); handled {
		if err != nil {
			return err
		}
		if tdpProfileFlag != "" {
			fmt.Printf("Cleared the power limits from profile %s\n", tdpProfileFlag)
			return nil
		}
		fmt.Println("TDP reset: switched to balanced profile (firmware power limits restored)")
		return nil
	}

	if err := requireDaemonForProfile(tdpProfileFlag); err != nil {
		return err
	}
	// Direct path (no daemon): switch to balanced, then HandBackToFirmware —
	// the stock row first, so a high custom TDP is down before the fans drop to
	// auto, and the release last, so balanced's own limits end in force.
	// Reset the undervolt as well: this lands on a stock profile, and every
	// other route to one clears CO. Guarded on SMUAvailable (a stat, never the
	// destructive probe) so machines without ryzen_smu do not get a spurious
	// warning, and on daemon.UndervoltApplied so an offset that was never
	// applied is never "cleared" — a speculative MP1 write is the one with a
	// known hard-hang mode (see Daemon.uvApplied).
	if cli.SMUAvailable() && daemon.UndervoltApplied() {
		if err := cli.ResetCurveOptimizer(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to reset undervolt: %v\n", err)
		}
	}
	if err := cli.SetProfile("balanced"); err != nil {
		return fmt.Errorf("switching to balanced profile: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	if err := cli.HandBackToFirmware("balanced"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to restore balanced's power limits: %v\n", err)
	}
	fmt.Println("TDP reset: switched to balanced profile")
	return nil
}

// parsePLOverrides returns the effective PL1/PL2/PL3 values, applying
// per-PL flag overrides when set. Non-zero overrides replace the unified watts value.
func parsePLOverrides(watts int) (pl1, pl2, pl3 int, err error) {
	pl1, pl2, pl3 = watts, watts, watts
	if tdpPL1Flag != "" {
		pl1, err = strconv.Atoi(tdpPL1Flag)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid --pl1 value %q: must be an integer", tdpPL1Flag)
		}
	}
	if tdpPL2Flag != "" {
		pl2, err = strconv.Atoi(tdpPL2Flag)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid --pl2 value %q: must be an integer", tdpPL2Flag)
		}
	}
	if tdpPL3Flag != "" {
		pl3, err = strconv.Atoi(tdpPL3Flag)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid --pl3 value %q: must be an integer", tdpPL3Flag)
		}
	}
	return pl1, pl2, pl3, nil
}

func init() {
	tdpCmd.Flags().BoolVar(&tdpGetFlag, "get", false, "Print current TDP power limits")
	tdpCmd.Flags().StringVar(&tdpSetFlag, "set", "", "Set TDP power limit in watts")
	tdpCmd.Flags().BoolVar(&tdpResetFlag, "reset", false, "Reset to balanced profile and hand the power limits back to the firmware")
	tdpCmd.Flags().StringVar(&tdpPL1Flag, "pl1", "", "Override PL1/SPL (watts)")
	tdpCmd.Flags().StringVar(&tdpPL2Flag, "pl2", "", "Override PL2/sPPT (watts)")
	tdpCmd.Flags().StringVar(&tdpPL3Flag, "pl3", "", "Override PL3/fPPT (watts)")
	tdpCmd.Flags().BoolVar(&tdpForceFlag, "force", false, "Allow sustained TDP (PL1) above 75W (up to the kernel's maximum)")
	tdpCmd.Flags().StringVar(&tdpProfileFlag, "profile", "", profileFlagUsage)
	rootCmd.AddCommand(tdpCmd)
}
