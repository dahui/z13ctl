package device

// match.go — DMI identity reading and matching. Paths are vars so tests can
// point them at a fake tree, the same seam internal/cli's paths.go provides.

import (
	"os"
	"strings"
)

// DMI identity paths. Vars, not consts, purely so tests can redirect them.
var (
	dmiVendorPath  = "/sys/class/dmi/id/sys_vendor"
	dmiProductPath = "/sys/class/dmi/id/product_name"
)

// readDMI returns the machine's vendor and product identity, whitespace-
// trimmed (the kernel serves them newline-terminated).
func readDMI() (vendor, product string, err error) {
	v, err := os.ReadFile(dmiVendorPath)
	if err != nil {
		return "", "", err
	}
	p, err := os.ReadFile(dmiProductPath)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(v)), strings.TrimSpace(string(p)), nil
}

// matches reports whether any of the config's DMI patterns claims this
// machine.
func (c Config) matches(vendor, product string) bool {
	for _, m := range c.Device.Match {
		if m.Vendor != vendor {
			continue
		}
		if m.Product != "" && m.Product == product {
			return true
		}
		if m.ProductPrefix != "" && strings.HasPrefix(product, m.ProductPrefix) {
			return true
		}
	}
	return false
}
