package asus

// paths.go — sysfs roots used by this package.
//
// These are vars rather than consts so tests can redirect them at a temporary
// directory tree and exercise the read/write helpers without hardware. Nothing
// outside tests should assign to them; use the Find*Path accessors instead.

var (
	// sysHwmonDir holds the hwmon class devices (fan curves, RPM).
	sysHwmonDir = "/sys/class/hwmon"

	// sysFirmwareAttrDir holds the asus-armoury BIOS attributes.
	sysFirmwareAttrDir = "/sys/class/firmware-attributes/asus-armoury/attributes"

	// pptBasePath holds the asus-nb-wmi PPT power limit attributes.
	pptBasePath = "/sys/devices/platform/asus-nb-wmi"
)
