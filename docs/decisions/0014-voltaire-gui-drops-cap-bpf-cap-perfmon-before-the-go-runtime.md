# voltaire-gui drops CAP_BPF/CAP_PERFMON before the Go runtime starts, except under gamescope

**voltaire-gui drops CAP_BPF/CAP_PERFMON before the Go runtime starts,
except under gamescope** (`voltaire-gui/caps.go`, 2026-11-15). File
capabilities make a process non-dumpable for life, and xdg-desktop-portal
refuses every request from a process it cannot inspect ("Unable to open
/proc/PID/root"). GTK then falls back to `~/.config/gtk-4.0/settings.ini`: on
this KDE desktop a 144 dpi font setting on top of the compositor's 150%, so
button and tab text was 1.5× too large, and Adwaita as the base theme, which
showed through as white switch knobs and arrows that never greyed. Every
z13gui and voltaire package install has looked like that since the blocker
shipped; source installs looked right only because they had no capabilities,
until `make install` started granting them. `GDK_DEBUG=no-portals` on an
uncapped build reproduces it pixel for pixel, and that is the quick check.
**The drop has to cover every thread, which is why it is a C constructor.**
Dumpable is what the portal needs, and a dumpable process with the main
thread stripped but another thread still holding CAP_BPF can be ptraced by
any process of the same user, which can then make that other thread run its
code: worse than not dropping at all. The constructor runs while the process
is single-threaded (runc's nsenter relies on the same guarantee), counts
`/proc/self/task` anyway, and stays non-dumpable if it is not alone.
`syscall.AllThreadsSyscall` is not an option, since it refuses with cgo.
Under gamescope the capabilities stay (the blocker needs them), which costs
nothing there: GTK runs on X11, whose settings come from XSETTINGS, not the
portal. The gamescope test is main's own (variable set *and* its socket
present). Verified: every thread's `CapPrm`/`CapEff` 0 on the desktop,
`/proc/PID/root` owned by the user, no portal warning, the rendering identical
to the uncapped build; with `GAMESCOPE_WAYLAND_DISPLAY` pointed at a live
socket the capabilities stay and `hidblocker: BPF LSM attached`.
