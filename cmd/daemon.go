package cmd

// daemon.go — "daemon" subcommand: run the z13ctl long-running device daemon.
//
// The daemon holds HID devices open, restores lighting state on startup,
// watches the Armoury Crate button, and serves a Unix socket for CLI and GUI
// clients. Designed to run as a systemd user service (z13ctl.socket +
// z13ctl.service); can also be launched directly for development.

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dahui/z13ctl/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the z13ctl device daemon",
	Long: `Run the z13ctl daemon as a long-running process.

The daemon opens and holds the ASUS HID devices, restores the last-applied
lighting state on startup, watches the Armoury Crate button, and serves a
Unix socket at $XDG_RUNTIME_DIR/z13ctl/z13ctl.sock.

CLI commands (apply, off, brightness, profile, batterylimit) will
automatically use the socket if the daemon is running, falling back to
direct hardware access when it is not.

The daemon is intended to be managed by systemd:

  systemctl --user enable --now z13ctl.socket

The contrib/systemd/user/ directory contains ready-to-use unit files.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		slog.Info("starting z13ctl daemon")
		if err := daemon.Run(ctx); err != nil {
			return err
		}
		slog.Info("z13ctl daemon stopped")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
