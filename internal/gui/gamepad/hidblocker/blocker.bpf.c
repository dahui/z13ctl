//go:build ignore

// Deliberately carries no SPDX header, unlike every other source file here.
//
// The LICENSE[] section at the bottom of this file declares "GPL" to the kernel.
// That is not a copyright statement — it is the string the kernel's BPF verifier
// reads to decide whether the program may call GPL-only helpers, and it is
// load-bearing: BPF_CORE_READ below expands to bpf_probe_read_kernel, which is
// gpl_only, so the program fails to load if it declares anything else.
//
// Stamping an Apache-2.0 header here would sit awkwardly next to that, since
// Apache-2.0 and GPL-2.0 are not one-way compatible, and the honest answer needs
// a decision rather than a default: the usual convention for BPF sources is to
// license them "GPL-2.0 OR BSD-3-Clause" so the kernel declaration and the
// source licence agree. Left unstamped pending that call — the repository
// LICENSE still applies to it in the meantime.

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

#define EAGAIN  11
#define MAY_READ 4
#define MAJOR(dev) ((unsigned int)((dev) >> 20))

// blocked_pids: PIDs whose hidraw reads should return -EAGAIN.
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 64);
	__type(key, __u32);
	__type(value, __u8);
} blocked_pids SEC(".maps");

// hidraw_config: index 0 = hidraw character device major number.
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, __u32);
} hidraw_config SEC(".maps");

SEC("lsm/file_permission")
int BPF_PROG(hidraw_block, struct file *file, int mask, int ret)
{
	if (ret != 0)
		return ret;

	if (!(mask & MAY_READ))
		return 0;

	__u32 pid = bpf_get_current_pid_tgid() >> 32;
	if (!bpf_map_lookup_elem(&blocked_pids, &pid))
		return 0;

	dev_t rdev = BPF_CORE_READ(file, f_inode, i_rdev);
	__u32 major = MAJOR(rdev);

	__u32 key = 0;
	__u32 *hidraw_major = bpf_map_lookup_elem(&hidraw_config, &key);
	if (!hidraw_major || major != *hidraw_major)
		return 0;

	return -EAGAIN;
}

char LICENSE[] SEC("license") = "GPL";
