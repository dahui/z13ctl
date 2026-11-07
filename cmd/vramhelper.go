package cmd

// vramhelper.go — hidden "vram-helper" subcommand: the root side of the
// z13ctl-vram helper. z13ctl-vram@.service runs it once per connection on
// /run/z13ctl/vram.sock, with the connection as stdin and stdout.

import (
	"errors"
	"os"

	"github.com/dahui/z13ctl/internal/cli"

	"github.com/spf13/cobra"
)

var vramHelperCmd = &cobra.Command{
	Use:    "vram-helper",
	Short:  "Answer one iGPU memory request on stdin (run by z13ctl-vram@.service)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if os.Geteuid() != 0 {
			return errors.New("vram-helper must run as root; it is started by z13ctl-vram.socket")
		}
		return cli.ServeVRAMHelper(os.Stdin, os.Stdout)
	},
}

func init() {
	rootCmd.AddCommand(vramHelperCmd)
}
