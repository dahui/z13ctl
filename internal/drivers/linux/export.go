package linux

// export.go — small exported windows onto internals that dry-run output needs.
// Nothing here performs I/O.

// ProfileNameForDevice returns the profile string actually written to one
// platform-profile device — some devices name "quiet" differently (e.g.
// "low-power"), and dry-run output must show the mapped name SetProfile
// writes, not the ASUS-side name it was asked for.
func ProfileNameForDevice(deviceDir, asusProfile string) string {
	return profileNameForDevice(deviceDir, asusProfile)
}

// SysProfileDir returns the platform-profile class directory currently in use
// (a var internally, so the fake sysfs tree can redirect it).
func SysProfileDir() string { return sysProfileDir }
