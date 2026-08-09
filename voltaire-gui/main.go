// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package main

// z13gui — GTK4 Wayland overlay drawer for z13ctl.
// Slides in from the right edge on Armoury Crate button press (via z13ctl daemon).

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/dahui/z13gui/internal/gui"
	"github.com/dahui/z13gui/internal/gui/gamepad"
	"github.com/dahui/z13gui/internal/startup"
	"github.com/dahui/z13gui/internal/theme"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// Version is set at link time via -X main.Version=<version>.
var Version = "dev"

func main() {
	// Scan args for our flags before GTK sees them. The flag package cannot be
	// used: app.Run() forwards the remainder to GLib's option parser, which errors
	// on anything it does not recognise, so our flags must be removed from the
	// slice rather than merely read.
	args := startup.ParseArgs(os.Args)
	switch args.Action {
	case startup.ActionVersion:
		fmt.Printf("z13gui %s\n", Version)
		os.Exit(0)
	case startup.ActionPrintTheme:
		fmt.Print(gui.DefaultThemeTOML())
		os.Exit(0)
	case startup.ActionListThemes:
		for _, t := range theme.Builtins {
			fmt.Printf("%-20s %s\n", t.ID, t.Name)
		}
		os.Exit(0)
	case startup.ActionNone:
	}

	// Configure slog with split-level filtering. gotk4's glib init() routes
	// all GLib/GTK messages through slog.Default(), adding a "glib_domain"
	// attribute. Our filterHandler uses that to apply separate thresholds:
	//   default: app=Info, GTK=Warn (show app events, suppress GTK debug/info noise)
	//   -d:      app=Debug, GTK=Debug (show everything including GTK internals)
	appLevel, gtkLevel := slog.LevelInfo, slog.LevelWarn
	if args.Debug {
		appLevel, gtkLevel = slog.LevelDebug, slog.LevelDebug
	}
	text := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: appLevel})
	slog.SetDefault(slog.New(startup.NewFilterHandler(text, appLevel, gtkLevel)))

	slog.Info("starting", "version", Version)

	// Disable GTK4 accessibility bridge (AT-SPI). z13gui is a hardware overlay
	// controlled by a physical button — no accessibility consumers. Without this,
	// GTK4 sends D-Bus events on every widget state change, which can timeout
	// under systemd where the AT-SPI bus may not be available.
	_ = os.Setenv("GTK_A11Y", "none")

	// Gamescope advertises Wayland layer-shell but doesn't implement
	// anchoring/margins. Force GTK4 to use X11 so we can set gamescope
	// overlay atoms instead. Must be set before GDK initialization.
	// Validate the socket exists to handle stale gamescope-environment
	// files left over from a previous Gaming Mode session.
	if gsDisplay := os.Getenv("GAMESCOPE_WAYLAND_DISPLAY"); gsDisplay != "" {
		runtime := os.Getenv("XDG_RUNTIME_DIR")
		socket := filepath.Join(runtime, gsDisplay)
		if runtime == "" {
			slog.Warn("GAMESCOPE_WAYLAND_DISPLAY set but XDG_RUNTIME_DIR missing, ignoring")
			_ = os.Unsetenv("GAMESCOPE_WAYLAND_DISPLAY")
		} else if _, err := os.Stat(socket); err != nil {
			slog.Info("gamescope socket missing (stale env?), using wayland", "path", socket)
			_ = os.Unsetenv("GAMESCOPE_WAYLAND_DISPLAY")
		} else {
			slog.Info("gamescope detected, forcing X11 backend")
			_ = os.Setenv("GDK_BACKEND", "x11")
		}
	}

	// Thaw any Steam process left frozen from a previous crash.
	gamepad.ThawFrozen()

	// Signal handler: thaw Steam before exit on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		gamepad.ThawFrozen()
		os.Exit(0)
	}()

	// GApplication registers com.github.dahui.z13gui on the session bus, so a
	// second launch of the binary does not start a second process: it forwards
	// "activate" to the running instance and exits. Without this guard that fires
	// gui.New again, building a second full drawer — its own layer surface,
	// subscribe loop, gamepad reader and telemetry poller — overlapping the first.
	// Launching from the desktop entry while the user service runs is enough to
	// trigger it. On re-activation, toggle the existing drawer instead.
	var win *gui.Window
	app := gtk.NewApplication("com.github.dahui.z13gui", 0)
	app.ConnectActivate(func() {
		if win != nil {
			slog.Info("re-activated, toggling existing drawer")
			win.Toggle()
			return
		}
		win = gui.New(app)
	})
	os.Exit(app.Run(args.GTKArgs))
}
