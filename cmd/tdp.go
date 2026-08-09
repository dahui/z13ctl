package cmd

// tdp.go — "tdp" subcommand: read or set TDP power limits via the Linux
// asus-nb-wmi PPT sysfs attributes. No HID access required.

import (
	"fmt"
	"os"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
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
	Short: "Get or set TDP power limits via asus-nb-wmi PPT",
	Long: `Get or set TDP power limits via the Linux asus-nb-wmi PPT sysfs attributes.

With --get, prints all current PPT (Package Power Tracking) values.

With --set, writes power limits in watts. By default, all PPT values are set to
the same value. Use --pl1, --pl2, --pl3 to override individual limits.

Safety: The sustained power limit (PL1) is capped at 75W by default. Use --force
to allow PL1 up to 93W (the absolute hardware maximum for the ROG Flow Z13
GZ302E). When PL1 exceeds 75W, both fans are held to a curve with a 50% PWM
floor that reaches 100% at 80°C, written before the power limit; if that write
fails, or the kernel does not honour it, the TDP is not applied at all. Burst
limits (PL2/PL3) are allowed up to 93W without --force since short bursts are
thermally safe.

Run the daemon when sustaining above 75W. The kernel releases custom fan curves
on every system power profile change while the power limit survives it, so
without the daemon to restore the floor the machine can end up at high power on
the firmware's ordinary fan curve.

With --reset, switches to the balanced profile, resets fan curves to auto mode,
and writes balanced's stock PPT values back to hardware. The firmware manages
fan curves for stock profiles but does not restore PPT on its own.

PPT attributes:
  PL1/SPL          — Sustained Power Limit: the continuous power budget the APU
                     can draw indefinitely. This is your effective base TDP.
  PL2/sPPT         — Short-term boost: the APU can draw this much power for
                     several seconds before throttling back to PL1.
  PL3/fPPT         — Fast boost: the maximum instantaneous power the APU can
                     draw for millisecond-scale spikes (e.g. launching an app).
  APU sPPT         — APU-specific short-term limit (automatically set to PL2).
  Platform sPPT    — Platform-level short-term limit (automatically set to PL2).

When using --set, all three limits are set to the same value by default. Use
--pl1, --pl2, and --pl3 to set them independently — for example, --set 45
--pl2 55 --pl3 65 allows short bursts up to 65W while sustaining 45W.

Setting a TDP edits the custom profile you are running, creating and
activating "custom" if a firmware profile is active. Switching back to a
firmware profile restores that profile's stock PPT values to hardware while
keeping every custom profile saved, so they stay re-selectable.

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
	tdp, err := hw.Power.ReadEffective(effectiveProfileForTDP(hw))
	if err != nil {
		return fmt.Errorf("reading TDP: %w", err)
	}

	fmt.Println("TDP Power Limits (watts):")
	fmt.Printf("  PL1 (SPL):          %d\n", tdp.PL1SPL)
	fmt.Printf("  PL2 (sPPT):         %d\n", tdp.PL2SPPT)
	fmt.Printf("  PL3 (fPPT):         %d\n", tdp.FPPT)
	fmt.Printf("  APU sPPT:           %d\n", tdp.APUSPPT)
	fmt.Printf("  Platform sPPT:      %d\n", tdp.PlatformSPPT)
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
	env := hw.Power.Envelope()

	// PL1 (sustained) requires --force above the safe max. PL2/PL3 (burst) are
	// allowed up to the hardware max without --force since short bursts are
	// thermally safe.
	pl1Max := env.TDPMaxSafe
	if tdpForceFlag {
		pl1Max = env.TDPMaxForced
	}
	if pl1 < env.TDPMin || pl1 > pl1Max {
		if pl1 > env.TDPMaxSafe && !tdpForceFlag {
			return fmt.Errorf("PL1 value %dW exceeds safe sustained maximum (%dW); use --force to allow up to %dW",
				pl1, env.TDPMaxSafe, env.TDPMaxForced)
		}
		return fmt.Errorf("PL1 value %dW out of range %d–%d", pl1, env.TDPMin, pl1Max)
	}
	for _, v := range []struct {
		name  string
		value int
	}{
		{"PL2", pl2}, {"PL3", pl3},
	} {
		if v.value < env.TDPMin || v.value > env.TDPMaxForced {
			return fmt.Errorf("%s value %dW out of range %d–%d", v.name, v.value, env.TDPMin, env.TDPMaxForced)
		}
	}

	if dryRunFlag {
		if tdpProfileFlag != "" {
			cli.DryRunProfileEdit(tdpProfileFlag, "power limits")
			return nil
		}
		cli.DryRunTdp(env, watts, pl1, pl2, pl3, tdpForceFlag, liveFanCurve(hw))
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
		printFloorNotice(env, pl1, preCurve, true)
		fmt.Printf("TDP set to %dW\n", watts)
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
	if err := hw.Power.ApplyTDPSafely(cli.TDPStateFor(watts, pl1, pl2, pl3), want); err != nil {
		return fmt.Errorf("setting TDP: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
	}
	printFloorNotice(env, pl1, want, false)
	if pl1 > env.TDPMaxSafe {
		fmt.Println("  Warning: a system power profile change (GNOME power modes,")
		fmt.Println("  power-profiles-daemon, Fn+F5) releases custom curves in the kernel driver while")
		fmt.Println("  this power limit stays in force, and the voltaire daemon is not running to")
		fmt.Println("  restore them. Start the daemon (see 'voltaire daemon') before sustaining >75W.")
	}
	fmt.Printf("TDP set to %dW\n", watts)
	return nil
}

// printFloorNotice explains what the high-TDP floor did to the fan curve, if
// anything. want is the curve that was in force before the change.
//
// The three outcomes are genuinely different and were previously collapsed into
// two: with no custom curve at all the whole built-in floor curve is written, and
// saying "points below 127 PWM were raised; every other point is unchanged" there
// described points the user never set. DryRunTdp already distinguished the case.
func printFloorNotice(env driver.PowerEnvelope, pl1 int, want []api.FanCurvePoint, daemon bool) {
	if pl1 <= env.TDPMaxSafe || len(env.FloorCurve) == 0 {
		return
	}
	minPWM := env.FloorCurve[0].PWM
	switch {
	case len(want) == 0:
		fmt.Printf("Fans set to the built-in high-TDP curve: a %d PWM (50%%) floor rising to 100%% at 80°C\n",
			minPWM)
		fmt.Println("  (no custom fan curve was in force to keep)")
	case safety.FloorAdjustsCurve(env, pl1, want):
		fmt.Println("Fan curve points below the built-in high-TDP curve were raised to it; every")
		fmt.Printf("  other point is unchanged. The floor rises with temperature — %d PWM (50%%) when\n", minPWM)
		fmt.Println("  cool, 255 (100%) at 80°C — so a point can be raised even well above 50%")
	default:
		fmt.Println("Your fan curve already clears the high-TDP floor and was kept exactly as drawn")
	}
	if daemon {
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
		cli.DryRunTdpReset(envOf(hw))
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
		fmt.Println("TDP reset: switched to balanced profile (stock PPT restored)")
		return nil
	}

	if err := requireDaemonForProfile(tdpProfileFlag); err != nil {
		return err
	}
	hw, err := hardware()
	if err != nil {
		return err
	}
	// Direct path (no daemon): switch to balanced, write its stock PPT values
	// back to hardware, and only then release the fans to firmware auto — so
	// they are never dropped to auto while a high custom TDP is still in force.
	// The firmware manages fan curves on a profile change but does not restore
	// PPT, so that part has to be explicit.
	// Reset the undervolt as well: this lands on a stock profile, and every
	// other route to one clears CO. Guarded on Present (a stat, never the
	// destructive probe) so machines without ryzen_smu do not get a spurious
	// warning.
	if hw.Undervolt != nil && hw.Undervolt.Present() {
		if err := hw.Undervolt.Reset(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to reset undervolt: %v\n", err)
		}
	}
	if hw.Profiles == nil {
		return fmt.Errorf("no profile control on this device")
	}
	if err := hw.Profiles.Set("balanced"); err != nil {
		return fmt.Errorf("switching to balanced profile: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
	}
	restoreStockPPT(hw, "balanced")
	if hw.Fans != nil {
		if err := hw.Fans.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to reset fan curves: %v\n", err)
		}
	}
	fmt.Println("TDP reset: switched to balanced profile")
	return nil
}

// restoreStockPPT writes the stock PPT values for a stock profile back to
// hardware on the direct (no-daemon) path. The PPT attributes have no "reset
// to firmware default" operation and the firmware does not re-apply
// per-profile limits on a platform_profile change, so without this a custom
// TDP leaks into every stock profile. A profile with no row in the envelope is
// a silent no-op, and write failures warn and continue: a profile switch must
// not hard-fail because the PPT restore did not take.
func restoreStockPPT(hw *device.Device, profile string) {
	if hw.Power == nil {
		return
	}
	if _, ok := hw.Power.Envelope().StockProfilePPT[profile]; !ok {
		return
	}
	if err := hw.Power.RestoreStock(profile); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to restore stock TDP for %s: %v\n", profile, err)
	}
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
	tdpCmd.Flags().BoolVar(&tdpResetFlag, "reset", false, "Reset to balanced profile and restore its stock PPT values")
	tdpCmd.Flags().StringVar(&tdpPL1Flag, "pl1", "", "Override PL1/SPL (watts)")
	tdpCmd.Flags().StringVar(&tdpPL2Flag, "pl2", "", "Override PL2/sPPT (watts)")
	tdpCmd.Flags().StringVar(&tdpPL3Flag, "pl3", "", "Override PL3/fPPT (watts)")
	tdpCmd.Flags().BoolVar(&tdpForceFlag, "force", false, "Allow sustained TDP (PL1) above 75W (up to 93W)")
	tdpCmd.Flags().StringVar(&tdpProfileFlag, "profile", "", profileFlagUsage)
	rootCmd.AddCommand(tdpCmd)
}
