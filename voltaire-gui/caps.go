// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package main

// caps.go — give up the gamepad blocker's capabilities outside Gaming Mode.
//
// Packages and `make install` grant voltaire-gui CAP_BPF and CAP_PERFMON so the
// gamepad blocker can load its BPF LSM program. The blocker exists only under
// gamescope, but a binary with file capabilities is marked non-dumpable for its
// whole life, and xdg-desktop-portal refuses every request from a process it
// cannot inspect ("Unable to open /proc/PID/root"). GTK then never gets the
// desktop's settings and falls back to ~/.config/gtk-4.0/settings.ini: on KDE
// that meant a 144 dpi font setting on top of the compositor's own scale, so
// text was half again too large, and Adwaita as the base theme instead of the
// desktop's, which showed through wherever voltaire's sheet styles nothing.
//
// The drop happens in a C constructor because it has to cover every thread.
// Being dumpable is what the portal needs, and a dumpable process whose main
// thread has dropped its capabilities while another thread kept them could be
// ptraced by any process of the same user, which could then make that other
// thread run its code. The constructor runs before the Go runtime starts any
// thread, so one capset covers the process. It counts the threads anyway, and
// declines to become dumpable if it is not alone.
//
// Under gamescope the capabilities stay: the blocker needs them, and GTK runs
// on X11 there, which takes its settings from XSETTINGS rather than the portal.
// The gamescope test is main's own (the variable set and its socket present),
// so a stale variable from an earlier Gaming Mode session drops them too.

/*
#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <linux/capability.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

enum {
	VOLTAIRE_CAPS_NONE_HELD = 0,  // nothing to drop (source run, no setcap)
	VOLTAIRE_CAPS_KEPT,           // gamescope: the blocker needs them
	VOLTAIRE_CAPS_DROPPED,        // dropped and dumpable again
	VOLTAIRE_CAPS_NOT_ALONE,      // dropped on this thread only; left non-dumpable
	VOLTAIRE_CAPS_FAILED,         // capget/capset failed; voltaire_caps_errno says why
};

static int voltaire_caps_state = VOLTAIRE_CAPS_NONE_HELD;
static int voltaire_caps_errno = 0;

static int voltaire_in_gamescope(void) {
	const char *display = getenv("GAMESCOPE_WAYLAND_DISPLAY");
	const char *runtime = getenv("XDG_RUNTIME_DIR");
	char path[PATH_MAX];
	struct stat st;

	if (display == NULL || display[0] == '\0' || runtime == NULL || runtime[0] == '\0') {
		return 0;
	}
	if (snprintf(path, sizeof path, "%s/%s", runtime, display) >= (int)sizeof path) {
		return 0;
	}
	return stat(path, &st) == 0;
}

static int voltaire_thread_count(void) {
	DIR *d = opendir("/proc/self/task");
	struct dirent *e;
	int n = 0;

	if (d == NULL) {
		return -1;
	}
	while ((e = readdir(d)) != NULL) {
		if (e->d_name[0] != '.') {
			n++;
		}
	}
	closedir(d);
	return n;
}

__attribute__((constructor)) static void voltaire_drop_caps(void) {
	struct __user_cap_header_struct hdr = {_LINUX_CAPABILITY_VERSION_3, 0};
	struct __user_cap_data_struct data[_LINUX_CAPABILITY_U32S_3];

	memset(data, 0, sizeof data);
	if (syscall(SYS_capget, &hdr, data) != 0) {
		voltaire_caps_state = VOLTAIRE_CAPS_FAILED;
		voltaire_caps_errno = errno;
		return;
	}
	if (data[0].permitted == 0 && data[1].permitted == 0) {
		return;
	}
	if (voltaire_in_gamescope()) {
		voltaire_caps_state = VOLTAIRE_CAPS_KEPT;
		return;
	}
	memset(data, 0, sizeof data);
	if (syscall(SYS_capset, &hdr, data) != 0) {
		voltaire_caps_state = VOLTAIRE_CAPS_FAILED;
		voltaire_caps_errno = errno;
		return;
	}
	if (voltaire_thread_count() != 1) {
		voltaire_caps_state = VOLTAIRE_CAPS_NOT_ALONE;
		return;
	}
	prctl(PR_SET_DUMPABLE, 1, 0, 0, 0);
	voltaire_caps_state = VOLTAIRE_CAPS_DROPPED;
}

static int voltaire_caps_result(void) { return voltaire_caps_state; }
static int voltaire_caps_err(void) { return voltaire_caps_errno; }
*/
import "C"

import (
	"log/slog"
	"syscall"
)

// logCapabilityDrop reports what the constructor did. It runs before main, so
// this is the first moment there is a logger to say it with.
func logCapabilityDrop() {
	switch C.voltaire_caps_result() {
	case C.VOLTAIRE_CAPS_KEPT:
		slog.Info("keeping CAP_BPF/CAP_PERFMON for the gamepad blocker (gamescope)")
	case C.VOLTAIRE_CAPS_DROPPED:
		slog.Info("dropped CAP_BPF/CAP_PERFMON: the gamepad blocker runs only under gamescope")
	case C.VOLTAIRE_CAPS_NOT_ALONE:
		slog.Warn("dropped CAP_BPF/CAP_PERFMON on the main thread only, so the process stays " +
			"non-dumpable and the desktop portal will refuse it; GTK falls back to settings.ini")
	case C.VOLTAIRE_CAPS_FAILED:
		slog.Warn("could not drop CAP_BPF/CAP_PERFMON; the desktop portal will refuse this process "+
			"and GTK falls back to settings.ini", "err", syscall.Errno(C.voltaire_caps_err()))
	}
}
