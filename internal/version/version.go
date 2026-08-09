// Package version holds the single build-time version shared by every
// voltaire binary, so the daemon+CLI binary and the GUI binary cannot
// drift apart (they used to carry separate cmd.Version / main.Version
// vars, each needing its own -X flag).
package version

// Version is the current release. Override at build time:
//
//	go build -ldflags "-X github.com/dahui/voltaire/v2/internal/version.Version=2.0.0"
//
// The default is used only in local builds without ldflags.
var Version = "2.0.0-dev"
