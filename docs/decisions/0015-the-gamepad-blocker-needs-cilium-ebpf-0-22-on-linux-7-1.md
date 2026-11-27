# The gamepad blocker needs cilium/ebpf ≥ 0.22 on Linux 7.1+

**The gamepad blocker needs cilium/ebpf ≥ 0.22 on Linux 7.1+.** Loading the
LSM program applies CO-RE relocations against the kernel's BTF, and 7.1
extended the BTF header: 0.21 fails with `parsing .BTF header: header
contains non-zero trailer`. Verified on 7.3.4 by running `hidblocker`'s
`TestNew` as root (compile with `go test -c`, run the binary under `sudo`):
0.21 fails, 0.22 attaches. Every privileged install on a current kernel had
been falling back to SIGSTOP. That root run is the check to repeat after any
ebpf bump. The old line was
`[ -f x ] && install ... || true`, which reported nothing when the binary was
absent and swallowed a real `install` failure with it.
