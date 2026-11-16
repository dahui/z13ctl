// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package togglegate debounces the daemon's gui-toggle event. Some firmware
// revisions report one Armoury Crate press twice in the same instant, which
// would open the drawer and close it again; Accept keeps the first event of a
// burst. It is a package of its own so the rule is testable outside the cgo
// GUI.
package togglegate

import "time"

// Accept suppresses duplicate gui-toggle bursts.
// A single physical Armoury Crate button press has been observed to reach the
// GUI twice within the same instant; keep the first event and drop the burst.
func Accept(last, now time.Time, window time.Duration) (time.Time, bool) {
	if !last.IsZero() && now.Sub(last) < window {
		return last, false
	}
	return now, true
}
