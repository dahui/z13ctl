package cmd

// autoswitch.go — "autoswitch" subcommand: pick a profile automatically by
// power source. Configuration only; the daemon does the switching.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var (
	autoswitchGetFlag     bool
	autoswitchACFlag      string
	autoswitchBatteryFlag string
	autoswitchOnFlag      bool
	autoswitchOffFlag     bool
	autoswitchClearFlag   bool
)

var autoswitchCmd = &cobra.Command{
	Use:   "autoswitch",
	Short: "Apply a different profile on AC and on battery",
	Long: `Apply a different profile automatically when the charger is plugged or unplugged.

Each side takes any profile name: a firmware profile (quiet, balanced,
performance) or a custom profile. Setting --ac or --battery enables autoswitch
unless --off is given.

An empty target leaves that side alone, which hands it back to your desktop's
power management:

  z13ctl autoswitch --ac "" --battery battery-uv

Build the profile you want on battery before selecting it here — '--profile' on
'tdp', 'fancurve' and 'undervolt' edits a profile without applying it:

  z13ctl profile --create battery-uv
  z13ctl tdp --set 35 --profile battery-uv
  z13ctl undervolt --set -25 --profile battery-uv
  z13ctl autoswitch --ac balanced --battery battery-uv

Autoswitch acts only when the power source actually changes. A profile you pick
by hand therefore stays until the next plug or unplug, and z13ctl does not
contest the profile with power-profiles-daemon in between. One consequence
worth knowing: GNOME's "Automatic Power Saver" triggers on low battery rather
than on unplugging, so it can still move a firmware profile afterwards.

Requires the daemon, which is what watches the power source.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if autoswitchGetFlag {
			return runAutoswitchGet()
		}
		if !cmd.Flags().Changed("ac") && !cmd.Flags().Changed("battery") &&
			!autoswitchOnFlag && !autoswitchOffFlag && !autoswitchClearFlag {
			return cmd.Help()
		}
		return runAutoswitchSet(cmd)
	},
}

func runAutoswitchSet(cmd *cobra.Command) error {
	if autoswitchOnFlag && autoswitchOffFlag {
		return fmt.Errorf("--on and --off are mutually exclusive")
	}
	if autoswitchClearFlag && (autoswitchACFlag != "" || autoswitchBatteryFlag != "" || autoswitchOnFlag) {
		return fmt.Errorf("--clear cannot be combined with --ac, --battery, or --on")
	}

	ac := strings.ToLower(strings.TrimSpace(autoswitchACFlag))
	battery := strings.ToLower(strings.TrimSpace(autoswitchBatteryFlag))
	enabled := !autoswitchOffFlag && !autoswitchClearFlag

	if autoswitchClearFlag {
		ac, battery = "", ""
	} else {
		// Every side the user did not name keeps its configured value. Without
		// this, 'autoswitch --ac quiet' would silently wipe the battery target —
		// the command reads as "change the AC side", not "replace both".
		// An explicit --ac "" still clears that side, because the flag was given.
		acGiven, batGiven := cmd.Flags().Changed("ac"), cmd.Flags().Changed("battery")
		if !acGiven || !batGiven {
			cur, err := currentAutoswitch()
			if err != nil {
				return err
			}
			if !acGiven {
				ac = cur.AC
			}
			if !batGiven {
				battery = cur.Battery
			}
			if enabled && ac == "" && battery == "" {
				return fmt.Errorf("nothing to enable: set a target first, e.g. 'z13ctl autoswitch --ac balanced --battery custom'")
			}
		}
	}

	if dryRunFlag {
		cli.DryRunAutoswitch(enabled, ac, battery)
		return nil
	}

	handled, err := api.SendAutoswitchSet(enabled, ac, battery)
	if !handled {
		return fmt.Errorf("autoswitch requires the daemon, which watches the power source; start it first")
	}
	if err != nil {
		return err
	}
	if !enabled {
		fmt.Println("Autoswitch disabled")
		return nil
	}
	fmt.Printf("Autoswitch enabled: %s on AC, %s on battery\n", describeTarget(ac), describeTarget(battery))
	return nil
}

// autoswitchStatus mirrors the daemon's autoswitch-get payload.
type autoswitchStatus struct {
	api.AutoswitchState
	OnAC        bool `json:"on_ac"`
	SourceKnown bool `json:"source_known"`
}

func currentAutoswitch() (autoswitchStatus, error) {
	var st autoswitchStatus
	handled, value, err := api.SendAutoswitchGet()
	if !handled {
		return st, fmt.Errorf("autoswitch requires the daemon, which watches the power source; start it first")
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal([]byte(value), &st); err != nil {
		return st, fmt.Errorf("parsing autoswitch state: %w", err)
	}
	return st, nil
}

func runAutoswitchGet() error {
	st, err := currentAutoswitch()
	if err != nil {
		return err
	}

	state := "disabled"
	if st.Enabled {
		state = "enabled"
	}
	fmt.Printf("Autoswitch: %s\n", state)
	fmt.Printf("  AC:      %s\n", describeTarget(st.AC))
	fmt.Printf("  Battery: %s\n", describeTarget(st.Battery))
	if !st.SourceKnown {
		fmt.Println("  Source:  unknown (no mains power supply found)")
		return nil
	}
	source, target := "battery", st.Battery
	if st.OnAC {
		source, target = "AC", st.AC
	}
	if st.Enabled && target != "" {
		fmt.Printf("  Source:  %s (would select %s)\n", source, target)
	} else {
		fmt.Printf("  Source:  %s\n", source)
	}
	return nil
}

func describeTarget(name string) string {
	if name == "" {
		return "(unchanged)"
	}
	return name
}

func init() {
	autoswitchCmd.Flags().BoolVar(&autoswitchGetFlag, "get", false, "Print the autoswitch configuration and current power source")
	autoswitchCmd.Flags().StringVar(&autoswitchACFlag, "ac", "", "Profile to apply on AC power (empty leaves the profile alone)")
	autoswitchCmd.Flags().StringVar(&autoswitchBatteryFlag, "battery", "", "Profile to apply on battery (empty leaves the profile alone)")
	autoswitchCmd.Flags().BoolVar(&autoswitchOnFlag, "on", false, "Enable autoswitch with the configured targets")
	autoswitchCmd.Flags().BoolVar(&autoswitchOffFlag, "off", false, "Disable autoswitch, keeping the configured targets")
	autoswitchCmd.Flags().BoolVar(&autoswitchClearFlag, "clear", false, "Disable autoswitch and clear both targets")
	rootCmd.AddCommand(autoswitchCmd)
}
