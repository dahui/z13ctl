module github.com/dahui/voltaire/v2

go 1.25.0

require (
	github.com/coreos/go-systemd/v22 v22.7.0
	github.com/holoplot/go-evdev v0.0.0-20260504100651-66d1748fe847
	github.com/spf13/cobra v1.10.2
)

require github.com/godbus/dbus/v5 v5.2.2

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/cilium/ebpf v0.21.0
	github.com/diamondburned/gotk4-layer-shell/pkg v0.0.0-20240109211357-6efa9f6dc438
	github.com/diamondburned/gotk4/pkg v0.3.1
)

require (
	github.com/KarpelesLab/weak v0.1.1 // indirect
	go4.org/unsafe/assume-no-moving-gc v0.0.0-20231121144256-b99613f794b6 // indirect
	golang.org/x/sync v0.20.0 // indirect
)

require (
	github.com/dahui/voltaire/api/v2 v2.0.0
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	golang.org/x/sys v0.46.0 // indirect
)

// The api module is unpublished until the v2.0.0 release; drop this directive
// when tagging (see the release workflow in CLAUDE.md).
replace github.com/dahui/voltaire/api/v2 => ./api
