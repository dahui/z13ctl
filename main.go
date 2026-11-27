// voltaire controls power, fans, lighting and firmware settings on the
// handhelds and laptops its device files describe (internal/device/devices).
// This is the CLI and daemon binary; voltaire-gui is the drawer. `voltaire
// --help` lists the commands.
package main

import (
	"fmt"
	"os"

	"github.com/dahui/voltaire/v2/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
