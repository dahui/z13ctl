# Profile and battery-threshold paths (`linux/profile.go`) use discovery via `/sys/class/*/` rather than hardcoded paths, so udev-chmoded device inodes are used (not the ACPI alias)

Profile and battery-threshold paths (`linux/profile.go`) use discovery via `/sys/class/*/` rather
than hardcoded paths, so udev-chmoded device inodes are used (not the ACPI alias).
