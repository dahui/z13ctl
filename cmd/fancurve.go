package cmd

// fancurve.go — "fancurve" subcommand: read or set custom fan curves via the
// Linux asus-nb-wmi hwmon sysfs interface. No HID access required.
//
// Both physical fans cool the same APU, so the same curve is always applied
// to both fans simultaneously.

import (
	"fmt"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	fanCurveGetFlag   bool
	fanCurveSetFlag   string
	fanCurveResetFlag bool
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

Safety: while sustained TDP (PL1) is above 75W, every curve point must be at
least 204 PWM (80%) and --reset is refused, since firmware auto mode has no
minimum. Lower the limit first with 'z13ctl tdp --reset'.`,
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
	// Display fan1 only — the Z13 APU has two physical fans but they share a
	// single hwmon control channel (pwm1). The kernel exposes a phantom fan2
	// channel (0 RPM, pwm2_enable returns EIO) intended for GPU-fan SKUs.
	rpms, rpmErr := cli.ReadBothFanRPM()
	modes, modeErr := cli.ReadBothFanModes()
	curves, curveErr := cli.ReadBothFanCurves()

	rpmStr := "N/A"
	if rpmErr == nil {
		rpmStr = fmt.Sprintf("%d RPM", rpms[0])
	}
	modeStr := "N/A"
	if modeErr == nil {
		modeStr = cli.FanModeName(modes[0])
	}
	tempStr := ""
	if temp, err := cli.ReadAPUTemperature(); err == nil {
		tempStr = fmt.Sprintf(", APU: %d°C", temp)
	}
	fmt.Printf("Fans: %s, mode: %s%s\n", rpmStr, modeStr, tempStr)
	if curveErr != nil {
		fmt.Printf("  error reading curve: %v\n", curveErr)
		return nil
	}
	for _, p := range curves[0] {
		pct := p.PWM * 100 / 255
		fmt.Printf("  %3d°C: %3d/255 (%2d%%)\n", p.Temp, p.PWM, pct)
	}
	return nil
}

func runFanCurveSet() error {
	points, err := cli.ParseFanCurve(fanCurveSetFlag)
	if err != nil {
		return fmt.Errorf("invalid fan curve: %w", err)
	}

	// Enforce the minimum PWM floor when sustained TDP exceeds the safe max.
	if err := cli.CheckFanCurveFloor(effectiveProfileForTDP(), points); err != nil {
		return err
	}

	if dryRunFlag {
		cli.DryRunFanCurve(points)
		return nil
	}

	if handled, err := api.SendFanCurveSet(fanCurveSetFlag); handled {
		if err != nil {
			return err
		}
		fmt.Println("Fan curves set for both fans (custom mode enabled)")
		return nil
	}

	if err := cli.SetBothFanCurves(points); err != nil {
		return fmt.Errorf("setting fan curves: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Println("Fan curves set for both fans (custom mode enabled)")
	return nil
}

func runFanCurveReset() error {
	// Checked before the dry-run branch, as in runFanCurveSet: this is a
	// read-only check, and a dry run that reported success for a reset the real
	// command would refuse would be worse than useless.
	//
	// Firmware auto has no PWM floor, so releasing the fans while a high
	// sustained TDP is still in force removes the protection the high-TDP curve
	// provides. "tdp --reset" is the way out — it lowers power first.
	if err := cli.CheckFanFloorRelease(effectiveProfileForTDP()); err != nil {
		return err
	}

	if dryRunFlag {
		cli.DryRunFanCurveReset()
		return nil
	}

	if handled, err := api.SendFanCurveReset(); handled {
		if err != nil {
			return err
		}
		fmt.Println("Fan curves reset to auto mode (both fans)")
		return nil
	}
	if err := cli.ResetAllFanCurves(); err != nil {
		return fmt.Errorf("resetting fan curves: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Println("Fan curves reset to auto mode (both fans)")
	return nil
}

func init() {
	fancurveCmd.Flags().BoolVar(&fanCurveGetFlag, "get", false, "Print the current fan curve, mode, and RPM")
	fancurveCmd.Flags().StringVar(&fanCurveSetFlag, "set", "", "Set a custom 8-point fan curve (temp:pwm or temp:pct%,...)")
	fancurveCmd.Flags().BoolVar(&fanCurveResetFlag, "reset", false, "Restore firmware auto fan mode")
	rootCmd.AddCommand(fancurveCmd)
}
