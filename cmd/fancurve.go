package cmd

// fancurve.go — "fancurve" subcommand: read or set custom fan curves via the
// Linux asus-nb-wmi hwmon sysfs interface. No HID access required.

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
	fanCurveFanFlag   string
)

var fancurveCmd = &cobra.Command{
	Use:   "fancurve",
	Short: "Get or set custom fan curves via asus-nb-wmi hwmon",
	Long: `Get or set custom fan curves via the Linux asus-nb-wmi hwmon sysfs interface.

With --get, prints the current 8-point fan curve, fan mode, and RPM for the
specified fan (or both fans if --fan is not set).

With --set, writes a custom 8-point fan curve. The curve must be specified as
8 comma-separated temp:pwm pairs where temp is in Celsius and pwm is 0–255.
Temps must be monotonically increasing; pwm values must be non-decreasing.

With --reset, restores firmware auto mode (pwm_enable=2).

Fan names:
  cpu  — CPU fan (fan1/pwm1)
  gpu  — GPU fan (fan2/pwm2)`,
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
	fans := []string{"cpu", "gpu"}
	if fanCurveFanFlag != "" {
		if _, err := cli.FanIndex(fanCurveFanFlag); err != nil {
			return err
		}
		fans = []string{fanCurveFanFlag}
	}
	for i, fan := range fans {
		if i > 0 {
			fmt.Println()
		}
		idx, _ := cli.FanIndex(fan)
		rpm, rpmErr := cli.ReadFanRPM(fan)
		mode, modeErr := cli.ReadFanMode(fan)
		points, curveErr := cli.ReadFanCurve(fan)

		rpmStr := "N/A"
		if rpmErr == nil {
			rpmStr = fmt.Sprintf("%d RPM", rpm)
		}
		modeStr := "N/A"
		if modeErr == nil {
			modeStr = cli.FanModeName(mode)
		}
		fmt.Printf("%s (fan%d): %s, mode: %s\n", fan, idx, rpmStr, modeStr)
		if curveErr != nil {
			fmt.Printf("  error reading curve: %v\n", curveErr)
			continue
		}
		for _, p := range points {
			pct := p.PWM * 100 / 255
			fmt.Printf("  %3d°C: %3d/255 (%2d%%)\n", p.Temp, p.PWM, pct)
		}
	}
	return nil
}

func runFanCurveSet() error {
	if fanCurveFanFlag == "" {
		return fmt.Errorf("--fan is required with --set (cpu or gpu)")
	}
	points, err := cli.ParseFanCurve(fanCurveSetFlag)
	if err != nil {
		return fmt.Errorf("invalid fan curve: %w", err)
	}

	if dryRunFlag {
		cli.DryRunFanCurve(fanCurveFanFlag, points)
		return nil
	}

	if handled, err := api.SendFanCurveSet(fanCurveFanFlag, fanCurveSetFlag); handled {
		if err != nil {
			return err
		}
		fmt.Printf("Fan curve set for %s fan (custom mode enabled)\n", fanCurveFanFlag)
		return nil
	}

	if err := cli.SetFanCurve(fanCurveFanFlag, points); err != nil {
		return fmt.Errorf("setting fan curve: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Printf("Fan curve set for %s fan (custom mode enabled)\n", fanCurveFanFlag)
	return nil
}

func runFanCurveReset() error {
	fan := fanCurveFanFlag // "" means both

	if dryRunFlag {
		cli.DryRunFanCurveReset(fan)
		return nil
	}

	if fan == "" {
		if handled, err := api.SendFanCurveReset(""); handled {
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

	if handled, err := api.SendFanCurveReset(fan); handled {
		if err != nil {
			return err
		}
		fmt.Printf("Fan curve reset to auto mode (%s fan)\n", fan)
		return nil
	}
	if err := cli.ResetFanCurve(fan); err != nil {
		return fmt.Errorf("resetting fan curve: %w\n  (run 'sudo z13ctl setup' to enable non-root access)", err)
	}
	fmt.Printf("Fan curve reset to auto mode (%s fan)\n", fan)
	return nil
}

func init() {
	fancurveCmd.Flags().BoolVar(&fanCurveGetFlag, "get", false, "Print the current fan curve(s) and RPM")
	fancurveCmd.Flags().StringVar(&fanCurveSetFlag, "set", "", "Set a custom 8-point fan curve (temp:pwm,...)")
	fancurveCmd.Flags().BoolVar(&fanCurveResetFlag, "reset", false, "Restore firmware auto fan mode")
	fancurveCmd.Flags().StringVar(&fanCurveFanFlag, "fan", "", "Target fan: cpu or gpu (required for --set)")
	rootCmd.AddCommand(fancurveCmd)
}
