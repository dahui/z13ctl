package cli

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

	// sysFirmwareAttrDir holds the asus-armoury BIOS attributes.
	sysFirmwareAttrDir = "/sys/class/firmware-attributes/asus-armoury/attributes"

	// pptBasePath holds the asus-nb-wmi PPT power limit attributes.
	pptBasePath = "/sys/devices/platform/asus-nb-wmi"

	// smuDriverPath holds the ryzen_smu kernel module's mailbox files.
	smuDriverPath = "/sys/kernel/ryzen_smu_drv"
)
