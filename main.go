// z13ctl — ROG Flow Z13 RGB lighting control
//
// Controls keyboard and lightbar RGB on the 2025 ASUS ROG Flow Z13 (USB 0b05:18c6)
// via Linux hidraw, using the Aura HID protocol reverse-engineered from g-helper.
//
// Usage:
//
//	z13ctl [--device keyboard|lightbar|PATH] [--dry-run] <command>
//
// Commands: apply  brightness  list  off  setup
package main

import (
	"fmt"
	"os"

	"z13ctl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
