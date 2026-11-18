package cmd

// rumble.go — "rumble" subcommand: read or set the game controller's
// vibration strength (hid-oxp's rumble_intensity on the OneXPlayer X2).
//
// Like CPU boost it is a setting the daemon records and replays, at startup
// and after a suspend, so --set prefers the daemon even though the sysfs write
// would succeed without it.

import (
	"fmt"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"

	"github.com/spf13/cobra"
)

var (
	rumbleGetFlag bool
	rumbleSetFlag string
)

var rumbleCmd = &cobra.Command{
	Use:   "rumble",
	Short: "Get or set the controller's vibration strength",
	Long: `Get or set the built-in controller's vibration (rumble) strength.

The scale is the controller's own, as the kernel reports it (0–5 on the
OneXPlayer X2 Mini Pro), where 0 turns vibration off. It scales every rumble a
game asks for. The voltaire daemon records the value and puts it back at
startup and after a suspend; setting it with the daemon stopped changes the
controller but nothing will restore it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !rumbleGetFlag && rumbleSetFlag == "" {
			return cmd.Help()
		}
		hw, err := hardware()
		if err != nil {
			return err
		}
		if hw.Rumble == nil {
			return fmt.Errorf("no controller vibration control on this device")
		}

		if rumbleSetFlag != "" {
			v, convErr := strconv.Atoi(rumbleSetFlag)
			if convErr != nil {
				return fmt.Errorf("invalid value %q: must be a whole number", rumbleSetFlag)
			}
			lo, hi, rErr := hw.Rumble.Range()
			if rErr == nil && (v < lo || v > hi) {
				return fmt.Errorf("invalid value %d: must be %d–%d", v, lo, hi)
			}

			if dryRunFlag {
				cli.DryRunRumble(v, lo, hi, rErr)
				return nil
			}

			if handled, sendErr := api.SendRumbleSet(v); handled {
				if sendErr != nil {
					return sendErr
				}
				fmt.Printf("Vibration strength %s\n", rumbleText(v, hi))
				return nil
			}
			if setErr := hw.Rumble.Set(v); setErr != nil {
				return fmt.Errorf("setting vibration strength: %w\n  (run 'sudo voltaire setup' to enable non-root access)", setErr)
			}
			fmt.Printf("Vibration strength %s (daemon not running — this will not be restored after a reboot or suspend)\n", rumbleText(v, hi))
			return nil
		}

		// --get reads the controller directly, as every other --get does.
		v, err := hw.Rumble.Get()
		if err != nil {
			return fmt.Errorf("reading vibration strength: %w", err)
		}
		_, hi, _ := hw.Rumble.Range()
		fmt.Printf("Vibration strength: %s\n", rumbleText(v, hi))
		return nil
	},
}

// rumbleText says a strength on its scale: "off", or "3 of 5".
func rumbleText(v, hi int) string {
	if v == 0 {
		return "off"
	}
	if hi > 0 {
		return fmt.Sprintf("%d of %d", v, hi)
	}
	return strconv.Itoa(v)
}

func init() {
	rumbleCmd.Flags().BoolVar(&rumbleGetFlag, "get", false, "Show the vibration strength")
	rumbleCmd.Flags().StringVar(&rumbleSetFlag, "set", "", "Set the vibration strength (0 is off)")
	rootCmd.AddCommand(rumbleCmd)
}
