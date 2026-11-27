package perms

// unit.go — voltaire-perms.service, the boot pass. It used to be a list of
// shell loops kept in step with setup's Go code by hand (and by a drift test);
// it now runs that Go code, so there is one implementation.

import (
	"fmt"
	"strings"
)

// Unit renders voltaire-perms.service. exe is the voltaire binary setup was
// run as (a package install's /usr/bin, a source install's /usr/local/bin);
// deviceID is set only when setup was told which device file to use rather
// than matching DMI, and the boot pass must be told the same.
//
// The unit is a oneshot that stays active (RemainAfterExit), so an upgrade
// must restart it, not merely enable it, for a changed grant to apply before
// the next boot; setup does. ConditionPathExists makes a removed binary skip
// the unit rather than fail it at every boot.
//
// ExecStartPre waits for udev's queue to drain. Ordering after
// systemd-udev-trigger only means coldplug was queued, and a driver rebind
// still in that queue renumbers hidraw nodes while udev applies GROUP by
// node name, so a late event can grant an interface no rule matches (seen
// on the Z13's lightbar: docs/decisions/0006). The pass must run after that
// to reset it. Bounded, and "-" so a slow queue delays the pass, never
// fails it; only this unit waits, nothing else at boot.
func Unit(exe string, h Header, deviceID string) string {
	args := []string{exe, "setup", "--perms-only", "--group", h.Group}
	if deviceID != "" {
		args = append(args, "--machine", deviceID)
	}
	return fmt.Sprintf(`[Unit]
Description=voltaire device permissions (the boot pass of voltaire setup)
%s# Grants voltaire's group the attributes this machine's device file needs that
# appear after their udev event, or have none (cpufreq boost, ryzen_smu).
After=systemd-udev-trigger.service systemd-modules-load.service
ConditionPathExists=%s

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=-udevadm settle --timeout=30
ExecStart=%s

[Install]
WantedBy=multi-user.target
`, h.lines(), exe, strings.Join(args, " "))
}
