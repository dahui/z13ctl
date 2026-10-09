package daemon

// policywatch.go — tells the reconcile watcher about every write of the
// firmware's thermal policy, including the ones that leave nothing behind to
// observe.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"

	"golang.org/x/sys/unix"
)

// policyNotifyPaths are the attributes the kernel notifies when the thermal
// policy is written. Vars so tests can point them elsewhere.
var policyNotifyPaths = []string{
	// Every successful platform_profile write.
	"/sys/firmware/acpi/platform_profile",
	// Every throttle_thermal_policy_write(): platform_profile writes, and fan
	// releases (pwm_enable=2), which go through the same call.
	"/sys/devices/platform/asus-nb-wmi/throttle_thermal_policy",
}

// policyNotifyTimeout bounds each poll so the watcher notices ctx ending.
const policyNotifyTimeout = 1000 // milliseconds

// watchPolicyWrites sets d.policyWritten each time the kernel reports a
// thermal policy write, until ctx is done.
//
// It exists for the writes the reconcile watcher cannot otherwise see. A
// platform_profile write or a fan release makes the firmware re-apply the
// profile's own power limits (issue #22), and both can leave no trace:
// power-profiles-daemon writes the *same* profile on a charger transition that
// does not change it, and a tool releasing fans that are already on firmware
// auto changes no mode. On a custom profile with no fan curve, platform_profile
// and pwm_enable then read as before and the PPT attributes still show the
// custom limit, while the machine runs at the firmware's.
//
// The kernel reports both. drivers/acpi/platform_profile.c calls sysfs_notify on
// /sys/firmware/acpi/platform_profile after every successful write through the
// legacy attribute or a class device, and after Fn+F5's cycle, with no
// same-value check. asus-wmi's throttle_thermal_policy_write(), which every
// platform_profile write and every pwm_enable=2 on the curve device goes
// through, notifies throttle_thermal_policy. poll(POLLPRI) sees each one.
// throttle_thermal_policy exists only under CONFIG_ASUS_WMI_DEPRECATED_ATTRS;
// without it a foreign redundant fan release goes unseen again. Its show
// function, unlike the ppt_* ones, logs no deprecation notice.
//
// Our own writes notify too. Profile writes land on firmware profiles, where
// the reconcile watcher does nothing. Fan releases on a custom profile
// (ReleaseFans) already re-write the limit, so the tick that follows re-writes
// the same values once more, and logs it at Info.
//
// The reads it takes are of cached values (profile_get, the driver's
// throttle_thermal_policy_mode), not EC calls, so it needs no ecWedged gate;
// the reconcile tick that acts on the flag has its own.
func (d *Daemon) watchPolicyWrites(ctx context.Context) {
	buf := make([]byte, 64)
	var files []*os.File
	var watched []string
	for _, path := range policyNotifyPaths {
		f, err := os.Open(path)
		if err != nil {
			slog.Debug("thermal policy attribute not watched", "path", path, "err", err)
			continue
		}
		// sysfs arms the notification on read, and re-arms on each read after one.
		if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
			slog.Debug("thermal policy attribute not watched", "path", path, "err", err)
			_ = f.Close()
			continue
		}
		files = append(files, f)
		watched = append(watched, path)
	}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	if len(files) == 0 {
		slog.Debug("thermal policy write watcher not started: nothing to watch")
		return
	}
	slog.Info("thermal policy write watcher started", "paths", watched)

	for ctx.Err() == nil && len(files) > 0 {
		fds := make([]unix.PollFd, len(files))
		for i, f := range files {
			fds[i] = unix.PollFd{Fd: int32(f.Fd()), Events: unix.POLLPRI | unix.POLLERR}
		}
		n, err := unix.Poll(fds, policyNotifyTimeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			slog.Warn("thermal policy write watcher stopped", "err", err)
			return
		}
		if n == 0 {
			continue
		}
		reported := false
		for i := len(files) - 1; i >= 0; i-- {
			if fds[i].Revents&(unix.POLLPRI|unix.POLLERR) == 0 {
				continue
			}
			// A removed attribute (module unloaded) reports POLLERR on every poll
			// and fails the read; drop it rather than spin re-applying the limit.
			if _, err := files[i].ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
				slog.Warn("thermal policy attribute no longer watched", "path", watched[i], "err", err)
				_ = files[i].Close()
				files = append(files[:i], files[i+1:]...)
				watched = append(watched[:i], watched[i+1:]...)
				continue
			}
			reported = true
		}
		if reported {
			d.policyWritten.Store(true)
			slog.Debug("thermal policy write reported by the kernel")
		}
	}
}
