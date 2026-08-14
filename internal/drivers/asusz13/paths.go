package asusz13

// paths.go — sysfs roots used by this package.
//
// These are vars rather than consts so tests can redirect them at a temporary
// directory tree and exercise the read/write helpers without hardware. Nothing
// outside tests should assign to them; use the Find*Path accessors instead.

var (
	// sysHwmonDir holds the hwmon class devices (fan curves, RPM, k10temp).
	sysHwmonDir = "/sys/class/hwmon"

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

	// sysFirmwareAttrDir holds the asus-armoury BIOS attributes.
	sysFirmwareAttrDir = "/sys/class/firmware-attributes/asus-armoury/attributes"

	// pptBasePath holds the asus-nb-wmi PPT power limit attributes.
	pptBasePath = "/sys/devices/platform/asus-nb-wmi"

	// smuDriverPath holds the ryzen_smu kernel module's mailbox files.
	smuDriverPath = "/sys/kernel/ryzen_smu_drv"

	// sysDrmDir holds the DRM class devices; GPU telemetry lives on the
	// amdgpu card's device node.
	sysDrmDir = "/sys/class/drm"

	// procStatPath and memInfoPath are the procfs sources for the CPU
	// utilisation counters and system memory gauges.
	procStatPath = "/proc/stat"
	memInfoPath  = "/proc/meminfo"

	// sysCPUDir holds the per-core cpufreq nodes.
	sysCPUDir = "/sys/devices/system/cpu"

	// sysAccelDir and devAccelDir locate the amdxdna NPU accel device.
	sysAccelDir = "/sys/class/accel"
	devAccelDir = "/dev/accel"
)
