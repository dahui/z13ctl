package cmd

// status.go — "status" subcommand: display a summary of all system metrics.
// Read-only. Aggregates APU temperature, fan RPM, profile, TDP, and battery
// information into a single dashboard view, once or (with --watch) repeatedly.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status (temperature, fans, profile, TDP, battery)",
	Long: `Display a summary of all system metrics in a single view.

Shows APU temperature, fan speed and mode, performance profile, TDP power
limits, and battery charge level and limit. All values are read directly
from sysfs.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if statusWatchFlag {
			return runStatusWatch(statusIntervalFlag)
		}
		return runStatus()
	},
}

var (
	statusWatchFlag    bool
	statusIntervalFlag time.Duration
)

func runStatus() error {
	return statusReport(os.Stdout)
}

// statusReport writes one status snapshot to out.
//
// It takes a writer so --watch can render into a buffer and emit each frame in
// one write: printing field by field straight to the terminal lets a redraw be
// seen half-finished, which at one frame a second reads as flicker.
func statusReport(out io.Writer) error {
	hw, err := hardware()
	if err != nil {
		return err
	}

	// Writing to os.Stdout or to a bytes.Buffer; neither yields an error a
	// status report could act on, and threading one through fourteen call sites
	// would say otherwise.
	outf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	outln := func(a ...any) { _, _ = fmt.Fprintln(out, a...) }

	// APU temperature.
	tempShown := false
	if hw.Telemetry != nil {
		if s, sErr := hw.Telemetry.Sample(); sErr == nil {
			outf("APU:     %d°C\n", s.TempC)
			tempShown = true
		}
	}
	if !tempShown {
		outln("APU:     N/A")
	}

	// Fan RPM and mode. The mode is folded across every readable fan, as the
	// daemon reports it: "custom" only when all of them honour the curve.
	rpmStr := "N/A"
	modeStr := ""
	if hw.Fans != nil {
		if rpms, rErr := hw.Fans.ReadRPM(); rErr == nil && len(rpms) > 0 {
			rpmStr = fmt.Sprintf("%d RPM", rpms[0])
		}
		if mode, mErr := hw.Fans.ReadMode(); mErr == nil {
			modeStr = ", mode: " + driver.FanModeName(mode)
		}
	}
	outf("Fans:    %s%s\n", rpmStr, modeStr)

	// Performance profile. platform_profile is never a custom profile name, so
	// the effective profile comes from the daemon when it is running; show the
	// firmware profile underneath it when the two differ.
	profile := effectiveProfileForTDP(hw)
	if hwProf := readCurrentProfile(hw); hwProf != profile && hwProf != "unknown" {
		outf("Profile: %s (platform: %s)\n", profile, hwProf)
	} else {
		outf("Profile: %s\n", profile)
	}

	// One battery reading serves the power-source line here and the charge
	// level at the bottom. ACKnown carries the "unknown is not on-battery"
	// distinction: no Mains supply means the line is simply omitted.
	var onAC bool
	acKnown := false
	capStr := "N/A"
	if hw.Battery != nil {
		if st, bErr := hw.Battery.Status(); bErr == nil {
			onAC, acKnown = st.OnAC, st.ACKnown
			capStr = fmt.Sprintf("%d%%", st.Capacity)
		}
	}

	// Power source, and what autoswitch would select for it.
	if acKnown {
		source := "battery"
		if onAC {
			source = "AC"
		}
		outf("Power:   %s%s\n", source, autoswitchNote(onAC))
	}

	// TDP power limits.
	tdpShown := false
	if hw.Power != nil {
		if tdp, tErr := hw.Power.ReadEffective(profile); tErr == nil {
			outf("TDP:     %dW (PL1) / %dW (PL2) / %dW (PL3)\n",
				tdp.PL1SPL, tdp.PL2SPPT, tdp.FPPT)
			tdpShown = true
		}
	}
	if !tdpShown {
		outln("TDP:     N/A")
	}

	// Undervolt (Curve Optimizer). Ask the daemon, which probed once at startup
	// and cached the answer.
	//
	// status must NOT probe itself: ProbeAvailable writes a CO offset of 0,
	// which is exactly a reset, and the cache that makes that harmless in the
	// daemon does not survive a CLI process — so probing here would silently
	// wipe an active undervolt every time anyone ran `status`. Without a
	// daemon, Present reports only what stat'ing sysfs can prove.
	// CO values have no sysfs readback, so current values need daemon state.
	if handled, st, err := api.SendGetState(); handled && err == nil && st != nil {
		if st.UndervoltAvailable {
			outln("UV:      available (use 'undervolt --get' for current values)")
		}
	} else if hw.Undervolt != nil && hw.Undervolt.Present() {
		outln("UV:      ryzen_smu loaded (start the daemon to confirm Curve Optimizer support)")
	}

	// Battery: current charge level and charge limit.
	limitStr := ""
	if hw.Battery != nil {
		if limit, lErr := hw.Battery.ChargeLimit(); lErr == nil {
			limitStr = fmt.Sprintf(" (limit: %d%%)", limit)
		}
	}
	outf("Battery: %s%s\n", capStr, limitStr)

	return nil
}

// runStatusWatch redraws the status report every interval until interrupted.
//
// Deliberately not a TUI: no alternate screen, no raw mode, no input handling,
// no dependency. It redraws in place by walking the cursor back over the lines
// it printed last time and clearing from there down — so the scrollback above
// is untouched, unlike a full clear, and Ctrl-C leaves the last frame on screen
// where you can still read it. A third party already fills the TUI niche over
// the same socket (ayixiayi/z13-panel); competing with it would be worse than
// pointing at it.
//
// It does not require the daemon. status reads sysfs directly by design, the
// device handle is cached for the process, and one pass costs what the daemon's
// own 1 Hz sampler costs — so refusing without a daemon would be a restriction
// with nothing behind it.
func runStatusWatch(interval time.Duration) error {
	if interval < 100*time.Millisecond {
		return fmt.Errorf("--interval %s is too short; the sensors do not update faster than about 1s", interval)
	}

	// Piped or redirected output gets plain frames with no escape codes: cursor
	// movement written to a file is noise, and someone capturing `status --watch`
	// wants something readable at the end of it.
	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lines := 0
	for {
		var buf bytes.Buffer
		if err := statusReport(&buf); err != nil {
			return err
		}
		frame := buf.String()

		if tty && lines > 0 {
			// Up over the previous frame, then clear from the cursor to the end
			// of the screen. Clearing is what keeps a shorter frame from leaving
			// the tail of a longer one behind — the fields here come and go
			// (Power and UV are omitted when unknown).
			fmt.Printf("\033[%dA\033[J", lines)
		}
		fmt.Print(frame)
		lines = strings.Count(frame, "\n")

		select {
		case <-stop:
			return nil
		case <-ticker.C:
		}
	}
}

// autoswitchNote returns a parenthetical naming the profile autoswitch selects
// for the current source, or "" when it is off, unconfigured, or the daemon is
// not running. Best-effort: status must not fail because autoswitch is unset.
func autoswitchNote(onAC bool) string {
	handled, value, err := api.SendAutoswitchGet()
	if !handled || err != nil {
		return ""
	}
	var st autoswitchStatus
	if err := json.Unmarshal([]byte(value), &st); err != nil || !st.Enabled {
		return ""
	}
	target := st.Battery
	if onAC {
		target = st.AC
	}
	if target == "" {
		return ""
	}
	return " (autoswitch: " + target + ")"
}

func init() {
	statusCmd.Flags().BoolVarP(&statusWatchFlag, "watch", "w", false, "Redraw the status continuously until interrupted")
	// One second matches the daemon's sampler, so a shorter interval would redraw
	// faster than the numbers can change.
	statusCmd.Flags().DurationVar(&statusIntervalFlag, "interval", time.Second, "How often to redraw with --watch")
	rootCmd.AddCommand(statusCmd)
}
