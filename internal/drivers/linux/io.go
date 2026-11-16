package linux

// io.go — this package's names for the shared sysfs helpers, so code moved
// from the Z13 driver reads as it did.

import "github.com/dahui/voltaire/v2/internal/drivers/internal/sysfsio"

func readIntFile(path string) (int, error) { return sysfsio.ReadInt(path) }

func writeIntFile(path string, value int) error { return sysfsio.WriteInt(path, value) }

func readSysfsTrimmed(path string) string { return sysfsio.ReadTrimmed(path) }

func readUint(path string) (uint64, error) { return sysfsio.ReadUint(path) }
