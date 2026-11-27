package cmd

// undervolt.go — "undervolt" subcommand: read or set CPU Curve Optimizer
// offsets via the ryzen_smu kernel module. Requires the ryzen_smu DKMS module.

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/daemon"
	"github.com/dahui/voltaire/v2/internal/driver"

	"github.com/spf13/cobra"
)

var (
	uvGetFlag     bool
	uvSetFlag     string
	uvResetFlag   bool
	uvProfileFlag string
)

var undervoltCmd = &cobra.Command{
	Use:   "undervolt",
	Short: "Get or set the CPU undervolt offset",
	Long: `Get or set an all-core CPU undervolt offset, on devices that offer one.

An undervolt lowers the CPU's voltage-frequency curve — negative values reduce
voltage, improving efficiency and thermals without reducing performance.

With --get, prints the current offset from daemon state. The offset has no
readback, so this shows the last-applied value and whether it is active.

With --set, applies an all-core offset. Values must be 0 (stock) or negative
(undervolt).

With --reset, resets the offset to 0 (stock voltage).

The accepted range is device data; an out-of-range offset is refused with the
range this machine accepts.

Offsets are volatile — they reset on reboot and sleep. The daemon reapplies
them automatically on startup and resume when a custom profile is active.

On a device with a single TDP slider (the OneXPlayer X2 Mini Pro), an offset
set without --profile is a device setting, like the slider: it stays in force
on the firmware profile and under any custom profile that has no offset of its
own, is reapplied at startup and resume, and leaves a running custom profile.

On AMD processors the offset is the Curve Optimizer (CO), written through the
ryzen_smu kernel module (ryzen_smu-dkms-git on Arch/AUR); the amkillam fork is
required for Strix Halo (Ryzen AI MAX+). A processor without a known Curve
Optimizer command is refused before anything is written.

Use --profile <name> to store an offset in a profile you are NOT running:
nothing is applied to the CPU until you select that profile.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !uvGetFlag && uvSetFlag == "" && !uvResetFlag {
			return cmd.Help()
		}

		if uvSetFlag != "" {
			return runUndervoltSet()
		}
		if uvResetFlag {
			return runUndervoltReset()
		}
		return runUndervoltGet()
	},
}

func runUndervoltGet() error {
	if handled, uv, err := api.SendUndervoltGet(); handled {
		if err != nil {
			return err
		}
		printUndervoltState(uv.UndervoltState, uv.Profile)
		return nil
	}

	// No daemon — report only what a stat can prove. Present is the stat-only
	// question; the destructive probe belongs to the daemon alone.
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Undervolt == nil {
		return errors.New(hw.Missing("undervolt", "no undervolt control on this device"))
	}
	if !hw.Undervolt.Present() {
		return fmt.Errorf("ryzen_smu kernel module not detected\n  Install: ryzen_smu-dkms-git (AUR) or equivalent for your distro")
	}
	fmt.Println("Curve Optimizer: not set (daemon not running)")
	return nil
}

func printUndervoltState(uv api.UndervoltState, profile string) {
	if uv.CPUCO == 0 {
		fmt.Println("Curve Optimizer: stock (0)")
		return
	}
	if uv.Active {
		fmt.Println("Curve Optimizer offsets:")
	} else {
		fmt.Printf("Curve Optimizer offsets (not active — %s profile):\n", profile)
	}
	suffix := ""
	if !uv.Active {
		suffix = "  (saved)"
	}
	fmt.Printf("  CPU: %d%s\n", uv.CPUCO, suffix)
}

func runUndervoltSet() error {
	// strconv.Atoi, not fmt.Sscan: Sscan stops at the first token, so it accepts
	// "-20 40" as -20 while the daemon parses the same flag with Atoi and rejects
	// it. The two paths must agree on what is a valid value.
	cpuOffset, err := strconv.Atoi(uvSetFlag)
	if err != nil {
		return fmt.Errorf("invalid CPU undervolt value %q: must be an integer", uvSetFlag)
	}

	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Undervolt == nil {
		return errors.New(hw.Missing("undervolt", "no undervolt control on this device"))
	}
	// The bounds come from device data; the message matches the daemon's, which
	// validates the same request the same way.
	if lo, hi := hw.Undervolt.Range(); cpuOffset < lo || cpuOffset > hi {
		return fmt.Errorf("CPU undervolt %d out of range %d to %d", cpuOffset, lo, hi)
	}

	if dryRunFlag {
		if uvProfileFlag != "" {
			cli.DryRunProfileEdit(uvProfileFlag, "Curve Optimizer offset")
			return nil
		}
		command, err := undervoltCommand(hw.Undervolt)
		if err != nil {
			return err
		}
		cli.DryRunUndervolt(command, cpuOffset)
		return nil
	}

	if err := ensureProfileTargetSupported(uvProfileFlag); err != nil {
		return err
	}
	if handled, err := api.SendUndervoltSetFor(uvProfileFlag, uvSetFlag); handled {
		if err != nil {
			return err
		}
		fmt.Print(profileEditMessage(uvProfileFlag, fmt.Sprintf("Curve Optimizer set: CPU %d", cpuOffset)))
		return nil
	}

	if err := requireDaemonForProfile(uvProfileFlag); err != nil {
		return err
	}
	if err := hw.Undervolt.Apply(cpuOffset); err != nil {
		return withSetupHint("setting curve optimizer", err)
	}
	fmt.Printf("Curve Optimizer set: CPU %d\n", cpuOffset)
	return nil
}

func runUndervoltReset() error {
	if dryRunFlag {
		if uvProfileFlag != "" {
			cli.DryRunProfileEdit(uvProfileFlag, "cleared Curve Optimizer offset")
			return nil
		}
		hw, err := hardware()
		if err != nil {
			return err
		}
		if hw.Undervolt == nil {
			return errors.New(hw.Missing("undervolt", "no undervolt control on this device"))
		}
		command, err := undervoltCommand(hw.Undervolt)
		if err != nil {
			return err
		}
		cli.DryRunUndervoltReset(command)
		return nil
	}

	if err := ensureProfileTargetSupported(uvProfileFlag); err != nil {
		return err
	}
	if handled, err := api.SendUndervoltResetFor(uvProfileFlag); handled {
		if err != nil {
			return err
		}
		fmt.Print(profileEditMessage(uvProfileFlag, "Curve Optimizer reset to stock (0)"))
		return nil
	}

	if err := requireDaemonForProfile(uvProfileFlag); err != nil {
		return err
	}
	hw, err := hardware()
	if err != nil {
		return err
	}
	if hw.Undervolt == nil {
		return errors.New(hw.Missing("undervolt", "no undervolt control on this device"))
	}
	// Asked for explicitly — but if no offset is applied there is nothing to
	// clear, and the write would be the speculative MP1 message with a known
	// hard-hang mode (see Daemon.uvApplied). "Reset" is already the state the
	// machine is in, so this succeeds rather than erroring.
	if !daemon.UndervoltApplied() {
		fmt.Println("Curve Optimizer already at stock (0) — nothing applied")
		return nil
	}
	if err := hw.Undervolt.Reset(); err != nil {
		return withSetupHint("resetting curve optimizer", err)
	}
	fmt.Println("Curve Optimizer reset to stock (0)")
	return nil
}

// undervoltCommand is the firmware message a write would send, when the driver
// names one. Its error is the refusal a real write would meet, so a dry run on
// a CPU the driver does not know says so rather than describing a write that
// would never happen.
func undervoltCommand(u driver.Undervolter) (string, error) {
	c, ok := u.(driver.UndervoltCommander)
	if !ok {
		return "", nil
	}
	command, err := c.Command()
	if err != nil {
		return "", fmt.Errorf("curve optimizer not available — %w", err)
	}
	return command, nil
}

func init() {
	undervoltCmd.Flags().BoolVar(&uvGetFlag, "get", false, "Print the current undervolt offset")
	undervoltCmd.Flags().StringVar(&uvSetFlag, "set", "", "Set the all-core undervolt offset (0 or negative)")
	undervoltCmd.Flags().BoolVar(&uvResetFlag, "reset", false, "Reset the offset to stock (0)")
	undervoltCmd.Flags().StringVar(&uvProfileFlag, "profile", "", profileFlagUsage)
	rootCmd.AddCommand(undervoltCmd)
}
