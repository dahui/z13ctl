package linux

// paths.go — sysfs and procfs roots used by this package.
//
// These are vars rather than consts so tests can redirect them at a temporary
// directory tree and exercise the read/write helpers without hardware. Nothing
// outside tests should assign to them; use the Find*Path accessors instead.

var (
	// sysProfileDir holds the platform-profile class devices.
	sysProfileDir = "/sys/class/platform-profile"

	// sysProfileACPI is the last-resort platform_profile path when no
	// platform-profile class device is present.
	sysProfileACPI = "/sys/firmware/acpi/platform_profile"

	// sysPowerSupplyDir holds the battery devices (BAT0/BAT1).
	sysPowerSupplyDir = "/sys/class/power_supply"

	// sysPowercapDir is the RAPL energy-counter interface. Its energy_uj files
	// are 0400 root:root by default (the Platypus mitigation); voltaire setup
	// grants group read, as it does for the PPT attributes.
	sysPowercapDir = "/sys/class/powercap"

	// procStatPath and memInfoPath are the procfs sources for the CPU
	// utilisation counters and system memory gauges.
	procStatPath = "/proc/stat"
	memInfoPath  = "/proc/meminfo"

	// sysCPUDir holds the per-core cpufreq nodes.
	sysCPUDir = "/sys/devices/system/cpu"

	// sysCPUBoostPath is cpufreq's global boost switch. Writing it moves every
	// policy at once — measured on the Z13: 0 drops scaling_max_freq from
	// 5187500 to 3000000 on all 32 policies, 1 restores it — which is why this
	// is the one file rather than a walk over policy*/boost. It is what
	// `cpupower frequency-set --boost` writes.
	sysCPUBoostPath = "/sys/devices/system/cpu/cpufreq/boost"

	// sysThermalDir holds the thermal zones; the passive trip is the
	// telemetry chart's temperature hint.
	sysThermalDir = "/sys/class/thermal"

	// procNetDevPath and sysNetDir are the network byte counters and the
	// class directory that says which interfaces are physical.
	procNetDevPath = "/proc/net/dev"
	sysNetDir      = "/sys/class/net"

	// sysHwmonDir holds the hwmon devices, found by name: the read-only fan
	// speed source (telemetry.fans) on a device whose fans voltaire does not
	// control.
	sysHwmonDir = "/sys/class/hwmon"
)
