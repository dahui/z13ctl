// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package daemon turns a z13ctl api call's result pair into a single error.
//
// Every api.Send* function returns (handled bool, err error), where handled is
// false and err is nil when the daemon is not running — the socket dial failed,
// so nothing was ever sent. Reading only err therefore treats "the daemon is not
// there" as success.
//
// That is not a hypothetical: the drawer used to discard handled at all thirteen
// call sites, so stopping the daemon and pressing Save TDP hid any existing
// error, logged "custom TDP saved" and left the typed values on screen. It is the
// same class of failure as z13ctl issue #14 — a control that reports success
// without doing anything — which is exactly what the error bar exists to prevent.
//
// It is its own package for the usual reason: internal/gui needs CGO and GTK4
// headers and cannot be unit tested, so the decision lives out here where it can
// be. Err is shaped to take an api call's results directly:
//
//	if err := daemon.Err(api.SendTdpReset()); err != nil {
package daemon

import "errors"

// ErrNotRunning reports that the daemon was unreachable, so the request was
// never sent. Worded for the error bar, which shows it to the user verbatim.
var ErrNotRunning = errors.New("z13ctl daemon is not running")

// Err collapses an api result pair into one error: nil only when the daemon
// handled the request and reported no failure.
//
// A real error wins over ErrNotRunning. The api layer only sets handled=false
// when the dial itself failed, in which case err is nil, so the two cannot
// disagree today — preferring err keeps it that way if that ever changes, since
// a specific message is always more useful than a generic one.
func Err(handled bool, err error) error {
	if err != nil {
		return err
	}
	if !handled {
		return ErrNotRunning
	}
	return nil
}
