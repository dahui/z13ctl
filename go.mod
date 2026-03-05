module github.com/dahui/z13ctl

go 1.23

require (
	github.com/coreos/go-systemd/v22 v22.7.0
	github.com/holoplot/go-evdev v0.0.0-20250804134636-ab1d56a1fe83
	github.com/spf13/cobra v1.10.2
)

require github.com/godbus/dbus/v5 v5.2.2 // indirect

require (
	github.com/dahui/z13ctl/api v1.0.0
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.27.0 // indirect
)

replace github.com/dahui/z13ctl/api v1.0.0 => ./api
