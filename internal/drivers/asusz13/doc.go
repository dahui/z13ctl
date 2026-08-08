// Package asusz13 is the hardware driver for the 2025 ASUS ROG Flow Z13
// (GZ302): fan curves and RPM via the asus-nb-wmi hwmon pair, PPT power limits
// via the asus-nb-wmi platform attributes, platform_profile, battery charge
// threshold, asus-armoury firmware toggles, and CPU Curve Optimizer via
// ryzen_smu — everything that was internal/cli's sysfs layer, moved here so
// device support is a package per family rather than one package that is
// secretly one device.
//
// Sysfs roots live in paths.go as vars purely so the fake tree in
// sysfs_fake_test.go can redirect them; the ppdRunner and smuReadFile/
// smuWriteFile seams exist for the same reason. Never let a test reach a real
// sysfs write.
//
// During the driver extraction internal/cli forwards its old exported surface
// here so cmd/ and internal/daemon compile unchanged; those forwarders are
// scaffolding and go away as consumers convert to the device registry.
package asusz13
