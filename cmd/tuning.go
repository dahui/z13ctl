// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package cmd

// tuning.go — clearing every tuning override at once.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"

	"github.com/spf13/cobra"
)

var (
	tuningResetFlag   bool
	tuningProfileFlag string
)

var tuningCmd = &cobra.Command{
	Use:   "tuning",
	Short: "Manage tuning overrides as a group",
	Long: `Manage the tuning overrides — fan curve, power limits and Curve Optimizer
offset — as one group.

--reset clears all three and returns the machine to the balanced profile with
firmware fan control and stock power limits. It is not the same as running
'fancurve --reset', 'tdp --reset' and 'undervolt --reset' by hand: each of those
has to lower power before releasing the fans, so issuing them in the wrong order
leaves the machine at a high sustained limit with no fan floor. This is one
daemon operation, so the ordering cannot be got wrong.

The saved fan curve, limits and offset are forgotten as well — that is the
difference from 'tdp --reset', which keeps the saved offset so it can be
recalled with 'profile --set custom'.`,
	Example: `  voltaire tuning --reset
  voltaire tuning --reset --profile gaming`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if !tuningResetFlag {
			return fmt.Errorf("nothing to do: pass --reset")
		}
		return runTuningReset()
	},
}

func runTuningReset() error {
	if dryRunFlag {
		if tuningProfileFlag != "" {
			cli.DryRunProfileEdit(tuningProfileFlag, "cleared every tuning override")
			return nil
		}
		hw, err := hardware()
		if err != nil {
			return err
		}
		cli.DryRunTuningReset(envOf(hw))
		return nil
	}

	if err := ensureProfileTargetSupported(tuningProfileFlag); err != nil {
		return err
	}
	if handled, err := api.SendTuningResetFor(tuningProfileFlag); handled {
		if err != nil {
			return err
		}
		if tuningProfileFlag != "" {
			fmt.Printf("Cleared every tuning override from profile %s\n", tuningProfileFlag)
			return nil
		}
		fmt.Println("Tuning reset: switched to balanced profile (stock PPT restored, fans on firmware auto)")
		return nil
	}

	// No daemon. Only the daemon owns the saved profiles, so a --profile target
	// is refused rather than half-applied; the bare form falls through to the
	// same hardware sequence tdp --reset performs, which is already the whole of
	// what this command does to hardware.
	if err := requireDaemonForProfile(tuningProfileFlag); err != nil {
		return err
	}
	if err := runTdpResetDirect(); err != nil {
		return err
	}
	fmt.Println("Tuning reset: switched to balanced profile (stock PPT restored, fans on firmware auto)")
	fmt.Println("  (no daemon running, so no saved profile was changed)")
	return nil
}

func init() {
	tuningCmd.Flags().BoolVar(&tuningResetFlag, "reset", false, "Clear the fan curve, power limits and Curve Optimizer offset")
	tuningCmd.Flags().StringVar(&tuningProfileFlag, "profile", "", "Custom profile to clear (default: the active profile)")
	rootCmd.AddCommand(tuningCmd)
}
