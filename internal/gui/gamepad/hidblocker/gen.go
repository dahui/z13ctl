// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package hidblocker

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64 blocker blocker.bpf.c -- -I. -Wall -O2 -g -Wno-address-of-packed-member
