package cmd

// fancurve.go — "fancurve" subcommand: read or set custom fan curves via the
// Linux asus-nb-wmi hwmon sysfs interface. No HID access required.
//
// Both physical fans cool the same APU, so the same curve is always applied
// to both fans simultaneously.

import (
	"fmt"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"

	"github.com/spf13/cobra"
)

var (
	fanCurveGetFlag         bool
	fanCurveSetFlag         string
	fanCurveResetFlag       bool
	fanCurveProfileFlag     string
	fanCurvePresetFlag      string
	fanCurveListPresetsFlag bool
)

var fancurveCmd = &cobra.Command{
	Use:   "fancurve",
	Short: "Get or set custom fan curves via asus-nb-wmi hwmon",
	Long: `Get or set custom fan curves via the Linux asus-nb-wmi hwmon sysfs interface.

The same curve is always applied to every fan.

With --get, prints the current fan curve, fan mode, and RPM.

With --set, writes a custom fan curve to every fan. The curve is a list of
comma-separated temp:speed pairs, one per curve point (--get shows how many the
device takes), where temp is in Celsius and speed is either a raw PWM value or a
percentage with a % suffix (0–100%). Both formats can be mixed. Temps must be
monotonically increasing; speed values must be non-decreasing.

With --reset, restores firmware auto mode (pwm_enable=2) for every fan.

The mode shown by --get is the truth: "custom" means the kernel is honouring
your curve, "auto" means it is not, whatever points are listed. The kernel
driver discards custom fan curves on every system power profile change — GNOME
power modes, power-profiles-daemon (including its automatic AC/battery
switching), Fn+F5, asusctl. Run the daemon and it re-applies your curve within
a couple of seconds; without it, re-run --set after any profile change.

Use --profile <name> to store a curve in a profile you are NOT running: nothing
is written to the fans, which is how you build the profile 'voltaire autoswitch'
selects on battery.

Safety: while sustained TDP (PL1) is above the device's safe maximum ('voltaire
tdp --get' prints it), every curve point must meet the device's high-TDP floor
curve at that point's temperature, and --reset is refused, since firmware auto
mode has no minimum. Lower the limit first with 'voltaire tdp --reset'. A curve stored in a
profile you are not running is checked against that profile's own power limit.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if fanCurveListPresetsFlag {
			return runFanCurveListPresets()
		}
		if fanCurvePresetFlag != "" && fanCurveSetFlag != "" {
			return fmt.Errorf("--preset and --set both name a curve; use one")
		}
		if !fanCurveGetFlag && fanCurveSetFlag == "" && !fanCurveResetFlag && fanCurvePresetFlag == "" {
			return cmd.Help()
		}

		// A preset resolves to the very same curve string --set takes, so
		// everything below this line — the floor check, the dry run, the
		// daemon send, the no-daemon fallback — is the one path. A preset is
		// sugar for a curve, never a second way to write one.
		if fanCurvePresetFlag != "" {
			if err := resolveFanPreset(fanCurvePresetFlag); err != nil {
				return err
			}
		}
		if fanCurveSetFlag != "" {
			return runFanCurveSet()
		}
		if fanCurveResetFlag {
			return runFanCurveReset()
		}
		return runFanCurveGet()
	},
}

// fanPresets returns the device's declared presets, or an error naming why
// there are none to choose from.
func fanPresets() ([]api.FanPreset, error) {
	hw, err := hardware()
	if err != nil {
		return nil, err
	}
	if hw.Fans == nil {
		return nil, fmt.Errorf("no fan control on this device")
	}
	presets := hw.Fans.Shape().Presets
	if len(presets) == 0 {
		return nil, fmt.Errorf("this device declares no fan curve presets")
	}
	return presets, nil
}

// resolveFanPreset turns --preset into the equivalent --set curve string.
func resolveFanPreset(name string) error {
	presets, err := fanPresets()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(presets))
	for _, p := range presets {
		if strings.EqualFold(p.Name, name) {
			fanCurveSetFlag = api.FormatFanCurve(p.Curve)
			return nil
		}
		names = append(names, p.Name)
	}
	return fmt.Errorf("unknown fan preset %q (this device offers: %s)", name, strings.Join(names, ", "))
}

func runFanCurveListPresets() error {
	presets, err := fanPresets()
	if err != nil {
		return err
	}
	for _, p := range presets {
		fmt.Printf("%s — %s\n", p.Name, p.Label)
		if p.Description != "" {
			fmt.Printf("  %s\n", p.Description)
		}
		fmt.Printf("  %s\n", api.FormatFanCurve(p.Curve))
	}
	fmt.Println()
	fmt.Println("Apply one with 'voltaire fancurve --preset <name>'. A preset is a starting")
	fmt.Println("point: it is applied exactly like a curve you drew, and nothing re-applies it")
	fmt.Println("when the profile changes.")
	return nil
}

func runFanCurveGet() error {
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Fans == nil {
		return fmt.Errorf("no fan control on this device")
	}
	// RPM and temperature come from the daemon when it is running: fan RPM is a
	// live asus-wmi read, which the daemon withholds while the EC is not
	// answering (see readLive). The mode and the curve registers are cached
	// kernel values and are read here either way. The mode is the driver's fold
	// across every readable fan: "custom" only when all of them honour the
	// curve, which is the truth the daemon acts on too.
	st, daemonUp := daemonState()
	live := readLive(hw, st, daemonUp)
	mode, modeErr := hw.Fans.ReadMode()
	curve, curveErr := hw.Fans.LiveCurve()

	modeStr := "N/A"
	if modeErr == nil {
		modeStr = driver.FanModeName(mode)
	}
	tempStr := ""
	if live.TempC > 0 {
		tempStr = fmt.Sprintf(", APU: %d°C", live.TempC)
	}
	fmt.Printf("Fans: %s, mode: %s%s\n", formatRPM(live.RPM), modeStr, tempStr)
	if curveErr != nil {
		fmt.Printf("  error reading curve: %v\n", curveErr)
		return nil
	}
	pwmMax := fanPWMMax(hw)
	for _, p := range curve {
		fmt.Printf("  %3d°C: %3d/%d (%2d%%)\n", p.Temp, p.PWM, pwmMax, pwmPercent(p.PWM, pwmMax))
	}
	return nil
}

func runFanCurveSet() error {
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Fans == nil {
		return fmt.Errorf("no fan control on this device")
	}
	points, err := cli.ParseFanCurve(hw.Fans.Shape(), fanCurveSetFlag)
	if err != nil {
		return fmt.Errorf("invalid fan curve: %w", err)
	}

	// Enforce the floor when sustained TDP exceeds the safe max, against the
	// limit hardware reports for the effective profile — a PPT read failure is
	// deliberately not a refusal. Only meaningful for the running machine: when
	// --profile names another profile the daemon checks the curve against that
	// profile's own TDP, since hardware says nothing about a profile that is
	// not applied. Run here only where the daemon will not: a dry run, and the
	// no-daemon path. The daemon makes the same check itself, and the CLI reading
	// the limits while it is up would bypass its wedged-EC gate (on asus-armoury
	// each read is a live ACPI call).
	checkFloor := func() error {
		if fanCurveProfileFlag != "" || hw.Power == nil {
			return nil
		}
		tdp, rErr := hw.Power.ReadEffective(effectiveProfileForTDP(hw))
		if rErr != nil {
			return nil
		}
		return safety.CheckCurveAgainstTDP(hw.Power.Envelope(), points, tdp.PL1SPL)
	}

	if dryRunFlag {
		if err := checkFloor(); err != nil {
			return err
		}
		if fanCurveProfileFlag != "" {
			cli.DryRunProfileEdit(fanCurveProfileFlag, "fan curve")
			return nil
		}
		cli.DryRunFanCurve(points)
		return nil
	}

	if err := ensureProfileTargetSupported(fanCurveProfileFlag); err != nil {
		return err
	}
	if handled, err := api.SendFanCurveSetFor(fanCurveProfileFlag, fanCurveSetFlag); handled {
		if err != nil {
			return err
		}
		if fanCurveProfileFlag != "" {
			fmt.Print(profileEditMessage(fanCurveProfileFlag, ""))
			return nil
		}
		fmt.Println("Fan curves set for both fans (custom mode enabled)")
		fmt.Println("  Note: the kernel driver drops custom fan curves whenever the system power")
		fmt.Println("  profile changes (GNOME power modes, power-profiles-daemon, Fn+F5). The voltaire")
		fmt.Println("  daemon watches for that and re-applies this curve within a couple of seconds.")
		return nil
	}

	if err := requireDaemonForProfile(fanCurveProfileFlag); err != nil {
		return err
	}
	if hw.Fans == nil {
		return fmt.Errorf("no fan control on this device")
	}
	if err := checkFloor(); err != nil {
		return err
	}
	if err := hw.Fans.ApplyCurve(points); err != nil {
		return fmt.Errorf("setting fan curves: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
	}
	fmt.Println("Fan curves set for both fans (custom mode enabled)")
	fmt.Println("  Warning: the kernel driver drops custom fan curves whenever the system power")
	fmt.Println("  profile changes (GNOME power modes, power-profiles-daemon, Fn+F5), and the")
	fmt.Println("  voltaire daemon is not running to restore it. Re-run this command after any")
	fmt.Println("  profile change, or start the daemon (see 'voltaire daemon').")
	return nil
}

func runFanCurveReset() error {
	hw, err := hardware()
	if err != nil {
		return err
	}

	// Firmware auto has no PWM floor, so releasing the fans while a high
	// sustained TDP is still in force removes the protection the high-TDP curve
	// provides. "tdp --reset" is the way out — it lowers power first.
	//
	// Checked in the dry run, since a dry run that reported success for a reset
	// the real command would refuse would be worse than useless, and on the
	// no-daemon path. The daemon makes the same check itself, and the CLI
	// reading the limits while it is up would bypass its wedged-EC gate (on
	// asus-armoury each read is a live ACPI call), as in runFanCurveSet.
	checkRelease := func() error {
		if fanCurveProfileFlag != "" || hw.Power == nil {
			return nil
		}
		return hw.Power.CheckFanFloorRelease(effectiveProfileForTDP(hw))
	}

	if dryRunFlag {
		if err := checkRelease(); err != nil {
			return err
		}
		if fanCurveProfileFlag != "" {
			cli.DryRunProfileEdit(fanCurveProfileFlag, "cleared fan curve")
			return nil
		}
		cli.DryRunFanCurveReset()
		return nil
	}

	if err := ensureProfileTargetSupported(fanCurveProfileFlag); err != nil {
		return err
	}
	if handled, err := api.SendFanCurveResetFor(fanCurveProfileFlag); handled {
		if err != nil {
			return err
		}
		if fanCurveProfileFlag != "" {
			fmt.Printf("Cleared the fan curve from profile %s\n", fanCurveProfileFlag)
			return nil
		}
		fmt.Println("Fan curves reset to auto mode (both fans)")
		return nil
	}
	if err := requireDaemonForProfile(fanCurveProfileFlag); err != nil {
		return err
	}
	if hw.Fans == nil {
		return fmt.Errorf("no fan control on this device")
	}
	if err := checkRelease(); err != nil {
		return err
	}
	if err := hw.ReleaseFans(customLimitInForce(hw)); err != nil {
		return fmt.Errorf("resetting fan curves: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
	}
	fmt.Println("Fan curves reset to auto mode (both fans)")
	return nil
}

// customLimitInForce is the power limit a no-daemon fan release must re-write, or
// nil when there is none to keep. The release makes the firmware re-apply the
// platform profile's own limits (issue #22), so without this a `tdp --set` made
// earlier would be silently undone by `fancurve --reset`.
//
// With no daemon there is no profile state, and the limiter's cache — whatever
// was last written — is the only record. It counts as a custom limit only when it
// is neither the interface's untouched boot cache (safety.CacheStale: 5 W on
// asus-nb-wmi, the defaults on asus-armoury) nor the active firmware profile's
// stock row as the interface holds it: re-writing either would replace the
// firmware's limits with ours. A deliberate limit equal to the boot cache is
// indistinguishable from it here and is not kept; the daemon has no such blind
// spot.
func customLimitInForce(hw *device.Device) *api.TDPState {
	if hw.Power == nil {
		return nil
	}
	env := hw.Power.Envelope()
	cur, err := hw.Power.Read()
	if err != nil || safety.CacheStale(env, cur) || cur.PL1SPL > env.TDPMaxSafe {
		return nil
	}
	if hw.Profiles != nil {
		if profile, err := hw.Profiles.Get(); err == nil {
			// EffectiveTDP: on asus-armoury the row reads back without APU and
			// Platform sPPT, so the raw row never equals the readback.
			if stock, ok := env.StockProfilePPT[profile]; ok && safety.EffectiveTDP(env, stock) == cur {
				return nil
			}
		}
	}
	return &cur
}

func init() {
	fancurveCmd.Flags().BoolVar(&fanCurveGetFlag, "get", false, "Print the current fan curve, mode, and RPM")
	fancurveCmd.Flags().StringVar(&fanCurveSetFlag, "set", "", "Set a custom 8-point fan curve (temp:pwm or temp:pct%,...)")
	fancurveCmd.Flags().BoolVar(&fanCurveResetFlag, "reset", false, "Restore firmware auto fan mode")
	fancurveCmd.Flags().StringVar(&fanCurvePresetFlag, "preset", "", "Apply a named preset curve (see --list-presets)")
	fancurveCmd.Flags().BoolVar(&fanCurveListPresetsFlag, "list-presets", false, "List the preset curves this device offers")
	fancurveCmd.Flags().StringVar(&fanCurveProfileFlag, "profile", "", profileFlagUsage)
	rootCmd.AddCommand(fancurveCmd)
}
