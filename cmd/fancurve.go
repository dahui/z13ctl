package cmd

// fancurve.go — "fancurve" subcommand: read or set custom fan curves via the
// Linux asus-nb-wmi hwmon sysfs interface. No HID access required.
//
// Both physical fans cool the same APU, so the same curve is always applied
// to both fans simultaneously.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"

	"github.com/spf13/cobra"
)

var (
	fanCurveGetFlag     bool
	fanCurveSetFlag     string
	fanCurveResetFlag   bool
	fanCurveProfileFlag string
)

var fancurveCmd = &cobra.Command{
	Use:   "fancurve",
	Short: "Get or set custom fan curves via asus-nb-wmi hwmon",
	Long: `Get or set custom fan curves via the Linux asus-nb-wmi hwmon sysfs interface.

Both physical fans cool the same APU, so the same curve is always applied to
both fans simultaneously.

With --get, prints the current 8-point fan curve, fan mode, and RPM.

With --set, writes a custom 8-point fan curve to both fans. The curve must be
specified as 8 comma-separated temp:speed pairs where temp is in Celsius and
speed is either a PWM value (0–255) or a percentage with a % suffix (0–100%).
Both formats can be mixed. Temps must be monotonically increasing; speed values
must be non-decreasing.

With --reset, restores firmware auto mode (pwm_enable=2) for both fans.

The mode shown by --get is the truth: "custom" means the kernel is honouring
your curve, "auto" means it is not, whatever points are listed. The kernel
driver discards custom fan curves on every system power profile change — GNOME
power modes, power-profiles-daemon (including its automatic AC/battery
switching), Fn+F5, asusctl. Run the daemon and it re-applies your curve within
a couple of seconds; without it, re-run --set after any profile change.

Use --profile <name> to store a curve in a profile you are NOT running: nothing
is written to the fans, which is how you build the profile 'voltaire autoswitch'
selects on battery.

Safety: while sustained TDP (PL1) is above 75W, every curve point must be at
least 127 PWM (50%) and --reset is refused, since firmware auto mode has no
minimum. Lower the limit first with 'voltaire tdp --reset'. A curve stored in a
profile you are not running is checked against that profile's own power limit.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !fanCurveGetFlag && fanCurveSetFlag == "" && !fanCurveResetFlag {
			return cmd.Help()
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

func runFanCurveGet() error {
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Fans == nil {
		return fmt.Errorf("no fan control on this device")
	}
	// Display fan 0 only — the Z13's two fans always run the same curve. The
	// mode is the driver's fold across every readable fan: "custom" only when
	// all of them honour the curve, which is the truth the daemon acts on too.
	rpms, rpmErr := hw.Fans.ReadRPM()
	mode, modeErr := hw.Fans.ReadMode()
	curve, curveErr := hw.Fans.LiveCurve()

	rpmStr := "N/A"
	if rpmErr == nil && len(rpms) > 0 {
		rpmStr = fmt.Sprintf("%d RPM", rpms[0])
	}
	modeStr := "N/A"
	if modeErr == nil {
		modeStr = driver.FanModeName(mode)
	}
	tempStr := ""
	if hw.Telemetry != nil {
		if s, sErr := hw.Telemetry.Sample(); sErr == nil {
			tempStr = fmt.Sprintf(", APU: %d°C", s.TempC)
		}
	}
	fmt.Printf("Fans: %s, mode: %s%s\n", rpmStr, modeStr, tempStr)
	if curveErr != nil {
		fmt.Printf("  error reading curve: %v\n", curveErr)
		return nil
	}
	for _, p := range curve {
		pct := p.PWM * 100 / 255
		fmt.Printf("  %3d°C: %3d/255 (%2d%%)\n", p.Temp, p.PWM, pct)
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
	// not applied.
	if fanCurveProfileFlag == "" && hw.Power != nil {
		if tdp, rErr := hw.Power.ReadEffective(effectiveProfileForTDP(hw)); rErr == nil {
			if cErr := safety.CheckCurveAgainstTDP(hw.Power.Envelope(), points, tdp.PL1SPL); cErr != nil {
				return cErr
			}
		}
	}

	if dryRunFlag {
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

	// Checked before the dry-run branch, as in runFanCurveSet: this is a
	// read-only check, and a dry run that reported success for a reset the real
	// command would refuse would be worse than useless.
	//
	// Firmware auto has no PWM floor, so releasing the fans while a high
	// sustained TDP is still in force removes the protection the high-TDP curve
	// provides. "tdp --reset" is the way out — it lowers power first.
	if fanCurveProfileFlag == "" && hw.Power != nil {
		if err := hw.Power.CheckFanFloorRelease(effectiveProfileForTDP(hw)); err != nil {
			return err
		}
	}

	if dryRunFlag {
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
	if err := hw.Fans.Release(); err != nil {
		return fmt.Errorf("resetting fan curves: %w\n  (run 'sudo voltaire setup' to enable non-root access)", err)
	}
	fmt.Println("Fan curves reset to auto mode (both fans)")
	return nil
}

func init() {
	fancurveCmd.Flags().BoolVar(&fanCurveGetFlag, "get", false, "Print the current fan curve, mode, and RPM")
	fancurveCmd.Flags().StringVar(&fanCurveSetFlag, "set", "", "Set a custom 8-point fan curve (temp:pwm or temp:pct%,...)")
	fancurveCmd.Flags().BoolVar(&fanCurveResetFlag, "reset", false, "Restore firmware auto fan mode")
	fancurveCmd.Flags().StringVar(&fanCurveProfileFlag, "profile", "", profileFlagUsage)
	rootCmd.AddCommand(fancurveCmd)
}
