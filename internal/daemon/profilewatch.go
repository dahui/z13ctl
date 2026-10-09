package daemon

// profilewatch.go — tells the reconcile watcher about every platform_profile
// write, including the ones that leave nothing behind to observe.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"

	"golang.org/x/sys/unix"
)

// profileNotifyPath is the attribute the kernel notifies on every successful
// platform_profile write. A var so tests can point it elsewhere.
var profileNotifyPath = "/sys/firmware/acpi/platform_profile"

// profileNotifyTimeout bounds each poll so the watcher notices ctx ending.
const profileNotifyTimeout = 1000 // milliseconds

// watchProfileWrites sets d.profileWritten each time the kernel reports a
// platform_profile write, until ctx is done.
//
// It exists for the one write the reconcile watcher cannot otherwise see. Any
// platform_profile write makes the firmware re-apply that profile's own power
// limits (issue #22), and power-profiles-daemon writes the *same* profile on a
// charger transition that does not change it. On a custom profile with no fan
// curve that leaves no trace: platform_profile reads the same, there is no curve
// to find dropped, and the PPT attributes still show the custom limit — while
// the machine runs at the firmware's. The kernel, though, calls sysfs_notify on
// /sys/firmware/acpi/platform_profile after every successful write through
// either the legacy attribute or a class device, with no same-value check
// (drivers/acpi/platform_profile.c), and Fn+F5's cycle does the same. That is
// observable with poll(POLLPRI).
//
// Our own platform_profile writes notify too. They are harmless here: each one
// lands on a firmware profile, where the reconcile watcher does nothing.
//
// The read it takes is of a cached value (the handlers' profile_get), not an EC
// call, so it needs no ecWedged gate; the reconcile tick that acts on the flag
// has its own.
func (d *Daemon) watchProfileWrites(ctx context.Context) {
	f, err := os.Open(profileNotifyPath)
	if err != nil {
		slog.Debug("platform_profile write watcher not started", "err", err)
		return
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 64)
	// sysfs arms the notification on read, and re-arms on each read after one.
	if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
		slog.Debug("platform_profile write watcher not started", "err", err)
		return
	}
	slog.Info("platform_profile write watcher started")

	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLPRI | unix.POLLERR}}
	for ctx.Err() == nil {
		n, err := unix.Poll(fds, profileNotifyTimeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			slog.Warn("platform_profile write watcher stopped", "err", err)
			return
		}
		if n == 0 || fds[0].Revents&(unix.POLLPRI|unix.POLLERR) == 0 {
			continue
		}
		_, _ = f.ReadAt(buf, 0)
		d.profileWritten.Store(true)
		slog.Debug("platform_profile write reported by the kernel")
	}
}
