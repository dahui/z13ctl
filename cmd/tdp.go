package cmd

// tdp.go — "tdp" subcommand: read or set TDP power limits through the device's
// power limiter (asus-armoury or asus-nb-wmi PPT on the Z13). No HID access required.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/daemon"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/asusz13"
	"github.com/dahui/voltaire/v2/internal/safety"

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
	Long: `Get or set TDP power limits. On the ROG Flow Z13 they go through the
kernel's asus-armoury firmware-attributes, or the older asus-nb-wmi PPT
attributes on kernels that do not expose them there.

With --get, prints the current PPT (Package Power Tracking) values and the
range the kernel accepts for each.

With --set, writes power limits in watts. By default, all PPT values are set to
the same value. Use --pl1, --pl2, --pl3 to override individual limits.

Ranges come from the kernel where it reports them (asus-armoury does), and from
the device data where it does not; --get prints the ranges in force. A PL2 or
PL3 below its minimum is raised to it, with a note saying so, and a PL1 outside
its range is refused.

Safety: the sustained power limit (PL1) is capped at the device's safe maximum,
which --get also prints. Use --force to allow PL1 up to the kernel's maximum.
Above the safe maximum the fans are held to the device's high-TDP floor curve,
written before the power limit; if that write fails, or the kernel does not
honour it, the TDP is not applied at all. Burst limits (PL2/PL3) need no --force
since short bursts are thermally safe.

Run the daemon to keep a custom TDP in force. Any system power profile change
(GNOME power modes, power-profiles-daemon on plugging or unplugging the charger,
Fn+F5) or fan release by another tool makes the firmware re-apply the profile's
own limits, and a profile change also releases custom fan curves; the daemon
notices and restores both. Without it, the limit and the fan floor are lost
until the next 'tdp --set'.

With --reset, switches to the device's default firmware profile (balanced on
the Z13), writes that profile's stock PPT values (bringing a high custom limit
down first), and then resets fan curves to auto mode, which puts the firmware's
own limits back in force.

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
is written to hardware, which is how you build the profile 'voltaire autoswitch'
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
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Power == nil {
		return fmt.Errorf("no power limit control on this device")
	}
	// The daemon when it is running: it refuses the read while the EC is not
	// answering (on asus-armoury each limit read is a live ACPI call), where a
	// read from here would go straight into the stalled EC.
	var tdp api.TDPState
	handled, value, sendErr := api.SendTdpGet()
	switch {
	case handled && sendErr != nil:
		return sendErr
	case handled:
		if err = json.Unmarshal([]byte(value), &tdp); err != nil {
			return fmt.Errorf("reading TDP: %w", err)
		}
	default:
		if tdp, err = hw.Power.ReadEffective(effectiveProfileForTDP(hw)); err != nil {
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
	env := powerEnvFor(hw)
	if env.Interface != "" {
		pl2, pl3 := safety.PL2Range(env), safety.PL3Range(env)
		fmt.Printf("  Interface:          %s (PL1 %d–%dW, PL2 %d–%dW, PL3 %d–%dW)\n", env.Interface,
			env.TDPMin, env.TDPMaxForced, pl2.Min, pl2.Max, pl3.Min, pl3.Max)
	}
	// The help text cannot know this machine's numbers, so it points here.
	if env.TDPMaxSafe > 0 {
		fmt.Printf("  Safe maximum:       %dW sustained (above it needs --force and a fan floor)\n", env.TDPMaxSafe)
	}
	return nil
}

// readCurrentProfile reads the firmware profile from hardware, or "unknown"
// when the device has no profile control or the read fails.
func readCurrentProfile(hw *device.Device) string {
	if hw.Profiles == nil {
		return "unknown"
	}
	p, err := hw.Profiles.Get()
	if err != nil {
		return "unknown"
	}
	return p
}

// effectiveProfileForTDP returns the profile name to use when interpreting PPT
// values. It prefers the daemon's own profile because "custom" is a virtual
// profile that is deliberately never written to platform_profile — so sysfs
// alone cannot tell a legitimate minimum-watts custom TDP from the kernel's
// stale boot cache, and ReadEffective would substitute the stock table for
// real values. Falls back to the platform profile when the daemon is not
// running.
func effectiveProfileForTDP(hw *device.Device) string {
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil && st.Profile != "" {
		return st.Profile
	}
	return readCurrentProfile(hw)
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

	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Power == nil {
		return fmt.Errorf("no power limit control on this device")
	}
	env := powerEnvFor(hw)

	// The one validation the daemon shares (safety.ResolveTDP): PL1 refused
	// outside its range, PL2/PL3 above theirs refused and below theirs raised,
	// with a note for each raise. Resolved here as well as in the daemon so the
	// notes print either way.
	tdp, notes, err := safety.ResolveTDP(env, watts, pl1, pl2, pl3, tdpForceFlag)
	if err != nil {
		return errors.New(strings.Replace(err.Error(), "use force", "use --force", 1))
	}

	if dryRunFlag {
		if tdpProfileFlag != "" {
			cli.DryRunProfileEdit(tdpProfileFlag, "power limits")
			return nil
		}
		cli.DryRunTdp(env, tdp, tdpForceFlag, liveFanCurve(hw), asusz13.PlanTDPWrites(tdp))
		printNotes(notes)
		return nil
	}

	// The daemon applies the fan floor itself (its engine's ApplyTDPSafely), so
	// hand the whole operation over before touching hardware here.
	if err := ensureProfileTargetSupported(tdpProfileFlag); err != nil {
		return err
	}

	// Sampled *before* the send. Asking after it is meaningless: the daemon has
	// already written the clamped curve, so the live curve then satisfies the floor
	// by construction and FloorAdjustsCurve is always false — except when the read
	// fails and nil makes it spuriously true, which is exactly backwards.
	preCurve := liveFanCurve(hw)

	if handled, err := api.SendTdpSetFor(tdpProfileFlag, tdpSetFlag, tdpPL1Flag, tdpPL2Flag, tdpPL3Flag, tdpForceFlag); handled {
		if err != nil {
			return err
		}
		if tdpProfileFlag != "" {
			fmt.Print(profileEditMessage(tdpProfileFlag, ""))
			return nil
		}
		printFloorNotice(env, fanPWMMax(hw), pl1, preCurve, true)
		fmt.Printf("TDP set to %dW\n", watts)
		printNotes(notes)
		return nil
	}

	if err := requireDaemonForProfile(tdpProfileFlag); err != nil {
		return err
	}
	// Direct path: the same engine the daemon uses, so the no-daemon path
	// enforces the fan floor on the same terms — fans first, and no TDP at all
	// if that write fails.
	//
	// The live curve is what this path has instead of profile state. Without it
	// the floor would replace a curve the user set moments earlier even when that
	// curve is well above it. preCurve was sampled before the socket attempt,
	// which never wrote anything on this branch, so it is still current.
	want := preCurve
	if err := hw.Power.ApplyTDPSafely(tdp, want); err != nil {
		return fmt.Errorf("setting TDP: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
	}
	printFloorNotice(env, fanPWMMax(hw), pl1, want, false)
	// Not only above the safe maximum: any profile change or fan release
	// discards a custom limit of any size (z13ctl issue #22), and with no daemon
	// nothing puts it back.
	fmt.Println("  Note: a system power profile change (GNOME power modes, power-profiles-daemon")
	fmt.Println("  on plugging or unplugging the charger, Fn+F5) makes the firmware re-apply its")
	fmt.Println("  own limits and releases custom fan curves, and the voltaire daemon is not")
	fmt.Println("  running to restore them. Start it (see 'voltaire daemon') to keep this limit.")
	fmt.Printf("TDP set to %dW\n", watts)
	printNotes(notes)
	return nil
}

// printNotes prints what ResolveTDP changed about the request.
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
func printFloorNotice(env driver.PowerEnvelope, pwmMax, pl1 int, want []api.FanCurvePoint, viaDaemon bool) {
	if pl1 <= env.TDPMaxSafe {
		return
	}
	bottom, top, topTemp, ok := safety.FloorSpan(env.FloorCurve)
	if !ok {
		return
	}
	pct := func(pwm int) int { return pwmPercent(pwm, pwmMax) }
	// The floor as the device declares it: flat, or a ramp to its top.
	shape := fmt.Sprintf("a flat %d PWM (%d%%) floor", bottom, pct(bottom))
	if top > bottom {
		shape = fmt.Sprintf("a %d PWM (%d%%) floor rising to %d PWM (%d%%) at %d°C",
			bottom, pct(bottom), top, pct(top), topTemp)
	}
	switch {
	case len(want) == 0:
		fmt.Printf("Fans set to the built-in high-TDP curve: %s\n", shape)
		fmt.Println("  (no custom fan curve was in force to keep)")
	case safety.FloorAdjustsCurve(env, pl1, want):
		fmt.Println("Fan curve points below the built-in high-TDP curve were raised to it; every")
		fmt.Println("  other point is unchanged. The floor is measured at each point's own")
		fmt.Printf("  temperature: %s\n", shape)
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
		hw, err := hardware()
		if err != nil {
			return err
		}
		cli.DryRunTdpReset(powerEnvFor(hw), defaultProfile(hw))
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
		fmt.Printf("TDP reset: switched to the %s profile (firmware power limits restored)\n", landingProfileName())
		return nil
	}

	if err := requireDaemonForProfile(tdpProfileFlag); err != nil {
		return err
	}
	landing, err := runTdpResetDirect()
	if err != nil {
		return err
	}
	fmt.Printf("TDP reset: switched to the %s profile\n", landing)
	return nil
}

// runTdpResetDirect is the no-daemon release sequence, shared by tdp --reset and
// tuning --reset. Without a daemon there are no saved profiles to edit, so the
// two commands do exactly the same thing to hardware and there is no reason for
// two copies of an ordering that has to be right.
//
// Switch to the device's default firmware profile, then HandBackToFirmware —
// the stock row first, so a high custom TDP is down before the fans drop to
// auto, and the release last, so that profile's own limits end in force. The
// landing profile is resolved before anything is touched, and returned for the
// caller's message.
//
// Reset the undervolt as well: this lands on a stock profile, and every other
// route to one clears CO. Guarded on Present (a stat, never the destructive
// probe) so machines without ryzen_smu do not get a spurious warning, and on
// daemon.UndervoltApplied so an offset that was never applied is never
// "cleared" — a speculative MP1 write is the one with a known hard-hang mode
// (see Daemon.uvApplied).
func runTdpResetDirect() (string, error) {
	hw, err := hardware()
	if err != nil {
		return "", err
	}
	if hw.Profiles == nil {
		return "", fmt.Errorf("no profile control on this device")
	}
	landing := hw.Profiles.Default()
	if landing == "" {
		return "", fmt.Errorf("this device names no default firmware profile to reset to")
	}
	if hw.Undervolt != nil && hw.Undervolt.Present() && daemon.UndervoltApplied() {
		if err := hw.Undervolt.Reset(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to reset undervolt: %v\n", err)
		}
	}
	if err := hw.Profiles.Set(landing); err != nil {
		return "", fmt.Errorf("switching to the %s profile: %w\n  (run 'sudo voltaire setup' to enable non-root access)", landing, err)
	}
	if err := hw.HandBackToFirmware(landing); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to restore the %s power limits: %v\n", landing, err)
	}
	return landing, nil
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
	tdpCmd.Flags().BoolVar(&tdpResetFlag, "reset", false, "Reset to the device's default firmware profile and hand the power limits back to the firmware")
	tdpCmd.Flags().StringVar(&tdpPL1Flag, "pl1", "", "Override PL1/SPL (watts)")
	tdpCmd.Flags().StringVar(&tdpPL2Flag, "pl2", "", "Override PL2/sPPT (watts)")
	tdpCmd.Flags().StringVar(&tdpPL3Flag, "pl3", "", "Override PL3/fPPT (watts)")
	tdpCmd.Flags().BoolVar(&tdpForceFlag, "force", false, "Allow sustained TDP (PL1) above the safe maximum, up to the kernel's maximum (see --get)")
	tdpCmd.Flags().StringVar(&tdpProfileFlag, "profile", "", profileFlagUsage)
	rootCmd.AddCommand(tdpCmd)
}
