package amd

// paths.go — sysfs and procfs roots used by this package.
//
// These are vars rather than consts so tests can redirect them at a temporary
// directory tree and exercise the read/write helpers without hardware. Nothing
// outside tests should assign to them.

var (
	// sysHwmonDir holds the hwmon class devices; k10temp is found by name.
	sysHwmonDir = "/sys/class/hwmon"

	// smuDriverPath holds the ryzen_smu kernel module's mailbox files. The
	// hardware-test build points it nowhere (`make build-hwtest`) so no Curve
	// Optimizer write can be sent.
	smuDriverPath = "/sys/kernel/ryzen_smu_drv"

	// sysDrmDir holds the DRM class devices; GPU telemetry lives on the
	// amdgpu card's device node.
	sysDrmDir = "/sys/class/drm"

	// procCPUInfoPath gives the CPUID family and model, which choose the
	// Curve Optimizer command.
	procCPUInfoPath = "/proc/cpuinfo"

	// sysAccelDir and devAccelDir locate the amdxdna NPU accel device.
	sysAccelDir = "/sys/class/accel"
	devAccelDir = "/dev/accel"
)
