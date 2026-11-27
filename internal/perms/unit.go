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
// fails it; only this unit waits, and InputPlumber when beforeInputPlumber.
//
// beforeInputPlumber is set when the user opted into the InputPlumber
// passthrough override: the boot pass remakes it from InputPlumber's current
// config, which must happen before InputPlumber reads its configs.
func Unit(exe string, h Header, deviceID string, beforeInputPlumber bool) string {
	args := []string{exe, "setup", "--perms-only", "--group", h.Group}
	if deviceID != "" {
		args = append(args, "--machine", deviceID)
	}
	before := ""
	if beforeInputPlumber {
		before = "# Remakes the InputPlumber passthrough override from its current config\n" +
			"# before InputPlumber reads it (voltaire setup --inputplumber-passthrough).\n" +
			"Before=inputplumber.service\n"
	}
	return fmt.Sprintf(`[Unit]
Description=voltaire device permissions (the boot pass of voltaire setup)
%s# Grants voltaire's group the attributes this machine's device file needs that
# appear after their udev event, or have none (cpufreq boost, ryzen_smu).
After=systemd-udev-trigger.service systemd-modules-load.service
%sConditionPathExists=%s

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=-udevadm settle --timeout=30
ExecStart=%s

[Install]
WantedBy=multi-user.target
`, h.lines(), before, exe, strings.Join(args, " "))
}

// ParseUnit reads back the group and --machine a unit setup wrote was
// written with, "" for each when content is not such a unit. A later plain
// "voltaire setup" (every package upgrade runs one) keeps them rather than
// falling back to the defaults.
func ParseUnit(content string) (group, machine string) {
	if !strings.Contains(content, Marker) {
		return "", ""
	}
	for _, line := range strings.Split(content, "\n") {
		rest, ok := strings.CutPrefix(line, "ExecStart=")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		for i := 0; i+1 < len(f); i++ {
			switch f[i] {
			case "--group":
				group = f[i+1]
			case "--machine":
				machine = f[i+1]
			}
		}
	}
	return group, machine
}
