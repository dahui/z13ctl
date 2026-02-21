// z13ctl — system control for the 2025 ASUS ROG Flow Z13
//
// Controls keyboard and lightbar RGB (via Linux hidraw), performance profile,
// and battery charge limit (via asus-wmi sysfs interfaces).
//
// Usage:
//
//	z13ctl [--device keyboard|lightbar|PATH] [--dry-run] <command>
//
// Commands: apply  brightness  list  off  profile  batterylimit  setup
package main

import (
	"fmt"
	"os"

	"github.com/dahui/z13ctl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
